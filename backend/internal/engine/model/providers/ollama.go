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
	"einoflow-backend/internal/engine/model/ollama"
	"einoflow-backend/internal/model"
)

// ollamaProvider 是本地 / 自建 Ollama。
//
// 已实测（Ollama 0.40.1）：`/api/tags` 列出已下载模型，`/api/embed` 支持批量向量化。
// **不提供 rerank**：其 rerank 模型调用即 GGML_ASSERT 崩溃，且没有 /rerank 端点（见 D5），
// 因此这里声明的能力不含 rerank，重排走 TEI 边车。
type ollamaProvider struct {
	http *http.Client
}

func newOllama() *ollamaProvider {
	return &ollamaProvider{http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *ollamaProvider) ID() string          { return "ollama" }
func (p *ollamaProvider) DisplayName() string { return "Ollama（本地模型）" }

// Description 一句话说明用途：仅凭厂商名使用者猜不出它能做什么、什么时候该选它。
func (p *ollamaProvider) Description() string {
	return "本机或自建服务器上的 Ollama，可跑对话与向量化模型；不提供重排接口。"
}
func (p *ollamaProvider) DefaultBaseURL() string { return "http://127.0.0.1:11434" }

func (p *ollamaProvider) Kinds() []model.ModelType {
	return []model.ModelType{model.ModelTypeLLM, model.ModelTypeEmbedding}
}

func (p *ollamaProvider) CredentialFields() []CredentialField {
	// Ollama 直连不需要凭证；只有经反向代理暴露时才可能要 Bearer token，
	// 因此标为可选而不是干脆不给字段。
	return []CredentialField{{
		Key:         "api_key",
		Label:       "访问令牌（可选）",
		Required:    false,
		Secret:      true,
		Placeholder: "直连本地服务时留空",
	}}
}

// ollamaTagsResponse 对应 GET /api/tags。
type ollamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

// Probe 通过列举本地模型完成连通性测试，并在指定了模型名时校验是否已下载。
//
// 这一条校验非常值钱：Ollama 的「模型不存在」不是配置错误，而是**运行时**才暴露的错误
// （上传文档时才发现），表现还只是「文档处理失败」。在这里提前查出来，排查成本最低。
func (p *ollamaProvider) Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	baseURL := cfg.BaseURLOrDefault(p.DefaultBaseURL())
	return measure(func() (ProbeResult, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
		if err != nil {
			return ProbeResult{}, err
		}
		if token := cfg.Credential("api_key"); token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}

		response, err := p.http.Do(request)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("无法连接 Ollama（%s）：%w", baseURL, err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("读取 Ollama 响应失败: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return ProbeResult{}, fmt.Errorf("Ollama 返回 HTTP %d：%s", response.StatusCode, truncate(string(body), 200))
		}

		var parsed ollamaTagsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return ProbeResult{}, fmt.Errorf("解析 Ollama 响应失败: %w", err)
		}

		names := make([]string, 0, len(parsed.Models))
		for _, item := range parsed.Models {
			if item.Name != "" {
				names = append(names, item.Name)
			}
		}
		return buildProbeResult("Ollama 连接正常", names, cfg.ModelName), nil
	})
}

// NewEmbedder 构造 Ollama 向量化实现，并顺带探测维度（失败即模型不可用）。
func (p *ollamaProvider) NewEmbedder(cfg Config) (enginemodel.Embedder, error) {
	if strings.TrimSpace(cfg.ModelName) == "" {
		return nil, fmt.Errorf("未指定模型名")
	}
	instance := ollama.New(ollama.Options{
		BaseURL: cfg.BaseURLOrDefault(p.DefaultBaseURL()),
		Model:   strings.TrimSpace(cfg.ModelName),
		Token:   cfg.Credential("api_key"),
	})
	// 构造即探测：维度是创建 / 校验向量集合的依据，拿不到维度就不能算「可用」。
	if _, err := instance.DetectDimension(context.Background()); err != nil {
		return nil, fmt.Errorf("Ollama 模型 %s 不可用: %w", cfg.ModelName, err)
	}
	return instance, nil
}

// buildProbeResult 统一「探测到了模型列表」之后的结论拼装。
//
// 抽出来是因为三家厂商的探测接口不同，但结论的表述必须一致，
// 否则使用者会在不同厂商上看到风格不同甚至互相矛盾的提示。
func buildProbeResult(okMessage string, models []string, want string) ProbeResult {
	result := ProbeResult{OK: true, Message: okMessage, Models: models}
	want = strings.TrimSpace(want)
	if want == "" {
		if len(models) > 0 {
			result.Message = fmt.Sprintf("%s，可选模型 %d 个", okMessage, len(models))
		}
		return result
	}

	for _, name := range models {
		if name == want {
			result.ModelAvailable = true
			result.Message = fmt.Sprintf("%s，模型 %s 可用", okMessage, want)
			return result
		}
	}

	// 模型不在列表里：这不是「连接失败」，所以 OK 仍为 true，
	// 但要把话说清楚，否则使用者会把「模型没下载」当成「地址填错」。
	result.Message = fmt.Sprintf("%s，但未找到模型 %s（需先拉取该模型）", okMessage, want)
	return result
}

// NewReranker 明确不支持：实测 Ollama 没有 /rerank 端点，其重排模型调用即 GGML_ASSERT 崩溃（D5）。
//
// 错误信息里直接给出替代方案：只说「不支持」会让使用者去反复试模型名，
// 而真正该做的是换成 TEI 边车。
func (p *ollamaProvider) NewReranker(Config) (enginemodel.Reranker, error) {
	return nil, fmt.Errorf("%w: Ollama 不提供重排接口，请添加 TEI 边车作为重排模型（docker compose --profile rerank up -d tei）",
		enginemodel.ErrRerankUnsupported)
}
