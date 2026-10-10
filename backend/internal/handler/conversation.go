package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// conversationHandler 承载会话与消息的 HTTP 处理。
type conversationHandler struct {
	conversations *service.ConversationService
}

// registerConversationRoutes 注册 /api/v1 下的会话与消息路由。
func registerConversationRoutes(group *route.RouterGroup, conversations *service.ConversationService) {
	h := &conversationHandler{conversations: conversations}
	group.POST("/conversations", h.create)
	group.GET("/conversations", h.list)
	group.GET("/conversations/:id", h.get)
	group.PUT("/conversations/:id", h.update)
	group.DELETE("/conversations/:id", h.delete)
	// 消息挂在会话下：语义上消息不能脱离会话存在，路径也体现这层从属关系。
	group.POST("/conversations/:id/messages", h.appendMessage)
	group.GET("/conversations/:id/messages", h.listMessages)
}

type createConversationRequest struct {
	AppID uint64 `json:"appId"`
	Title string `json:"title"`
}

type updateConversationRequest struct {
	Title *string `json:"title"`
}

type conversationResponse struct {
	ID        uint64 `json:"id"`
	AppID     uint64 `json:"appId"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type conversationListResponse struct {
	Items    []conversationResponse `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

type appendMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageResponse struct {
	ID             uint64 `json:"id"`
	ConversationID uint64 `json:"conversationId"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	// Citations 是助手消息引用的片段：历史会话里回看时仍能看到来源（可溯源要求）。
	Citations  []model.MessageCitation `json:"citations,omitempty"`
	TokenCount int                     `json:"tokenCount"`
	LatencyMS  int                     `json:"latencyMs"`
	CreatedAt  string                  `json:"createdAt"`
}

type messageListResponse struct {
	Items    []messageResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

func newConversationResponse(c *model.Conversation) conversationResponse {
	return conversationResponse{
		ID:        c.ID,
		AppID:     c.AppID,
		Title:     c.Title,
		CreatedAt: formatTime(c.CreatedAt),
		UpdatedAt: formatTime(c.UpdatedAt),
	}
}

func newMessageResponse(m *model.Message) messageResponse {
	return messageResponse{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		Role:           string(m.Role),
		Content:        m.Content,
		Citations:      m.Citations,
		TokenCount:     m.TokenCount,
		LatencyMS:      m.LatencyMS,
		CreatedAt:      formatTime(m.CreatedAt),
	}
}

func (h *conversationHandler) create(ctx context.Context, c *app.RequestContext) {
	var req createConversationRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	created, err := h.conversations.Create(ctx, service.CreateConversationInput{
		AppID: req.AppID,
		Title: req.Title,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, OK(newConversationResponse(created)))
}

func (h *conversationHandler) list(ctx context.Context, c *app.RequestContext) {
	appID, _ := strconv.ParseUint(c.Query("appId"), 10, 64)
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.conversations.List(ctx, service.ListConversationsInput{
		AppID:    appID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]conversationResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newConversationResponse(item))
	}
	c.JSON(consts.StatusOK, OK(conversationListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}

func (h *conversationHandler) get(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	conversation, err := h.conversations.Get(ctx, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newConversationResponse(conversation)))
}

func (h *conversationHandler) update(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req updateConversationRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	updated, err := h.conversations.Update(ctx, id, service.UpdateConversationInput{Title: req.Title})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newConversationResponse(updated)))
}

func (h *conversationHandler) delete(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	if err := h.conversations.Delete(ctx, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

func (h *conversationHandler) appendMessage(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req appendMessageRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	message, err := h.conversations.AppendMessage(ctx, service.AppendMessageInput{
		ConversationID: id,
		Role:           model.MessageRole(req.Role),
		Content:        req.Content,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, OK(newMessageResponse(message)))
}

func (h *conversationHandler) listMessages(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.conversations.ListMessages(ctx, service.ListMessagesInput{
		ConversationID: id,
		Page:           page,
		PageSize:       pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]messageResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newMessageResponse(item))
	}
	c.JSON(consts.StatusOK, OK(messageListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}
