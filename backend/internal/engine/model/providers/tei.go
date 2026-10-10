package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/model"
)

// teiProvider 是 HuggingFace text-embeddings-inference（TEI）边车。
//
// 为什么需要它（D5 实测结论）：Ollama 没有 `/rerank` 端点，其 rerank 模型调用即崩溃，
// 所以重排改用 TEI 边车承载；本项目**只把它用于重排**，向量化仍走 Ollama / OpenAI 兼容接口。
//
// 重排客户端在阶段 3 落地，这里先提供「配置 + 连通性测试」，
// 让使用者能提前把边车配好，并当场看到它到底加载了哪个模型。
type teiProvider struct {
	http *http.Client
}

func newTEI() *teiProvider {
	return &teiProvider{http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *teiProvider) ID() string             { return "tei" }
func (p *teiProvider) DisplayName() string    { return "TEI 边车（重排 / Rerank）" }
func (p *teiProvider) DefaultBaseURL() string { return "http://127.0.0.1:8080" }

func (p *teiProvider) Kinds() []model.ModelType {
	return []model.ModelType{model.ModelTypeRerank}
}

// CredentialFields 返回 nil：本地边车一般不鉴权。
// 不返回「一个标着可选的字段」——不存在的凭证字段不该出现在表单里。
func (p *teiProvider) CredentialFields() []CredentialField { return nil }

// teiInfoResponse 对应 GET /info。
type teiInfoResponse struct {
	ModelID        string `json:"model_id"`
	ModelDtype     string `json:"model_dtype"`
	MaxInputLength int    `json:"max_input_length"`
	Version        string `json:"version"`
}

// Probe 读取 /info，报告边车实际加载的模型。
//
// 这条信息是刻意要暴露的：边车加载哪个模型完全由它的启动参数决定，
// 界面里写「重排模型 = bge-reranker-v2-m3」而边车实际跑的是别的模型，
// 属于「配置显示 A、实际跑 B」的典型陷阱，只有把真实值显示出来才能发现。
func (p *teiProvider) Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	baseURL := cfg.BaseURLOrDefault(p.DefaultBaseURL())
	return measure(func() (ProbeResult, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/info", nil)
		if err != nil {
			return ProbeResult{}, err
		}

		response, err := p.http.Do(request)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("无法连接 TEI 边车（%s）：%w", baseURL, err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("读取响应失败: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return ProbeResult{}, fmt.Errorf("边车返回 HTTP %d：%s", response.StatusCode, truncate(string(body), 200))
		}

		var parsed teiInfoResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return ProbeResult{}, fmt.Errorf("解析边车信息失败: %w", err)
		}

		result := ProbeResult{OK: true, Models: []string{}}
		if parsed.ModelID != "" {
			result.Models = []string{parsed.ModelID}
		}

		loaded := parsed.ModelID
		if loaded == "" {
			loaded = "未知"
		}
		configured := strings.TrimSpace(cfg.ModelName)
		switch {
		case configured == "":
			result.Message = fmt.Sprintf("边车连接正常，实际加载模型：%s", loaded)
		case configured == parsed.ModelID:
			result.ModelAvailable = true
			result.Message = fmt.Sprintf("边车连接正常，已加载模型 %s，与配置一致", loaded)
		default:
			// 不一致只提示、不判失败：加载哪个模型由边车启动参数决定，
			// 使用者可能是故意先配好配置、稍后重启边车。
			result.Message = fmt.Sprintf("边车连接正常，但实际加载的是 %s，与配置的 %s 不一致（请检查边车启动参数）",
				loaded, configured)
		}
		return result, nil
	})
}

// NewEmbedder 明确不支持：TEI 在本项目里专职重排。
//
// 返回带指引的错误，比「静默返回空实现」更容易排查。
func (p *teiProvider) NewEmbedder(Config) (enginemodel.Embedder, error) {
	return nil, fmt.Errorf("TEI 边车用于重排，不支持向量化；向量化请选择 Ollama 或 OpenAI 兼容接口")
}
