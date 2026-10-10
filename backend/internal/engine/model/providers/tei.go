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
	"einoflow-backend/internal/engine/model/tei"
	"einoflow-backend/internal/model"
)

// teiProvider 对接「TEI 协议」的重排服务。
//
// 协议兼容是关键：官方 TEI 镜像与项目自带的 deploy/rerank-local 说同一套 HTTP 接口，
// 因此同一份配置在 Linux 服务器与 macOS 开发机上都能用，后端不需要感知差异。
//
// 对外展示名刻意不叫「边车」：那是部署术语，使用者看到「TEI 边车」并不知道它干什么。
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

func (p *teiProvider) ID() string          { return "tei" }
func (p *teiProvider) DisplayName() string { return "TEI 重排服务" }

func (p *teiProvider) Description() string {
	// 描述里点明「两种运行方式」：开源之后别人用的机器各不相同，
	// 只写一种会让 Apple Silicon 或没装 Docker 的人以为这功能用不了。
	return "承载重排的推理服务（Ollama 没有重排接口）。两种跑法协议一致，任选其一：" +
		"Linux 服务器用 docker compose --profile rerank up -d tei（官方 TEI 镜像）；" +
		"macOS / 无 Docker 环境用 deploy/rerank-local（PyTorch 直接调本机 GPU）。"
}
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
			return ProbeResult{}, fmt.Errorf("无法连接 TEI 重排服务（%s）：%w", baseURL, err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("读取响应失败: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return ProbeResult{}, fmt.Errorf("重排服务返回 HTTP %d：%s", response.StatusCode, truncate(string(body), 200))
		}

		var parsed teiInfoResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return ProbeResult{}, fmt.Errorf("解析重排服务信息失败: %w", err)
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
			result.Message = fmt.Sprintf("连接正常，服务实际加载的模型是：%s", loaded)
		case sameModelName(configured, parsed.ModelID):
			result.ModelAvailable = true
			result.Message = fmt.Sprintf("连接正常，服务加载的模型是 %s，与配置一致", loaded)
		default:
			// 不一致只提示、不判失败：加载哪个模型由边车启动参数决定，
			// 使用者可能是故意先配好配置、稍后重启边车。
			result.Message = fmt.Sprintf("连接正常，但服务实际加载的是 %s，与配置的 %s 不一致（请检查重排服务的启动参数）",
				loaded, configured)
		}
		return result, nil
	})
}

// sameModelName 判断「配置的模型」与「边车实际加载的模型」是否一致。
//
// 不能只做字符串相等：本项目为了让 TEI 跳过自带下载器（它要求响应带 ETag，
// 而 hf-mirror 不返回该头），是用**本地路径**启动耳车的，
// 于是 /info 报回的是 /data/models/BAAI/bge-reranker-v2-m3 这种路径。
// 这里按路径后缀比对，既能识别出「同一个模型」，又保留对「换了模型」的告警能力。
func sameModelName(configured, loaded string) bool {
	configured = strings.Trim(strings.TrimSpace(configured), "/")
	loaded = strings.Trim(strings.TrimSpace(loaded), "/")
	if configured == "" || loaded == "" {
		return configured == loaded
	}
	if strings.EqualFold(configured, loaded) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(loaded), strings.ToLower("/"+configured))
}

// NewReranker 构造 TEI 重排客户端。
//
// 刻意不做「启动时校验边车加载的模型」：那需要额外一次 /info 调用，
// 而配置页的「测试连接」已经能看到真实加载的模型；
// 在这里再拦一道会让「边车稍后启动」这种正常时序变成硬失败。
func (p *teiProvider) NewReranker(cfg Config) (enginemodel.Reranker, error) {
	if strings.TrimSpace(cfg.ModelName) == "" {
		return nil, fmt.Errorf("未指定重排模型名")
	}
	return tei.New(tei.Options{
		BaseURL: cfg.BaseURLOrDefault(p.DefaultBaseURL()),
		Model:   strings.TrimSpace(cfg.ModelName),
		APIKey:  cfg.Credential("api_key"),
	}), nil
}

// NewChatModel 明确不支持：TEI 是文本嵌入/重排推理服务，不做对话。
func (p *teiProvider) NewChatModel(Config) (enginemodel.ChatModel, error) {
	return nil, fmt.Errorf("%w: TEI 重排服务不做对话，请选择 Ollama / DeepSeek / OpenAI 兼容接口",
		enginemodel.ErrChatUnsupported)
}

// NewEmbedder 明确不支持：TEI 在本项目里专职重排。
//
// 返回带指引的错误，比「静默返回空实现」更容易排查。
func (p *teiProvider) NewEmbedder(Config) (enginemodel.Embedder, error) {
	return nil, fmt.Errorf("TEI 重排服务只做重排，不支持向量化；向量化请选择 Ollama 或 OpenAI 兼容接口")
}
