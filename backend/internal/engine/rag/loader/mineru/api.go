package mineru

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ---------- 请求 / 响应结构 ----------
// 只声明用到的字段：服务端新增字段不会影响反序列化，缺字段时零值也能被下面的校验拦住。

type createUploadRequest struct {
	Filename string `json:"filename"`
	Bytes    int    `json:"bytes"`
	MIMEType string `json:"mime_type"`
}

type fileObject struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
}

type uploadResponse struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	UploadURL     string            `json:"upload_url"`
	UploadMethod  string            `json:"upload_method"`
	UploadHeaders map[string]string `json:"upload_headers"`
	// 完成上传后，file 字段里才有可用的产物文件 ID。
	File *fileObject `json:"file"`
}

type jobFileSource struct {
	Type   string `json:"type"`
	FileID string `json:"file_id"`
}

type jobFileEntry struct {
	Source jobFileSource `json:"source"`
}

type createJobRequest struct {
	Files         []jobFileEntry `json:"files"`
	Tier          string         `json:"tier,omitempty"`
	OCRMode       string         `json:"ocr_mode,omitempty"`
	OutputFormats []string       `json:"output_formats,omitempty"`
}

type errorDetail struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

type outputFileRef struct {
	FileID string `json:"file_id"`
	Bytes  int64  `json:"bytes"`
}

type fileParseInfo struct {
	ModelUsed     *string `json:"model_used"`
	DurationMS    *int    `json:"duration_ms"`
	ParserVersion *string `json:"parser_version"`
}

type jobOutputFiles struct {
	MiddleJSON *outputFileRef `json:"middle_json"`
	Markdown   *outputFileRef `json:"markdown"`
}

type jobFileResult struct {
	FileID      string          `json:"file_id"`
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	PageRange   string          `json:"page_range"`
	Error       *errorDetail    `json:"error"`
	Parse       *fileParseInfo  `json:"parse"`
	OutputFiles *jobOutputFiles `json:"output_files"`
}

type jobProgress struct {
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

type jobResponse struct {
	JobID    string          `json:"job_id"`
	Status   string          `json:"status"`
	Tier     string          `json:"tier"`
	Files    []jobFileResult `json:"files"`
	Progress *jobProgress    `json:"progress"`
}

// ---------- 对外接口 ----------

// ParseRequest 是一次解析请求。
type ParseRequest struct {
	Filename string
	Content  []byte
	MIMEType string
	// Tier 为空时由服务端决定默认档位。
	Tier string
	// OCRMode 可选：auto / txt / ocr，留空即 auto（由服务端判断是否有文本层）。
	OCRMode string
	// OutputFormats 指定需要的产物。
	// 留空时只取 middle_json——注意服务端默认只返回 markdown，必须显式请求结构化产物。
	OutputFormats []string
}

// ParseResult 是解析产物与元信息。
type ParseResult struct {
	// MiddleJSON 是结构化产物（schema=docvortex.middle），解析层主要消费它。
	MiddleJSON []byte
	// Markdown 是可读产物，取不到时为空，不影响主流程。
	Markdown []byte
	// ParserVersion 是服务端返回的解析器版本，例如 4.0.11。
	ParserVersion string
	// DurationMS 是服务端记录的解析耗时。
	DurationMS int
	// Tier 是服务端实际执行的档位。
	Tier string
	// Warnings 记录非致命问题，例如某个产物缺失。
	Warnings []string
}

// Parse 上传文件并等待解析完成。
func (c *Client) Parse(ctx context.Context, req ParseRequest) (*ParseResult, error) {
	if len(req.Content) == 0 {
		return nil, fmt.Errorf("文件内容为空")
	}

	fileID, err := c.upload(ctx, req)
	if err != nil {
		return nil, err
	}

	job, err := c.createJob(ctx, fileID, req)
	if err != nil {
		return nil, err
	}

	finished, err := c.waitJob(ctx, job.JobID)
	if err != nil {
		return nil, err
	}
	return c.collectResult(ctx, finished)
}

// upload 完成「创建上传 → 写入字节 → 完成上传」三步，返回 file_id。
func (c *Client) upload(ctx context.Context, req ParseRequest) (string, error) {
	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	var created uploadResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/uploads", createUploadRequest{
		Filename: req.Filename,
		Bytes:    len(req.Content),
		MIMEType: mimeType,
	}, &created); err != nil {
		return "", fmt.Errorf("创建上传失败: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("创建上传未返回 upload id")
	}

	// 服务端可能返回独立的上传地址（例如预签名 URL），优先使用它。
	uploadURL := created.UploadURL
	if uploadURL == "" {
		uploadURL = c.baseURL + "/v1/uploads/" + created.ID + "/content"
	}
	method := created.UploadMethod
	if method == "" {
		method = http.MethodPut
	}
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	for key, value := range created.UploadHeaders {
		headers[key] = value
	}
	if err := c.doRaw(ctx, method, uploadURL, headers, req.Content); err != nil {
		return "", fmt.Errorf("上传文件内容失败: %w", err)
	}

	var completed uploadResponse
	if err := c.doJSON(ctx, http.MethodPost,
		"/v1/uploads/"+created.ID+"/complete", struct{}{}, &completed); err != nil {
		return "", fmt.Errorf("完成上传失败: %w", err)
	}
	if completed.File == nil || completed.File.ID == "" {
		return "", fmt.Errorf("完成上传后未返回 file_id（upload_id=%s）", created.ID)
	}
	return completed.File.ID, nil
}

// createJob 提交解析任务。
func (c *Client) createJob(ctx context.Context, fileID string, req ParseRequest) (*jobResponse, error) {
	formats := req.OutputFormats
	if len(formats) == 0 {
		formats = []string{outputFormatMiddleJSON}
	}

	var job jobResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/parse/jobs", createJobRequest{
		Files:         []jobFileEntry{{Source: jobFileSource{Type: "file_id", FileID: fileID}}},
		Tier:          req.Tier,
		OCRMode:       req.OCRMode,
		OutputFormats: formats,
	}, &job); err != nil {
		return nil, fmt.Errorf("创建解析任务失败: %w", err)
	}
	if job.JobID == "" {
		return nil, fmt.Errorf("创建解析任务未返回 job_id")
	}
	return &job, nil
}

// waitJob 轮询任务直到终态。
func (c *Client) waitJob(ctx context.Context, jobID string) (*jobResponse, error) {
	deadline := time.Now().Add(c.pollTimeout)
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("等待解析任务 %s 被取消: %w", jobID, ctx.Err())
		case <-timer.C:
		}

		var job jobResponse
		if err := c.doJSON(ctx, http.MethodGet, "/v1/parse/jobs/"+jobID, nil, &job); err != nil {
			return nil, fmt.Errorf("查询解析任务 %s 失败: %w", jobID, err)
		}

		switch job.Status {
		case jobStatusCompleted:
			return &job, nil
		case jobStatusFailed, jobStatusCanceled:
			return nil, fmt.Errorf("解析任务 %s 状态为 %s：%s", jobID, job.Status, summarizeJobErrors(job.Files))
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("等待解析任务 %s 超时（%s），最后状态 %s", jobID, c.pollTimeout, job.Status)
		}
		timer.Reset(c.pollInterval)
	}
}

// collectResult 取回产物内容并汇总元信息与警告。
func (c *Client) collectResult(ctx context.Context, job *jobResponse) (*ParseResult, error) {
	result := &ParseResult{Tier: job.Tier}

	for _, file := range job.Files {
		if file.Parse != nil {
			if file.Parse.ParserVersion != nil {
				result.ParserVersion = *file.Parse.ParserVersion
			}
			if file.Parse.DurationMS != nil {
				result.DurationMS = *file.Parse.DurationMS
			}
		}
		if file.Error != nil && file.Error.Message != "" {
			// 单文件报错先记下来，最后统一判断是否有可用产物。
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s 解析报错：%s", file.Name, file.Error.Message))
		}
		if file.OutputFiles == nil {
			continue
		}

		if ref := file.OutputFiles.MiddleJSON; ref != nil && ref.FileID != "" {
			content, err := c.fetchFile(ctx, ref.FileID)
			if err != nil {
				return nil, fmt.Errorf("获取 middle_json 失败: %w", err)
			}
			result.MiddleJSON = content
		}
		if ref := file.OutputFiles.Markdown; ref != nil && ref.FileID != "" {
			// markdown 只是附加产物，取不到不影响入库，降级为警告。
			content, err := c.fetchFile(ctx, ref.FileID)
			if err != nil {
				result.Warnings = append(result.Warnings, "获取 markdown 失败："+err.Error())
			} else {
				result.Markdown = content
			}
		}
	}

	if len(result.MiddleJSON) == 0 {
		return nil, fmt.Errorf("解析完成但未拿到 middle_json（警告：%s）", strings.Join(result.Warnings, "；"))
	}
	return result, nil
}

// summarizeJobErrors 汇总任务内各文件的错误信息，用于失败时给出可定位的原因。
func summarizeJobErrors(files []jobFileResult) string {
	var parts []string
	for _, file := range files {
		if file.Error != nil && file.Error.Message != "" {
			parts = append(parts, file.Name+": "+file.Error.Message)
			continue
		}
		if file.Status != "" && file.Status != jobStatusCompleted {
			parts = append(parts, file.Name+": "+file.Status)
		}
	}
	if len(parts) == 0 {
		return "服务端未给出具体原因"
	}
	return strings.Join(parts, "；")
}
