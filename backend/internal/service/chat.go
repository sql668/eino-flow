package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"einoflow-backend/internal/engine/chat"
	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/engine/runtime"
	"einoflow-backend/internal/engine/runtime/tools"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

const (
	// maxQuestionRunes 限制单轮提问长度：过长的问题既没有检索意义，也会挤占提示词预算。
	maxQuestionRunes = 4000
	// maxHistoryMessages 是多轮对话带上的历史条数上限。
	// 不限制的话，长会话会把上下文塞满，模型开始「忘掉」最前面的资料。
	maxHistoryMessages = 20
	// maxCitationRunes 是单条引用片段放进提示词的最大长度，
	// 防止某段特别长时把提示词预算吃光。
	maxCitationRunes = 2000
	// maxToolArgumentsRunes / maxToolResultRunes 是工具调用记录与 SSE 事件的摘要长度。
	// 完整结果已通过 citations 与分段表可回查，消息表里只留可解释的摘要。
	maxToolArgumentsRunes = 400
	maxToolResultRunes    = 800
	// conversationTitleRunes 是自动生成的会话标题长度。
	conversationTitleRunes = 30
)

// ChatInput 是一次问答请求。
type ChatInput struct {
	AppID uint64
	// ConversationID 为 0 时自动新建会话。
	ConversationID uint64
	Question       string
	// DatasetID 指定知识库；RAG 形态必填。
	DatasetID uint64
	// TopK 为 0 时用配置默认值。
	TopK int
}

// 流式事件类型。
const (
	ChatEventCitations = "citations"
	// ChatEventReasoning 是推理模型的「思考过程」增量。
	//
	// 单独一种事件而不是混进 delta：思考内容不进正文、不落库，
	// 它只解决「推理模型思考的十几秒里界面一片空白」。
	ChatEventReasoning = "reasoning"
	ChatEventDelta     = "delta"
	// ChatEventToolCall / ChatEventToolResult 是工具调用的开始与结果事件。
	// 它们必须在该步文本之前到达：用户先看到"正在查资料"，再看到回答。
	ChatEventToolCall   = "tool_call"
	ChatEventToolResult = "tool_result"
	ChatEventDone       = "done"
)

// ChatEvent 是流式问答的一个事件。
//
// 类型固定四种，前端按 type 分支处理，不需要解析文本去猜：
// citations（引用片段，出第一段回答之前先给一次）、reasoning（思考过程增量）、
// delta（回答增量）、done（结束与统计）。
type ChatEvent struct {
	Type string
	// Citations 在 Type=citations 时有值。
	Citations []model.MessageCitation
	// Content 在 Type=delta / reasoning 时有值。
	Content string
	// Tool 在 Type=tool_call / tool_result 时有值。
	Tool *ChatToolEvent
	// 以下字段在 Type=done 时有值。
	ConversationID uint64
	MessageID      uint64
	ModelName      string
	LatencyMS      int
}

// ChatToolEvent 是一次工具调用的对外事件与落库摘要。
type ChatToolEvent struct {
	Round      int    `json:"round"`
	CallID     string `json:"callId"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments,omitempty"`
	Result     string `json:"result,omitempty"`
	Failed     bool   `json:"failed,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	DurationMS int    `json:"durationMs"`
}

// ChatService 承载问答与流式输出。
type ChatService struct {
	apps          repository.AppRepository
	conversations repository.ConversationRepository
	messages      repository.MessageRepository
	models        enginemodel.Resolver
	// retrieval 是检索能力：包装成接口后注入 Eino 图，留痕与重排策略仍留在 service。
	retrieval *RetrievalService
	// agentMaxToolRounds 是智能体单次问答的工具调用上限（来自配置）。
	agentMaxToolRounds int
}

// NewChatService 构造问答服务。
func NewChatService(
	apps repository.AppRepository,
	conversations repository.ConversationRepository,
	messages repository.MessageRepository,
	models enginemodel.Resolver,
	retrieval *RetrievalService,
	agentMaxToolRounds int,
) *ChatService {
	return &ChatService{
		apps:               apps,
		conversations:      conversations,
		messages:           messages,
		models:             models,
		retrieval:          retrieval,
		agentMaxToolRounds: agentMaxToolRounds,
	}
}

// Chat 执行一次问答，事件通过回调逐块推送。
//
// 用回调而不是返回 channel：HTTP 层需要在同一个请求上下文里写流，
// 回调能天然保证「事件写出」与「链路推进」的先后顺序，也避免客户端断开时 channel 泄漏。
func (s *ChatService) Chat(ctx context.Context, input ChatInput, emit func(ChatEvent) error) error {
	app, err := s.apps.GetByID(ctx, input.AppID)
	if err != nil {
		return translateError(err)
	}

	question := strings.TrimSpace(input.Question)
	if question == "" {
		return fmt.Errorf("%w: 问题不能为空", ErrInvalidInput)
	}
	if len([]rune(question)) > maxQuestionRunes {
		return fmt.Errorf("%w: 问题过长（最多 %d 个字符）", ErrInvalidInput, maxQuestionRunes)
	}

	// 按应用形态选择链路：rag 走知识库，chat 是纯对话，agent 走 ReAct。
	// workflow 属于 V1.0，仍明确拒绝而不是退化成纯对话——
	// 退化成「什么都能答」的聊天机器人，会让人以为编排在跑。
	useRetrieval := false
	useAgent := false
	switch app.Mode {
	case model.AppModeRAG:
		if input.DatasetID == 0 {
			return fmt.Errorf("%w: RAG 问答必须指定知识库（datasetId）", ErrInvalidInput)
		}
		useRetrieval = true
	case model.AppModeChat:
	case model.AppModeAgent:
		useAgent = true
	default:
		return fmt.Errorf("%w: 应用形态 %s 的对话尚未支持（当前支持 chat / rag / agent）",
			ErrInvalidInput, app.Mode)
	}

	conversationID, err := s.ensureConversation(ctx, input, question)
	if err != nil {
		return err
	}

	// 先读历史再落用户消息：否则本轮问题会同时出现在 history 与 Question 里，
	// 模型会看到同一句话两遍（RAG 链路也会受影响）。
	history, err := s.loadHistory(ctx, conversationID)
	if err != nil {
		return err
	}

	// 先落用户消息：模型调用可能很慢甚至失败，但用户说过的话必须留下来。
	if err := s.messages.Create(ctx, &model.Message{
		ConversationID: conversationID,
		Role:           model.MessageRoleUser,
		Content:        question,
	}); err != nil {
		return err
	}

	if useAgent {
		return s.chatAgent(ctx, input, conversationID, question, history, emit)
	}

	chatModel, err := s.models.ChatModel(ctx, enginemodel.ScopeRef{AppID: input.AppID})
	if err != nil {
		return translateError(err)
	}

	deps := chat.Deps{ChatModel: chatModel}
	if useRetrieval {
		deps.Retriever = chatRetriever{service: s, datasetID: input.DatasetID}
	}
	chain, err := chat.NewRAGChain(ctx, deps)
	if err != nil {
		return err
	}

	citations := make([]chat.Context, 0, 8)
	started := time.Now()
	stream, err := chain.Stream(ctx, &chat.Request{
		Question:  question,
		Scope:     chat.Scope{AppID: input.AppID, DatasetID: input.DatasetID, TopK: input.TopK},
		History:   history,
		Citations: &citations,
	})
	if err != nil {
		return translateError(err)
	}
	defer stream.Close()

	// 引用事件在第一个文本块之前发出：此时检索节点已经跑完，前端可以先渲染来源卡片。
	citationsSent := useRetrieval == false
	var answer strings.Builder
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return fmt.Errorf("生成回答失败: %w", recvErr)
		}
		if !citationsSent {
			citationsSent = true
			if err := emit(ChatEvent{Type: ChatEventCitations, Citations: toMessageCitations(citations)}); err != nil {
				return err
			}
		}
		if chunk == nil {
			continue
		}
		// 思考过程先于回答出现（qwen3 这类推理模型）：转发它，界面才有东西可显示。
		// 引用事件在此之前已发出，所以「来源 + 思考」会先铺满等待的空档。
		if chunk.ReasoningContent != "" {
			if err := emit(ChatEvent{Type: ChatEventReasoning, Content: chunk.ReasoningContent}); err != nil {
				return err
			}
		}
		if chunk.Content == "" {
			continue
		}
		answer.WriteString(chunk.Content)
		if err := emit(ChatEvent{Type: ChatEventDelta, Content: chunk.Content}); err != nil {
			// 写不出去通常意味着客户端断开：直接返回，停止后续生成，不白烧算力。
			return err
		}
	}

	// 模型没有产出任何文本也要把引用发一次（例如它直接结束）。
	if !citationsSent {
		if err := emit(ChatEvent{Type: ChatEventCitations, Citations: toMessageCitations(citations)}); err != nil {
			return err
		}
	}

	assistant := &model.Message{
		ConversationID: conversationID,
		Role:           model.MessageRoleAssistant,
		Content:        answer.String(),
		Citations:      toMessageCitations(citations),
		LatencyMS:      int(time.Since(started).Milliseconds()),
		TokenCount:     estimateTokenCount(answer.String()),
	}
	if err := s.messages.Create(ctx, assistant); err != nil {
		return err
	}

	return emit(ChatEvent{
		Type:           ChatEventDone,
		ConversationID: conversationID,
		MessageID:      assistant.ID,
		ModelName:      describeChatModel(ctx, s.models, input.AppID),
		LatencyMS:      assistant.LatencyMS,
	})
}

// chatAgent 执行一次智能体问答。
//
// 工具集按本次问答组装：指定知识库才注册检索工具，计算工具始终可用
// （见 change add-react-agent-chat 的 D2 / D7）。
func (s *ChatService) chatAgent(
	ctx context.Context,
	input ChatInput,
	conversationID uint64,
	question string,
	history []*schema.Message,
	emit func(ChatEvent) error,
) error {
	toolModel, err := s.models.ToolCallingChatModel(ctx, enginemodel.ScopeRef{AppID: input.AppID})
	if err != nil {
		return translateError(err)
	}

	citations := &citationCollector{}
	registry := runtime.NewRegistry()
	calculator, err := tools.NewCalculator()
	if err != nil {
		return err
	}
	if err := registry.Register(ctx, calculator); err != nil {
		return err
	}

	datasetIDs := make([]uint64, 0, 1)
	if input.DatasetID != 0 {
		datasetIDs = append(datasetIDs, input.DatasetID)
		retrievalTool, err := tools.NewKnowledgeRetrievalTool(agentSearcher{
			service:   s,
			citations: citations,
		})
		if err != nil {
			return err
		}
		if err := registry.Register(ctx, retrievalTool); err != nil {
			return err
		}
	}

	names, err := tools.ToolNamesForChat(datasetIDs)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	subset, err := registry.Select(names...)
	if err != nil {
		return err
	}

	recorder := &toolCallRecorder{}
	observer := func(_ context.Context, event runtime.ToolEvent) error {
		recorder.record(event)
		payload := toChatToolEvent(event)
		switch event.Kind {
		case runtime.ToolEventCall:
			return emit(ChatEvent{Type: ChatEventToolCall, Tool: &payload})
		case runtime.ToolEventResult:
			return emit(ChatEvent{Type: ChatEventToolResult, Tool: &payload})
		default:
			return nil
		}
	}

	agentRuntime, err := runtime.NewAgent(ctx, runtime.AgentConfig{
		Model:         toolModel,
		Tools:         subset.Tools(),
		MaxToolRounds: s.agentMaxToolRounds,
		Observer:      observer,
	})
	if err != nil {
		return err
	}

	messages := make([]*schema.Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, schema.UserMessage(question))

	started := time.Now()
	stream, err := agentRuntime.Stream(ctx, messages)
	if err != nil {
		return err
	}
	defer stream.Close()

	citationsSent := false
	var answer strings.Builder
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			if runtime.IsToolRoundLimit(recvErr) {
				// 达到上限不是"服务坏了"：把已产出的内容留下，并明确说明。
				note := fmt.Sprintf("\n\n（已达到工具调用上限 %d 次，以上为当前可用信息。）",
					s.agentMaxToolRounds)
				answer.WriteString(note)
				if err := emit(ChatEvent{Type: ChatEventDelta, Content: note}); err != nil {
					return err
				}
				break
			}
			return fmt.Errorf("生成回答失败: %w", recvErr)
		}
		if chunk == nil {
			continue
		}
		if chunk.ReasoningContent != "" {
			if err := emit(ChatEvent{Type: ChatEventReasoning, Content: chunk.ReasoningContent}); err != nil {
				return err
			}
		}
		if chunk.Content == "" {
			continue
		}
		// 引用必须早于第一段回答文本：工具已经跑完，用户可以先把来源看到。
		if !citationsSent {
			if err := emitAgentCitations(emit, citations, &citationsSent); err != nil {
				return err
			}
		}
		answer.WriteString(chunk.Content)
		if err := emit(ChatEvent{Type: ChatEventDelta, Content: chunk.Content}); err != nil {
			return err
		}
	}

	// 模型没有产出文本（例如直接结束）时，也把已经检索到的引用发出去。
	if !citationsSent {
		if err := emitAgentCitations(emit, citations, &citationsSent); err != nil {
			return err
		}
	}

	assistant := &model.Message{
		ConversationID: conversationID,
		Role:           model.MessageRoleAssistant,
		Content:        answer.String(),
		Citations:      toMessageCitations(citations.list()),
		ToolCalls:      recorder.list(),
		LatencyMS:      int(time.Since(started).Milliseconds()),
		TokenCount:     estimateTokenCount(answer.String()),
	}
	if err := s.messages.Create(ctx, assistant); err != nil {
		return err
	}

	return emit(ChatEvent{
		Type:           ChatEventDone,
		ConversationID: conversationID,
		MessageID:      assistant.ID,
		ModelName:      describeChatModel(ctx, s.models, input.AppID),
		LatencyMS:      assistant.LatencyMS,
	})
}

// emitAgentCitations 在确有引用时发出一次 citations 事件。
func emitAgentCitations(
	emit func(ChatEvent) error, citations *citationCollector, sent *bool,
) error {
	contexts := citations.list()
	if len(contexts) == 0 {
		return nil
	}
	*sent = true
	return emit(ChatEvent{Type: ChatEventCitations, Citations: toMessageCitations(contexts)})
}

// agentSearcher 把检索服务适配成工具层的 Searcher。
//
// 顺手把命中片段收集成引用：智能体没有独立的检索节点，
// 引用只能从工具结果里来（否则前端拿不到出处卡片）。
type agentSearcher struct {
	service   *ChatService
	citations *citationCollector
}

func (s agentSearcher) Search(
	ctx context.Context, datasetID uint64, query string,
) ([]tools.RetrievalHit, error) {
	result, err := s.service.retrieval.Search(ctx, SearchInput{
		DatasetID: datasetID,
		Query:     query,
	})
	if err != nil {
		return nil, err
	}

	s.citations.add(hitsToChatContexts(result.Hits))
	hits := make([]tools.RetrievalHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, tools.RetrievalHit{
			ChunkID:      hit.ChunkID,
			SegmentID:    hit.SegmentID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Content:      hit.Content,
			Page:         hit.Page,
			TitlePath:    hit.TitlePath,
			Kind:         hit.Kind,
		})
	}
	return hits, nil
}

// citationCollector 汇总一次智能体问答里的引用片段。
//
// 同一片段可能被多轮检索命中，按片段 ID 去重；编号在输出时统一重排，
// 保证回答里的 [n] 与引用列表始终对得上。
type citationCollector struct {
	mu    sync.Mutex
	order []chat.Context
	seen  map[uint64]struct{}
}

func (c *citationCollector) add(items []chat.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = make(map[uint64]struct{})
	}
	for _, item := range items {
		if item.SegmentID != 0 {
			if _, exists := c.seen[item.SegmentID]; exists {
				continue
			}
			c.seen[item.SegmentID] = struct{}{}
		}
		c.order = append(c.order, item)
	}
}

func (c *citationCollector) list() []chat.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]chat.Context, len(c.order))
	copy(out, c.order)
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

// toolCallRecorder 把工具事件整理成可落库的调用记录。
//
// 以 Round 为键：同一轮可能并行调用多个工具，CallID 理论上唯一，
// 但 Ollama 等实现可能不返回 ID，Round 才是运行时可保证的唯一序号。
type toolCallRecorder struct {
	mu      sync.Mutex
	byRound map[int]*model.MessageToolCall
	order   []int
}

func (r *toolCallRecorder) record(event runtime.ToolEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byRound == nil {
		r.byRound = make(map[int]*model.MessageToolCall)
	}
	record, exists := r.byRound[event.Round]
	if !exists {
		record = &model.MessageToolCall{Round: event.Round}
		r.byRound[event.Round] = record
		r.order = append(r.order, event.Round)
	}
	record.CallID = event.CallID
	record.Name = event.Name
	record.Skipped = event.Skipped
	switch event.Kind {
	case runtime.ToolEventCall:
		record.Arguments = truncateRunes(event.Arguments, maxToolArgumentsRunes)
	case runtime.ToolEventResult:
		record.Result = truncateRunes(event.Result, maxToolResultRunes)
		record.Failed = event.Failed
		record.DurationMS = int(event.Duration.Milliseconds())
	}
}

func (r *toolCallRecorder) list() []model.MessageToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	rounds := make([]int, len(r.order))
	copy(rounds, r.order)
	sort.Ints(rounds)

	out := make([]model.MessageToolCall, 0, len(rounds))
	for _, round := range rounds {
		record, exists := r.byRound[round]
		if !exists {
			continue
		}
		out = append(out, *record)
	}
	return out
}

// toChatToolEvent 把运行时事件转成对外事件（摘要长度与落库一致）。
func toChatToolEvent(event runtime.ToolEvent) ChatToolEvent {
	return ChatToolEvent{
		Round:      event.Round,
		CallID:     event.CallID,
		Name:       event.Name,
		Arguments:  truncateRunes(event.Arguments, maxToolArgumentsRunes),
		Result:     truncateRunes(event.Result, maxToolResultRunes),
		Failed:     event.Failed,
		Skipped:    event.Skipped,
		DurationMS: int(event.Duration.Milliseconds()),
	}
}

// chatRetriever 把检索服务适配成 Eino 图里的检索节点。
//
// 适配而不是让图直接调 RetrievalService：图只需要「给我片段」，
// 而留痕、重排开关、分数落库属于业务语义，不该进图。
type chatRetriever struct {
	service   *ChatService
	datasetID uint64
}

// Retrieve 实现 chat.Retriever：走一次完整的两阶段检索（含留痕）。
func (r chatRetriever) Retrieve(ctx context.Context, scope chat.Scope, query string) ([]chat.Context, error) {
	result, err := r.service.retrieval.Search(ctx, SearchInput{
		DatasetID: r.datasetID,
		Query:     query,
		TopK:      scope.TopK,
	})
	if err != nil {
		return nil, err
	}

	return hitsToChatContexts(result.Hits), nil
}

// hitsToChatContexts 把检索命中转成链路上下文（含引用编号与正文截断）。
//
// RAG 链路与智能体检索工具共用这一份转换：引用编号、正文长度与溯源字段
// 只要有一处不一致，界面上的 [n] 就会和来源对不上。
func hitsToChatContexts(hits []rag.Hit) []chat.Context {
	contexts := make([]chat.Context, 0, len(hits))
	for i, hit := range hits {
		contexts = append(contexts, chat.Context{
			Index:        i + 1,
			SegmentID:    hit.SegmentID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Page:         hit.Page,
			TitlePath:    hit.TitlePath,
			Kind:         hit.Kind,
			Content:      truncateRunes(hit.Content, maxCitationRunes),
			// 引用上下文里只带排序分：它是决定「第 N 条引用」的那个数，
			// 两路原始分属于检索调试信息（检索留痕页展示），不往引用里塞。
			Score:       hit.Score,
			RerankScore: hit.RerankScore,
		})
	}
	return contexts
}

// ensureConversation 取回或新建会话。
func (s *ChatService) ensureConversation(ctx context.Context, input ChatInput, question string) (uint64, error) {
	if input.ConversationID != 0 {
		conversation, err := s.conversations.GetByID(ctx, input.ConversationID)
		if err != nil {
			return 0, translateError(err)
		}
		// 会话必须属于该应用：否则 A 应用的提问会写进 B 应用的会话里。
		if conversation.AppID != input.AppID {
			return 0, fmt.Errorf("%w: 会话 %d 不属于应用 %d",
				ErrInvalidInput, input.ConversationID, input.AppID)
		}
		return conversation.ID, nil
	}

	// 用问题开头做标题：会话列表里能一眼认出是哪次对话。
	title := []rune(question)
	if len(title) > conversationTitleRunes {
		title = title[:conversationTitleRunes]
	}
	conversation := &model.Conversation{AppID: input.AppID, Title: string(title)}
	if err := s.conversations.Create(ctx, conversation); err != nil {
		return 0, err
	}
	return conversation.ID, nil
}

// loadHistory 读取最近若干条消息作为多轮上下文。
//
// 只带角色与正文、不带引用：历史里的来源属于展示层，
// 塞进提示词既占预算，也会让模型把「上一次的资料」当成这一次的依据。
func (s *ChatService) loadHistory(ctx context.Context, conversationID uint64) ([]*schema.Message, error) {
	items, err := s.messages.ListRecent(ctx, conversationID, maxHistoryMessages)
	if err != nil {
		return nil, err
	}

	history := make([]*schema.Message, 0, len(items))
	for _, message := range items {
		switch message.Role {
		case model.MessageRoleUser:
			history = append(history, schema.UserMessage(message.Content))
		case model.MessageRoleAssistant:
			history = append(history, schema.AssistantMessage(message.Content, nil))
		default:
			// system 由链路自己控制、tool 调用尚未落地，都不进历史。
		}
	}
	return history, nil
}

// toMessageCitations 把链路里的片段转成落库与对外展示的结构。
func toMessageCitations(contexts []chat.Context) []model.MessageCitation {
	citations := make([]model.MessageCitation, 0, len(contexts))
	for _, item := range contexts {
		citations = append(citations, model.MessageCitation{
			Index:        item.Index,
			SegmentID:    item.SegmentID,
			DocumentID:   item.DocumentID,
			DocumentName: item.DocumentName,
			Page:         item.Page,
			TitlePath:    nonNilTitlePath(item.TitlePath),
			Score:        item.Score,
			RerankScore:  item.RerankScore,
		})
	}
	return citations
}

// nonNilTitlePath 把 nil 标题路径规范成空切片。
//
// 与切片时同样的理由：citations 是 NOT NULL 的 jsonb 列，
// json.Marshal 对 nil 切片输出 null，直接写入会违反非空约束。
func nonNilTitlePath(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

// estimateTokenCount 估算 token 数。
//
// 与切片时口径一致（按字符数），在有真实分词器之前只用于大致观测；
// 台账已记为技术债，换分词器时这里同步替换。
func estimateTokenCount(text string) int {
	return len([]rune(text))
}

// describeChatModel 取当前生效的对话模型名，仅用于响应里的展示字段。
func describeChatModel(ctx context.Context, resolver enginemodel.Resolver, appID uint64) string {
	chatModel, err := resolver.ChatModel(ctx, enginemodel.ScopeRef{AppID: appID})
	if err != nil || chatModel == nil {
		return ""
	}
	// Eino 的接口没有「模型名」方法，用类型断言取我们自己的实现；
	// 取不到就留空，不让展示字段把主流程拖下水。
	if named, ok := chatModel.(interface{ Name() string }); ok {
		return named.Name()
	}
	return ""
}
