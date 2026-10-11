package handler

import (
	"context"
	"io"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// uploadFormField 是上传接口里文件字段的名字。
const uploadFormField = "file"

// defaultUploaderID 是 V0.1 的上传人占位标识（尚无登录体系）。
const defaultUploaderID = "local-user"

// documentHandler 承载文档上传与查询的 HTTP 处理。
type documentHandler struct {
	documents *service.DocumentService
}

// registerDocumentRoutes 注册文档相关路由。
// 列表挂在知识库下（文档从属于知识库），单文档操作用顶层路径更简短。
func registerDocumentRoutes(group *route.RouterGroup, documents *service.DocumentService) {
	h := &documentHandler{documents: documents}
	group.POST("/datasets/:id/documents", h.upload)
	group.GET("/datasets/:id/documents", h.list)
	group.GET("/documents/:id", h.get)
	group.DELETE("/documents/:id", h.delete)
	group.GET("/documents/:id/segments", h.listSegments)
}

// documentResponse 是文档的对外结构。
// 刻意不暴露 source —— 那是对象存储内部的 key，属于实现细节。
type documentResponse struct {
	ID        uint64 `json:"id"`
	DatasetID uint64 `json:"datasetId"`
	Name      string `json:"name"`
	FileSize  int64  `json:"fileSize"`
	Status    string `json:"status"`
	// CharCount 是解析出的正文字符数，文档列表用它展示体量。
	CharCount int `json:"charCount"`
	// ChunkMode 是最终采用的分段模式；未入库时为空。
	ChunkMode    string `json:"chunkMode"`
	SegmentCount int    `json:"segmentCount"`
	// Parsed 表示解析产物是否已就绪（工作台能否进入预览）。
	Parsed       bool   `json:"parsed"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type documentListResponse struct {
	Items    []documentResponse `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

// segmentResponse 是分段的对外结构，带溯源信息供前端展示引用来源。
type segmentResponse struct {
	ID uint64 `json:"id"`
	// ChunkID 是向量库主键：界面上「按片段复查」与引用留痕都用它。
	ChunkID    string `json:"chunkId"`
	ChunkIndex int    `json:"chunkIndex"`
	Content    string `json:"content"`
	CharCount  int    `json:"charCount"`
	// OverlapRunes 表示这条片段开头有多少字是与前一条重叠的窗口。
	OverlapRunes int      `json:"overlapRunes"`
	TokenCount   int      `json:"tokenCount"`
	Page         int      `json:"page"`
	Locator      string   `json:"locator"`
	Kind         string   `json:"kind"`
	TitlePath    []string `json:"titlePath"`
	CreatedAt    string   `json:"createdAt"`
}

type segmentListResponse struct {
	Items    []segmentResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

func newDocumentResponse(doc *model.Document) documentResponse {
	return documentResponse{
		ID:           doc.ID,
		DatasetID:    doc.DatasetID,
		Name:         doc.Name,
		FileSize:     doc.FileSize,
		Status:       string(doc.Status),
		CharCount:    doc.CharCount,
		ChunkMode:    doc.ChunkMode,
		SegmentCount: doc.SegmentCount,
		Parsed:       doc.ParsedKey != "",
		ErrorMessage: doc.ErrorMessage,
		CreatedAt:    formatTime(doc.CreatedAt),
		UpdatedAt:    formatTime(doc.UpdatedAt),
	}
}

func newSegmentResponse(segment *model.Segment) segmentResponse {
	return segmentResponse{
		ID:           segment.ID,
		ChunkID:      segment.ChunkID,
		ChunkIndex:   segment.ChunkIndex,
		Content:      segment.Content,
		CharCount:    segment.CharCount,
		OverlapRunes: segment.OverlapRunes,
		TokenCount:   segment.TokenCount,
		Page:         segment.Page,
		Locator:      segment.Locator,
		Kind:         segment.Kind,
		TitlePath:    segment.TitlePath,
		CreatedAt:    formatTime(segment.CreatedAt),
	}
}

// upload 接收 multipart 表单，保存文件并投递后台解析。
func (h *documentHandler) upload(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}

	fileHeader, err := c.FormFile(uploadFormField)
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "缺少上传文件字段 "+uploadFormField))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "读取上传文件失败"))
		return
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "读取上传内容失败"))
		return
	}

	doc, err := h.documents.Upload(ctx, service.UploadDocumentInput{
		DatasetID: datasetID,
		Filename:  fileHeader.Filename,
		Content:   content,
		MIMEType:  fileHeader.Header.Get("Content-Type"),
		// V0.1 还没有登录体系：先写占位值，字段与链路先通，
		// 接入鉴权后换成真实用户标识即可（向量库元数据里已经在用）。
		UserID: defaultUploaderID,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	// 202：文件已接收，解析在后台进行，客户端需要轮询文档状态。
	c.JSON(consts.StatusAccepted, OK(newDocumentResponse(doc)))
}

func (h *documentHandler) list(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.documents.List(ctx, service.ListDocumentsInput{
		DatasetID: datasetID,
		Status:    model.DocumentStatus(c.Query("status")),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]documentResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newDocumentResponse(item))
	}
	c.JSON(consts.StatusOK, OK(documentListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}

func (h *documentHandler) get(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	doc, err := h.documents.Get(ctx, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newDocumentResponse(doc)))
}

func (h *documentHandler) delete(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.documents.Delete(ctx, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

func (h *documentHandler) listSegments(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.documents.ListSegments(ctx, service.ListSegmentsInput{
		DocumentID: id,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]segmentResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newSegmentResponse(item))
	}
	c.JSON(consts.StatusOK, OK(segmentListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}
