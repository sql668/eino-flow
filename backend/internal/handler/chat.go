package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/http1/resp"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/service"
)

// chatHandler 承载问答与流式输出。
type chatHandler struct {
	chats *service.ChatService
}

// registerChatRoutes 注册问答路由。
//
// 挂在应用下（/apps/:id/chat）：问答永远发生在某个应用的配置之下
// （形态决定是否检索、模型按应用作用域解析）。
func registerChatRoutes(group *route.RouterGroup, chats *service.ChatService) {
	h := &chatHandler{chats: chats}
	group.POST("/apps/:id/chat", h.chat)
}

type chatRequest struct {
	// ConversationID 为空表示新建会话。
	ConversationID uint64 `json:"conversationId"`
	Question       string `json:"question"`
	// DatasetID 是 RAG 形态必须指定的知识库。
	DatasetID uint64 `json:"datasetId"`
	TopK      int    `json:"topK"`
}

// citationPayload 是流式响应里的引用片段。
type citationPayload struct {
	Index        int      `json:"index"`
	SegmentID    uint64   `json:"segmentId"`
	DocumentID   uint64   `json:"documentId"`
	DocumentName string   `json:"documentName"`
	Page         int      `json:"page"`
	TitlePath    []string `json:"titlePath"`
	Score        float32  `json:"score"`
	RerankScore  *float32 `json:"rerankScore,omitempty"`
}

// chat 以 SSE 方式流式返回问答结果。
//
// 事件类型：citations（引用片段）→ delta（增量文本，可多次）→ done（结束与统计）。
// 之所以先发引用再发文本：前端可以在回答开始时就把「来源卡片」渲染出来，
// 而不是等整段答完才知道引用的是什么。
func (h *chatHandler) chat(ctx context.Context, c *app.RequestContext) {
	appID, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req chatRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	// 先做一次入参预检：SSE 一旦开始写，就没法再返回 4xx 了（响应头已经发出），
	// 所以能在写流之前发现的错误必须先发现。
	if req.Question == "" {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "question 不能为空"))
		return
	}

	// 切换成分块流式写：Hertz 在第一次写入时才把响应头写出去，
	// 因此下面的 Header.Set 必须在这之前完成。
	// 说明：结束分块由框架在释放 writer 时补（不要自己调 Finalize）。
	writer := resp.NewChunkedBodyWriter(&c.Response, c.GetWriter())
	c.Response.HijackWriter(writer)
	c.Response.Header.Set("Content-Type", "text/event-stream; charset=utf-8")
	c.Response.Header.Set("Cache-Control", "no-cache")
	c.Response.Header.Set("Connection", "keep-alive")
	// 关掉 Nginx / 隧道侧的缓冲，否则流式会变成「最后一次性吐出来」。
	c.Response.Header.Set("X-Accel-Buffering", "no")

	emit := func(event service.ChatEvent) error {
		return writeSSE(writer, event)
	}

	if err := h.chats.Chat(ctx, service.ChatInput{
		AppID:          appID,
		ConversationID: req.ConversationID,
		Question:       req.Question,
		DatasetID:      req.DatasetID,
		TopK:           req.TopK,
	}, emit); err != nil {
		// 流已经开始，无法再改状态码：把错误作为事件发出去，前端据此提示。
		// 同时打日志，便于服务端排查（前端只能看到一句话）。
		log.Printf("问答失败（应用 %d）: %v", appID, err)
		_ = writeSSEEvent(writer, "error", map[string]string{"message": err.Error()})
		return
	}
}

// writeSSE 把业务事件编码成 SSE 帧。
func writeSSE(writer network.ExtWriter, event service.ChatEvent) error {
	switch event.Type {
	case service.ChatEventCitations:
		payload := make([]citationPayload, 0, len(event.Citations))
		for _, item := range event.Citations {
			payload = append(payload, citationPayload{
				Index:        item.Index,
				SegmentID:    item.SegmentID,
				DocumentID:   item.DocumentID,
				DocumentName: item.DocumentName,
				Page:         item.Page,
				TitlePath:    item.TitlePath,
				Score:        item.Score,
				RerankScore:  item.RerankScore,
			})
		}
		return writeSSEEvent(writer, service.ChatEventCitations, map[string]any{"citations": payload})
	case service.ChatEventDelta:
		return writeSSEEvent(writer, service.ChatEventDelta, map[string]string{"content": event.Content})
	case service.ChatEventReasoning:
		// 思考过程与回答同构（都是一段增量文本），单独一种事件类型是为了让前端
		// 能把它渲染成「思考中」而不是混进正文。
		return writeSSEEvent(writer, service.ChatEventReasoning, map[string]string{"content": event.Content})
	case service.ChatEventToolCall:
		if event.Tool == nil {
			return fmt.Errorf("工具调用事件缺少负载")
		}
		return writeSSEEvent(writer, service.ChatEventToolCall, event.Tool)
	case service.ChatEventToolResult:
		if event.Tool == nil {
			return fmt.Errorf("工具结果事件缺少负载")
		}
		return writeSSEEvent(writer, service.ChatEventToolResult, event.Tool)
	case service.ChatEventDone:
		return writeSSEEvent(writer, service.ChatEventDone, map[string]any{
			"conversationId": event.ConversationID,
			"messageId":      event.MessageID,
			"model":          event.ModelName,
			"latencyMs":      event.LatencyMS,
		})
	default:
		return fmt.Errorf("未知的流式事件类型 %q", event.Type)
	}
}

// writeSSEEvent 按 SSE 规范写一帧：event 行 + data 行 + 空行。
func writeSSEEvent(writer network.ExtWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化流式事件失败: %w", err)
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	if _, err := writer.Write([]byte(frame)); err != nil {
		return fmt.Errorf("写入流式响应失败: %w", err)
	}
	// 立即 Flush：不刷的话数据会攒在缓冲区里，前端就看不到「流式」效果。
	return writer.Flush()
}
