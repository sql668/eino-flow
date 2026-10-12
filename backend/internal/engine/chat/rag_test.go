package chat

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeChatModel 是 Eino 对话模型的测试替身。
//
// 为什么替身而不打桩 HTTP：链路里真正要验证的是「提示词怎么组装、事件怎么产出」，
// 这些与模型实现无关；打桩 HTTP 会引入端口与启动顺序，把测试变成集成测试。
// 替身同时记录收到的消息，让「资料在历史之前、问题在最后」这类顺序断言成为可能。
type fakeChatModel struct {
	// chunks 是流式输出的分块（同时用于 Generate 时拼接完整回答）。
	chunks []*schema.Message
	// streamErr / genErr 非空时模拟对应方法失败。
	streamErr error
	genErr    error

	// received 记录每次调用收到的消息列表（按调用顺序）。
	received    [][]*schema.Message
	streamCalls int
}

func (f *fakeChatModel) Generate(
	_ context.Context, input []*schema.Message, _ ...model.Option,
) (*schema.Message, error) {
	f.received = append(f.received, input)
	if f.genErr != nil {
		return nil, f.genErr
	}
	var content, reasoning strings.Builder
	for _, chunk := range f.chunks {
		content.WriteString(chunk.Content)
		reasoning.WriteString(chunk.ReasoningContent)
	}
	message := schema.AssistantMessage(content.String(), nil)
	// 思考过程与正文分开承载：调用方（service）据此发 reasoning 事件而不是 delta。
	message.ReasoningContent = reasoning.String()
	return message, nil
}

func (f *fakeChatModel) Stream(
	_ context.Context, input []*schema.Message, _ ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	f.received = append(f.received, input)
	f.streamCalls++
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return schema.StreamReaderFromArray(f.chunks), nil
}

// fakeRetriever 给出固定的片段，或模拟检索失败。
type fakeRetriever struct {
	contexts []Context
	err      error

	calls     int
	lastQuery string
	lastScope Scope
}

func (f *fakeRetriever) Retrieve(_ context.Context, scope Scope, query string) ([]Context, error) {
	f.calls++
	f.lastScope = scope
	f.lastQuery = query
	if f.err != nil {
		return nil, f.err
	}
	return f.contexts, nil
}

// newTestChain 装配一条带替身的链路。
func newTestChain(t *testing.T, retriever Retriever, chatModel model.BaseChatModel) *RAGChain {
	t.Helper()
	chain, err := NewRAGChain(context.Background(), Deps{
		Retriever:    retriever,
		ChatModel:    chatModel,
		SystemPrompt: "测试系统提示词",
	})
	if err != nil {
		t.Fatalf("装配链路失败: %v", err)
	}
	return chain
}

// drainStream 读完流并返回所有分块（同时验证不会阻塞在 Recv 上）。
func drainStream(t *testing.T, stream *schema.StreamReader[*schema.Message]) []*schema.Message {
	t.Helper()
	defer stream.Close()
	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return chunks
		}
		if err != nil {
			t.Fatalf("读取流失败: %v", err)
		}
		chunks = append(chunks, chunk)
	}
}

// joinContent 把分块的正文拼起来，模拟前端的拼接逻辑。
func joinContent(chunks []*schema.Message) string {
	var builder strings.Builder
	for _, chunk := range chunks {
		builder.WriteString(chunk.Content)
	}
	return builder.String()
}

// messagesText 把一条链路的输入提示词拍平成文本，便于断言内容。
func messagesText(messages []*schema.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

// TestRAGChain_StreamConcatenatesToModelOutput 覆盖任务 3.1：
// 正常回答时逐块产出，且拼接结果与模型输出一致。
func TestRAGChain_StreamConcatenatesToModelOutput(t *testing.T) {
	retriever := &fakeRetriever{contexts: []Context{{Index: 1, Content: "电池更换步骤"}}}
	chatModel := &fakeChatModel{chunks: []*schema.Message{
		schema.AssistantMessage("先断开", nil),
		schema.AssistantMessage("电源。", nil),
	}}
	chain := newTestChain(t, retriever, chatModel)

	citations := make([]Context, 0, 2)
	stream, err := chain.Stream(context.Background(), &Request{
		Question:  "怎么更换电池",
		Scope:     Scope{DatasetID: 7},
		Citations: &citations,
	})
	if err != nil {
		t.Fatalf("执行链路失败: %v", err)
	}

	chunks := drainStream(t, stream)
	if len(chunks) != 2 {
		t.Fatalf("期望 2 个分块（逐块产出，不攒批），实际 %d 个", len(chunks))
	}
	if got := joinContent(chunks); got != "先断开电源。" {
		t.Fatalf("拼接结果应为模型输出，实际 %q", got)
	}
}

// TestRAGChain_EmptyRetrievalStillAnswers 覆盖任务 3.2：
// 检索没命中时仍要作答，且引用为空——不能报错，也不能伪造引用。
func TestRAGChain_EmptyRetrievalStillAnswers(t *testing.T) {
	retriever := &fakeRetriever{contexts: nil}
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("资料中没有相关内容", nil)}}
	chain := newTestChain(t, retriever, chatModel)

	citations := make([]Context, 0, 2)
	message, err := chain.Invoke(context.Background(), &Request{
		Question:  "今天的天气",
		Scope:     Scope{DatasetID: 7},
		Citations: &citations,
	})
	if err != nil {
		t.Fatalf("检索为空不应报错，实际: %v", err)
	}
	if message.Content == "" {
		t.Fatal("期望仍给出回答")
	}
	if len(citations) != 0 {
		t.Fatalf("检索为空时引用应为空，实际 %d 条", len(citations))
	}
	prompt := messagesText(chatModel.received[0])
	if !strings.Contains(prompt, "没有命中任何片段") {
		t.Fatalf("空检索时应给模型明确的「无资料」提示，实际提示词:\n%s", prompt)
	}
}

// TestRAGChain_RetrievalFailurePropagates 覆盖任务 3.3：
// 检索失败必须向上传递，并且**不再调用模型**（不能拿空资料硬答）。
func TestRAGChain_RetrievalFailurePropagates(t *testing.T) {
	sentinel := errors.New("向量库不可用")
	retriever := &fakeRetriever{err: sentinel}
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("不该被调用", nil)}}
	chain := newTestChain(t, retriever, chatModel)

	_, err := chain.Invoke(context.Background(), &Request{Question: "怎么更换电池", Scope: Scope{DatasetID: 7}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("期望检索错误被原样包装上抛，实际: %v", err)
	}
	if chatModel.streamCalls != 0 || len(chatModel.received) != 0 {
		t.Fatal("检索失败后不应再调用模型")
	}
}

// TestRAGChain_CitationsCarrySource 覆盖任务 3.4：
// 引用要带回溯信息（文档、页码、标题路径、片段类型），并按检索顺序编号。
func TestRAGChain_CitationsCarrySource(t *testing.T) {
	rerank := float32(0.91)
	retriever := &fakeRetriever{contexts: []Context{
		{Index: 1, SegmentID: 11, DocumentID: 3, DocumentName: "万用表RS-12的使用.pdf", Page: 6,
			TitlePath: []string{"维护", "更换电池"}, Kind: "text", Content: "断开电源后再操作", Score: 0.74},
		{Index: 2, SegmentID: 12, DocumentID: 3, DocumentName: "万用表RS-12的使用.pdf", Page: 9,
			TitlePath: []string{"规格"}, Kind: "table", Content: "电池型号：9V", Score: 0.52, RerankScore: &rerank},
	}}
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("见资料 [1]", nil)}}
	chain := newTestChain(t, retriever, chatModel)

	citations := make([]Context, 0, 2)
	if _, err := chain.Invoke(context.Background(), &Request{
		Question:  "怎么更换电池",
		Scope:     Scope{AppID: 4, DatasetID: 7, TopK: 5},
		Citations: &citations,
	}); err != nil {
		t.Fatalf("执行链路失败: %v", err)
	}

	if len(citations) != 2 {
		t.Fatalf("期望 2 条引用，实际 %d 条", len(citations))
	}
	first := citations[0]
	if first.DocumentName != "万用表RS-12的使用.pdf" || first.Page != 6 || first.Kind != "text" {
		t.Fatalf("引用缺少溯源信息: %+v", first)
	}
	if len(first.TitlePath) != 2 || first.TitlePath[1] != "更换电池" {
		t.Fatalf("引用应带标题路径，实际 %v", first.TitlePath)
	}
	if citations[1].RerankScore == nil || *citations[1].RerankScore != rerank {
		t.Fatal("经过重排的引用应带上重排分")
	}

	// 提示词里的编号与来源描述要和引用一致，模型才能写出可核对的 [n]。
	prompt := messagesText(chatModel.received[0])
	if !strings.Contains(prompt, "[1] 来源：万用表RS-12的使用.pdf · 第 6 页") {
		t.Fatalf("提示词里缺少带出处的编号，实际:\n%s", prompt)
	}
	if !strings.Contains(prompt, "表格") {
		t.Fatalf("表格片段应在来源描述里标明类型，实际:\n%s", prompt)
	}
}

// TestRAGChain_PromptOrder 覆盖任务 4.2 的一部分：
// 资料紧跟系统提示、历史随后、本轮问题在最后——顺序是提示词效果的一部分。
func TestRAGChain_PromptOrder(t *testing.T) {
	retriever := &fakeRetriever{contexts: []Context{{Index: 1, Content: "资料正文"}}}
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("回答", nil)}}
	chain := newTestChain(t, retriever, chatModel)

	history := []*schema.Message{
		schema.UserMessage("之前的问题"),
		schema.AssistantMessage("之前的回答", nil),
	}
	if _, err := chain.Invoke(context.Background(), &Request{
		Question: "本轮问题",
		Scope:    Scope{DatasetID: 7},
		History:  history,
	}); err != nil {
		t.Fatalf("执行链路失败: %v", err)
	}

	got := chatModel.received[0]
	wantRoles := []schema.RoleType{schema.System, schema.System, schema.User, schema.Assistant, schema.User}
	if len(got) != len(wantRoles) {
		t.Fatalf("期望 %d 条消息，实际 %d 条", len(wantRoles), len(got))
	}
	for i, role := range wantRoles {
		if got[i].Role != role {
			t.Fatalf("第 %d 条消息角色应为 %s，实际 %s", i+1, role, got[i].Role)
		}
	}
	if !strings.Contains(got[1].Content, "资料正文") {
		t.Fatal("第二条应是参考资料块")
	}
	if got[len(got)-1].Content != "本轮问题" {
		t.Fatal("最后一条应是本轮问题")
	}
}

// TestRAGChain_ModelFailurePropagates 覆盖任务 2.4 的语义：
// 模型调用失败要带上下文向上传递，不能吞掉。
func TestRAGChain_ModelFailurePropagates(t *testing.T) {
	sentinel := errors.New("模型服务 500")
	chain := newTestChain(t, nil, &fakeChatModel{streamErr: sentinel})

	if _, err := chain.Stream(context.Background(), &Request{Question: "你好"}); !errors.Is(err, sentinel) {
		t.Fatalf("期望模型错误被上抛，实际: %v", err)
	}
}

// TestRAGChain_NoDatasetSkipsRetrieval 固化既有行为：
// 本次没指定知识库就不检索（纯对话），而不是拿空结果去检索一次。
func TestRAGChain_NoDatasetSkipsRetrieval(t *testing.T) {
	retriever := &fakeRetriever{contexts: []Context{{Index: 1, Content: "不该被用到"}}}
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("你好", nil)}}
	chain := newTestChain(t, retriever, chatModel)

	if _, err := chain.Invoke(context.Background(), &Request{Question: "你好"}); err != nil {
		t.Fatalf("纯对话不应报错: %v", err)
	}
	if retriever.calls != 0 {
		t.Fatalf("未指定知识库时不应触发检索，实际调用 %d 次", retriever.calls)
	}
}

// TestRAGChain_EmptyQuestionRejected 固化参数校验：空问题在进模型之前就被拒绝。
func TestRAGChain_EmptyQuestionRejected(t *testing.T) {
	chatModel := &fakeChatModel{chunks: []*schema.Message{schema.AssistantMessage("回答", nil)}}
	chain := newTestChain(t, nil, chatModel)

	if _, err := chain.Invoke(context.Background(), &Request{Question: "   "}); err == nil {
		t.Fatal("空问题应被拒绝")
	}
	if len(chatModel.received) != 0 {
		t.Fatal("空问题不应调用模型")
	}
}
