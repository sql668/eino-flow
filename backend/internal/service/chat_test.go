package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	// Eino 的 model 包与本项目 model 包同名，这里起别名区分：
	// 前者只提供对话模型的接口与选项，后者是本项目的领域模型。
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// ─────────────────────────────────────────────────────────────
// 测试替身
//
// 一律嵌入接口再覆盖用到的方法：仓储接口有十几个方法，逐个实现只会制造噪声，
// 而只要测试不碰其它方法，嵌入的 nil 接口就不会被调用。
// ─────────────────────────────────────────────────────────────

type fakeAppRepo struct {
	repository.AppRepository
	app *model.App
	err error
}

func (f *fakeAppRepo) GetByID(context.Context, uint64) (*model.App, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.app, nil
}

type fakeConversationRepo struct {
	repository.ConversationRepository
	nextID  uint64
	created []*model.Conversation
}

func (f *fakeConversationRepo) Create(_ context.Context, conversation *model.Conversation) error {
	f.nextID++
	conversation.ID = f.nextID
	f.created = append(f.created, conversation)
	return nil
}

func (f *fakeConversationRepo) GetByID(_ context.Context, id uint64) (*model.Conversation, error) {
	for _, item := range f.created {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, repository.ErrNotFound
}

type fakeMessageRepo struct {
	repository.MessageRepository
	created []*model.Message
	recent  []*model.Message
}

func (f *fakeMessageRepo) Create(_ context.Context, message *model.Message) error {
	f.created = append(f.created, message)
	return nil
}

func (f *fakeMessageRepo) ListRecent(context.Context, uint64, int) ([]*model.Message, error) {
	return f.recent, nil
}

type fakeResolver struct {
	enginemodel.Resolver
	chatModel enginemodel.ChatModel
	err       error
}

func (f *fakeResolver) ChatModel(context.Context, enginemodel.ScopeRef) (enginemodel.ChatModel, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.chatModel, nil
}

type fakeDatasetRepo struct {
	repository.DatasetRepository
	dataset *model.Dataset
}

func (f *fakeDatasetRepo) GetByID(context.Context, uint64) (*model.Dataset, error) {
	return f.dataset, nil
}

// fakeRagRetriever 实现 service.Retriever：给定命中或错误，并记录收到的检索请求。
type fakeRagRetriever struct {
	output      rag.RetrieveOutput
	err         error
	calls       int
	lastRequest rag.RetrieveRequest
}

func (f *fakeRagRetriever) Retrieve(
	_ context.Context, request rag.RetrieveRequest,
) (rag.RetrieveOutput, error) {
	f.calls++
	f.lastRequest = request
	if f.err != nil {
		return rag.RetrieveOutput{}, f.err
	}
	return f.output, nil
}

type fakeTraceRepo struct {
	repository.RetrievalRepository
	runs []*model.RetrievalRun
	hits []*model.RetrievalHit
}

func (f *fakeTraceRepo) CreateRun(_ context.Context, run *model.RetrievalRun) error {
	run.ID = uint64(len(f.runs) + 1)
	f.runs = append(f.runs, run)
	return nil
}

func (f *fakeTraceRepo) CreateHits(_ context.Context, hits []*model.RetrievalHit) error {
	f.hits = append(f.hits, hits...)
	return nil
}

// eventCollector 是 SSE 事件的测试收集器（任务 1.3）。
//
// 用「按顺序收集 + 事后断言序列」而不是逐条 if：D13 记录过的问题都是**顺序**问题，
// 序列断言能直接表达不变量，失败信息也能一眼看出是哪一步错了。
type eventCollector struct {
	events []ChatEvent
	// onDelta 在收到第一个 delta 时回调（用于"事件不攒批"的断言）。
	onDelta func()
	deltaes int
}

func (c *eventCollector) emit(event ChatEvent) error {
	if event.Type == ChatEventDelta {
		c.deltaes++
		if c.deltaes == 1 && c.onDelta != nil {
			c.onDelta()
		}
	}
	c.events = append(c.events, event)
	return nil
}

func (c *eventCollector) types() []string {
	out := make([]string, 0, len(c.events))
	for _, event := range c.events {
		out = append(out, event.Type)
	}
	return out
}

func (c *eventCollector) contentOf(eventType string) string {
	var builder strings.Builder
	for _, event := range c.events {
		if event.Type == eventType {
			builder.WriteString(event.Content)
		}
	}
	return builder.String()
}

func (c *eventCollector) firstIndexOf(eventType string) int {
	for i, event := range c.events {
		if event.Type == eventType {
			return i
		}
	}
	return -1
}

// testChatModel 是对话模型替身：可控制输出分块、思考内容与报错。
type testChatModel struct {
	chunks    []*schema.Message
	streamErr error
	// release 非 nil 时，第二块要等它被关闭才发出——用于验证"事件不攒批"。
	release    chan struct{}
	secondSent atomic.Bool
}

func (m *testChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	var content, reasoning strings.Builder
	for _, chunk := range m.chunks {
		content.WriteString(chunk.Content)
		reasoning.WriteString(chunk.ReasoningContent)
	}
	message := schema.AssistantMessage(content.String(), nil)
	message.ReasoningContent = reasoning.String()
	return message, nil
}

func (m *testChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	if m.release == nil || len(m.chunks) < 2 {
		return schema.StreamReaderFromArray(m.chunks), nil
	}
	reader, writer := schema.Pipe[*schema.Message](len(m.chunks))
	go func() {
		defer writer.Close()
		writer.Send(m.chunks[0], nil)
		select {
		case <-m.release:
		case <-time.After(2 * time.Second):
			// 兜底：实现若把整条流攒到最后才消费，这里不能把测试挂死，
			// 超时后照常推出后续分块，断言会以"攒批"的形式失败。
		}
		m.secondSent.Store(true)
		for _, chunk := range m.chunks[1:] {
			writer.Send(chunk, nil)
		}
	}()
	return reader, nil
}

// ─────────────────────────────────────────────────────────────
// 测试装置
// ─────────────────────────────────────────────────────────────

type chatFixture struct {
	service  *ChatService
	apps     *fakeAppRepo
	messages *fakeMessageRepo
	resolver *fakeResolver
	rag      *fakeRagRetriever
	traces   *fakeTraceRepo
}

// newChatFixture 装配一个 ChatService：真正用上 RetrievalService，
// 只在它最外层的依赖上换替身——这样"引用先于回答"这类顺序不变量才是真的被测到。
func newChatFixture(mode model.AppMode, chatModel enginemodel.ChatModel) *chatFixture {
	apps := &fakeAppRepo{app: &model.App{ID: 4, Name: "测试应用", Mode: mode}}
	conversations := &fakeConversationRepo{}
	messages := &fakeMessageRepo{}
	resolver := &fakeResolver{chatModel: chatModel}
	datasets := &fakeDatasetRepo{dataset: &model.Dataset{ID: 7, Name: "测试知识库"}}
	ragRetriever := &fakeRagRetriever{}
	traces := &fakeTraceRepo{}

	retrieval := NewRetrievalService(datasets, ragRetriever, traces, RetrievalDefaults{
		TopK:          5,
		CandidateTopK: 20,
	})
	return &chatFixture{
		service:  NewChatService(apps, conversations, messages, resolver, retrieval),
		apps:     apps,
		messages: messages,
		resolver: resolver,
		rag:      ragRetriever,
		traces:   traces,
	}
}

// withHits 让检索返回一条带完整溯源的命中。
func (f *chatFixture) withHits() {
	f.rag.output = rag.RetrieveOutput{
		Hits: []rag.Hit{{
			SegmentID:    11,
			DocumentID:   3,
			DocumentName: "万用表RS-12的使用.pdf",
			Content:      "更换电池前请先断开电源。",
			Rank:         1,
			Score:        0.74,
			Page:         6,
			TitlePath:    []string{"维护", "更换电池"},
			Kind:         "text",
		}},
		Candidates:     []rag.Hit{{SegmentID: 11, Rank: 1, Score: 0.74}},
		EmbeddingModel: "bge-m3",
		Mode:           repository.SearchModeVector,
		Threshold:      0,
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 1.3：收集器自测
// ─────────────────────────────────────────────────────────────

func TestEventCollector_RecordsTypeAndOrder(t *testing.T) {
	collector := &eventCollector{}
	for _, event := range []ChatEvent{
		{Type: ChatEventCitations},
		{Type: ChatEventReasoning, Content: "想一想"},
		{Type: ChatEventDelta, Content: "答案"},
		{Type: ChatEventDone},
	} {
		if err := collector.emit(event); err != nil {
			t.Fatalf("收集事件失败: %v", err)
		}
	}

	want := []string{ChatEventCitations, ChatEventReasoning, ChatEventDelta, ChatEventDone}
	got := collector.types()
	if len(got) != len(want) {
		t.Fatalf("期望 %d 个事件，实际 %d 个：%v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 个事件应为 %s，实际 %s", i+1, want[i], got[i])
		}
	}
	if collector.contentOf(ChatEventDelta) != "答案" {
		t.Fatal("按类型取内容失败")
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 2.1：形态路由
// ─────────────────────────────────────────────────────────────

func TestChat_ModeRouting(t *testing.T) {
	t.Run("chat 形态是纯对话，不检索", func(t *testing.T) {
		fixture := newChatFixture(model.AppModeChat, &testChatModel{
			chunks: []*schema.Message{schema.AssistantMessage("你好", nil)},
		})
		collector := &eventCollector{}

		if err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "你好",
		}, collector.emit); err != nil {
			t.Fatalf("纯对话不应失败: %v", err)
		}
		if fixture.rag.calls != 0 {
			t.Fatalf("chat 形态不应触发检索，实际 %d 次", fixture.rag.calls)
		}
		if collector.firstIndexOf(ChatEventCitations) != -1 {
			t.Fatal("chat 形态没有引用来源，不应发 citations 事件")
		}
		if collector.types()[len(collector.types())-1] != ChatEventDone {
			t.Fatal("最后一个事件应是 done")
		}
	})

	t.Run("rag 形态必须带知识库并触发检索", func(t *testing.T) {
		fixture := newChatFixture(model.AppModeRAG, &testChatModel{
			chunks: []*schema.Message{schema.AssistantMessage("先断开电源 [1]", nil)},
		})
		fixture.withHits()
		collector := &eventCollector{}

		if err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "怎么更换电池", DatasetID: 7,
		}, collector.emit); err != nil {
			t.Fatalf("RAG 问答不应失败: %v", err)
		}
		if fixture.rag.calls != 1 {
			t.Fatalf("rag 形态应触发一次检索，实际 %d 次", fixture.rag.calls)
		}
		if fixture.rag.lastRequest.DatasetID != 7 {
			t.Fatalf("检索应带上知识库 ID，实际 %d", fixture.rag.lastRequest.DatasetID)
		}
	})

	t.Run("rag 形态缺少知识库时拒绝", func(t *testing.T) {
		fixture := newChatFixture(model.AppModeRAG, &testChatModel{})
		err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "怎么更换电池",
		}, (&eventCollector{}).emit)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("期望参数错误，实际: %v", err)
		}
	})

	for _, mode := range []model.AppMode{model.AppModeAgent, model.AppModeWorkflow} {
		t.Run(string(mode)+" 形态明确拒绝且不落任何消息", func(t *testing.T) {
			fixture := newChatFixture(mode, &testChatModel{
				chunks: []*schema.Message{schema.AssistantMessage("不该被调用", nil)},
			})
			err := fixture.service.Chat(context.Background(), ChatInput{
				AppID: 4, Question: "你好", DatasetID: 7,
			}, (&eventCollector{}).emit)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("期望明确拒绝，实际: %v", err)
			}
			if !strings.Contains(err.Error(), string(mode)) {
				t.Fatalf("错误信息应说明是哪种形态，实际: %v", err)
			}
			if len(fixture.messages.created) != 0 {
				t.Fatal("被拒绝的形态不应写入任何消息（不能留下半截会话）")
			}
			if fixture.rag.calls != 0 {
				t.Fatal("被拒绝的形态不应触发检索")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 2.2 / 4.2：事件顺序不变量
// ─────────────────────────────────────────────────────────────

// 这是本 change 的核心断言：引用来源必须**先于第一段回答增量**到达，
// 让用户在等首字的时候就能看到出处。顺序错了（比如先出文本再补引用）也"能用"，
// 但体验与设计意图就不一样了，所以把它写成显式的序列断言。
func TestChat_CitationsArriveBeforeFirstDelta(t *testing.T) {
	fixture := newChatFixture(model.AppModeRAG, &testChatModel{
		chunks: []*schema.Message{
			// 推理模型的思考块：不变量要连着思考一起断言，
			// 否则「思考插到引用之前」这种顺序问题仍然测不出来。
			{Role: schema.Assistant, ReasoningContent: "先确认是电池还是保险丝"},
			schema.AssistantMessage("先断开", nil),
			schema.AssistantMessage("电源 [1]", nil),
		},
	})
	fixture.withHits()
	collector := &eventCollector{}

	if err := fixture.service.Chat(context.Background(), ChatInput{
		AppID: 4, Question: "怎么更换电池", DatasetID: 7,
	}, collector.emit); err != nil {
		t.Fatalf("RAG 问答失败: %v", err)
	}

	got := collector.types()
	want := []string{ChatEventCitations, ChatEventReasoning, ChatEventDelta, ChatEventDelta, ChatEventDone}
	if len(got) != len(want) {
		t.Fatalf("事件序列长度不符：期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 个事件应为 %s，实际 %s（完整序列 %v）", i+1, want[i], got[i], got)
		}
	}
	if len(collector.events[0].Citations) != 1 {
		t.Fatal("citations 事件应带上命中片段")
	}
	if collector.events[0].Citations[0].DocumentName != "万用表RS-12的使用.pdf" {
		t.Fatal("引用应带文档名，供界面展示出处")
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 2.3：思考过程与正文分离（双向断言）
// ─────────────────────────────────────────────────────────────

// 只断言"思考出现在 reasoning 事件里"是不够的——那样会漏掉"顺便也进了正文"这种回归，
// 所以两头都要断言：事件里有、正文与落库内容里没有。
func TestChat_ReasoningStaysOutOfAnswer(t *testing.T) {
	thinking := "先想想用户问的是电池还是保险丝"
	fixture := newChatFixture(model.AppModeChat, &testChatModel{
		chunks: []*schema.Message{
			{Role: schema.Assistant, ReasoningContent: thinking},
			schema.AssistantMessage("请先断开电源。", nil),
		},
	})
	collector := &eventCollector{}

	if err := fixture.service.Chat(context.Background(), ChatInput{
		AppID: 4, Question: "怎么换电池",
	}, collector.emit); err != nil {
		t.Fatalf("问答失败: %v", err)
	}

	if !strings.Contains(collector.contentOf(ChatEventReasoning), thinking) {
		t.Fatal("思考过程应通过 reasoning 事件转发（否则推理模型思考期间界面一片空白）")
	}
	if strings.Contains(collector.contentOf(ChatEventDelta), thinking) {
		t.Fatal("思考过程不得混进回答正文")
	}

	if len(fixture.messages.created) != 2 {
		t.Fatalf("期望落库用户消息 + 助手消息共 2 条，实际 %d 条", len(fixture.messages.created))
	}
	assistant := fixture.messages.created[1]
	if strings.Contains(assistant.Content, thinking) {
		t.Fatal("思考过程不得落库进助手消息")
	}
	if assistant.Content != "请先断开电源。" {
		t.Fatalf("助手消息应只包含回答正文，实际 %q", assistant.Content)
	}
	if fixture.messages.created[0].Role != model.MessageRoleUser {
		t.Fatal("第一条落库消息应是用户提问")
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 4.1：增量不攒批
// ─────────────────────────────────────────────────────────────

// 模型的第二块被门控住：只有实现"收到一块就立刻发一次 delta"，
// 第一段增量才会早于第二块发出。这正是 D13 里 SSE 被中间层攒批要防住的行为。
func TestChat_StreamsEachChunkImmediately(t *testing.T) {
	release := make(chan struct{})
	chatModel := &testChatModel{
		chunks: []*schema.Message{
			schema.AssistantMessage("第一段", nil),
			schema.AssistantMessage("第二段", nil),
		},
		release: release,
	}
	fixture := newChatFixture(model.AppModeChat, chatModel)

	var deltaBeforeSecondChunk atomic.Bool
	collector := &eventCollector{}
	collector.onDelta = func() {
		deltaBeforeSecondChunk.Store(!chatModel.secondSent.Load())
		close(release)
	}

	if err := fixture.service.Chat(context.Background(), ChatInput{
		AppID: 4, Question: "讲两段",
	}, collector.emit); err != nil {
		t.Fatalf("问答失败: %v", err)
	}

	if !deltaBeforeSecondChunk.Load() {
		t.Fatal("第一段增量应在模型产出第二块之前就发出（否则就是被攒批了）")
	}
	if got := collector.contentOf(ChatEventDelta); got != "第一段第二段" {
		t.Fatalf("增量拼接结果不符：%q", got)
	}
	if collector.deltaes != 2 {
		t.Fatalf("两块内容应产生两个 delta 事件，实际 %d 个", collector.deltaes)
	}
}

// ─────────────────────────────────────────────────────────────
// 任务 2.4：失败路径
// ─────────────────────────────────────────────────────────────

func TestChat_ModelErrorsArePropagated(t *testing.T) {
	t.Run("取模型失败", func(t *testing.T) {
		sentinel := errors.New("没有可用的对话模型")
		fixture := newChatFixture(model.AppModeChat, &testChatModel{})
		fixture.resolver.err = sentinel

		err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "你好",
		}, (&eventCollector{}).emit)
		if !errors.Is(err, sentinel) {
			t.Fatalf("期望模型解析错误被上抛，实际: %v", err)
		}
		if len(fixture.messages.created) != 1 {
			t.Fatal("用户消息仍应先落库（用户说过的话不能因为模型失败而丢失）")
		}
	})

	t.Run("生成失败", func(t *testing.T) {
		sentinel := errors.New("上游 500")
		fixture := newChatFixture(model.AppModeChat, &testChatModel{streamErr: sentinel})

		err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "你好",
		}, (&eventCollector{}).emit)
		if err == nil {
			t.Fatal("模型失败必须报错，不能静默结束")
		}
		// 错误由 Eino 的节点包装（NodeRunError）后上抛：这里断言的是"原始错误没有丢"，
		// 排障时要能看出是模型侧的问题而不是链路内部的问题。
		if !errors.Is(err, sentinel) {
			t.Fatalf("原始错误应保留在错误链里（否则无法判断失败来自哪一层），实际: %v", err)
		}
	})

	t.Run("检索失败不落助手消息", func(t *testing.T) {
		sentinel := errors.New("向量库不可用")
		fixture := newChatFixture(model.AppModeRAG, &testChatModel{
			chunks: []*schema.Message{schema.AssistantMessage("不该出现", nil)},
		})
		fixture.rag.err = sentinel

		err := fixture.service.Chat(context.Background(), ChatInput{
			AppID: 4, Question: "怎么更换电池", DatasetID: 7,
		}, (&eventCollector{}).emit)
		if err == nil {
			t.Fatal("检索失败时应报错，不能拿空资料硬答")
		}
		for _, message := range fixture.messages.created {
			if message.Role == model.MessageRoleAssistant {
				t.Fatal("检索失败不应写入助手消息")
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────
// 参数校验
// ─────────────────────────────────────────────────────────────

func TestChat_EmptyQuestionRejected(t *testing.T) {
	fixture := newChatFixture(model.AppModeChat, &testChatModel{})
	err := fixture.service.Chat(context.Background(), ChatInput{
		AppID: 4, Question: "   ",
	}, (&eventCollector{}).emit)

	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空问题应返回参数错误，实际: %v", err)
	}
	if len(fixture.messages.created) != 0 {
		t.Fatal("空问题不应写库")
	}
}
