package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"einoflow-backend/internal/engine/chat"
	enginemodel "einoflow-backend/internal/engine/model"
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
	ChatEventDone      = "done"
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
	// 以下字段在 Type=done 时有值。
	ConversationID uint64
	MessageID      uint64
	ModelName      string
	LatencyMS      int
}

// ChatService 承载问答与流式输出。
type ChatService struct {
	apps          repository.AppRepository
	conversations repository.ConversationRepository
	messages      repository.MessageRepository
	models        enginemodel.Resolver
	// retrieval 是检索能力：包装成接口后注入 Eino 图，留痕与重排策略仍留在 service。
	retrieval *RetrievalService
}

// NewChatService 构造问答服务。
func NewChatService(
	apps repository.AppRepository,
	conversations repository.ConversationRepository,
	messages repository.MessageRepository,
	models enginemodel.Resolver,
	retrieval *RetrievalService,
) *ChatService {
	return &ChatService{
		apps:          apps,
		conversations: conversations,
		messages:      messages,
		models:        models,
		retrieval:     retrieval,
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

	// 按应用形态决定是否检索：rag 走知识库，chat 是纯对话。
	// 其余形态（agent / workflow）尚未落地，明确拒绝而不是退化成纯对话——
	// 退化成「什么都能答」的聊天机器人，会让人以为编排在跑。
	useRetrieval := false
	switch app.Mode {
	case model.AppModeRAG:
		if input.DatasetID == 0 {
			return fmt.Errorf("%w: RAG 问答必须指定知识库（datasetId）", ErrInvalidInput)
		}
		useRetrieval = true
	case model.AppModeChat:
	default:
		return fmt.Errorf("%w: 应用形态 %s 的对话尚未支持（当前支持 chat / rag）",
			ErrInvalidInput, app.Mode)
	}

	conversationID, err := s.ensureConversation(ctx, input, question)
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

	chatModel, err := s.models.ChatModel(ctx, enginemodel.ScopeRef{AppID: input.AppID})
	if err != nil {
		return translateError(err)
	}

	history, err := s.loadHistory(ctx, conversationID)
	if err != nil {
		return err
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

	contexts := make([]chat.Context, 0, len(result.Hits))
	for i, hit := range result.Hits {
		content := hit.Content
		if runes := []rune(content); len(runes) > maxCitationRunes {
			content = string(runes[:maxCitationRunes]) + "…"
		}
		contexts = append(contexts, chat.Context{
			Index:        i + 1,
			SegmentID:    hit.SegmentID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Page:         hit.Page,
			TitlePath:    hit.TitlePath,
			Kind:         hit.Kind,
			Content:      content,
			// 引用上下文里只带排序分：它是决定「第 N 条引用」的那个数，
			// 两路原始分属于检索调试信息（检索留痕页展示），不往引用里塞。
			Score:       hit.Score,
			RerankScore: hit.RerankScore,
		})
	}
	return contexts, nil
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
