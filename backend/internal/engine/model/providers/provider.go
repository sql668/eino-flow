// Package providers 是「内置厂商」的注册表与实现。
//
// 对应 docs/PROGRESS.md D3 的 A 方案：
// **厂商是代码，凭证与模型选择是数据。**
//   - 代码侧（本包）声明：这个厂商叫什么、支持哪几类能力、需要填哪些凭证字段、
//     怎么测试连通性、怎么造出向量化实现；
//   - 数据侧（数据库）存：用户填的 base_url 与加密后的凭证、启用了哪些模型、默认绑哪个。
//
// 这样既能有「添加供应商 / 选模型」的配置体验，又不必在 V0.1 就实现
// 动态插件下载、沙箱加载与版本管理（那属于 V2.0 插件市场）。
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	// 别名：本包同时用到「模型能力抽象」（engine/model）与「领域模型」（internal/model）。
	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/model"
)

// CredentialField 声明一项需要用户填写的凭证字段。
//
// 由厂商自己声明而不是写死「api_key」：有的厂商要 key + secret，
// 有的是本地服务压根不要凭证。声明出来，前端表单与后端校验就能共用同一份定义。
type CredentialField struct {
	// Key 是字段名（存进凭证表的 JSON 里），例如 api_key。
	Key string
	// Label 是给使用者看的中文标签。
	Label string
	// Required 表示是否必填。
	Required bool
	// Secret 为 true 时前端用密码框渲染，后端只回掩码，绝不回明文。
	Secret bool
	// Placeholder 是输入框提示。
	Placeholder string
}

// Config 是一次调用所需的厂商配置（凭证已解密）。
type Config struct {
	// Provider 是厂商标识。
	Provider string
	// BaseURL 是用户配置或厂商默认的服务地址。
	BaseURL string
	// ModelName 是模型名；做厂商级连通性测试时可以为空。
	ModelName string
	// Credentials 是解密后的凭证字段。
	Credentials map[string]string
}

// BaseURLOrDefault 返回配置的地址，为空时回落到厂商默认值。
func (c Config) BaseURLOrDefault(def string) string {
	if value := strings.TrimSpace(c.BaseURL); value != "" {
		return strings.TrimRight(value, "/")
	}
	return strings.TrimRight(def, "/")
}

// Credential 读取某个凭证字段。
func (c Config) Credential(key string) string {
	if c.Credentials == nil {
		return ""
	}
	return strings.TrimSpace(c.Credentials[key])
}

// ProbeResult 是连通性测试的结果。
//
// 成功也要带 Message：只说「测试通过」无法区分「连上了但没看到模型」与
// 「连上了且模型都在」，而这正是排查配置错误时最需要的信息。
type ProbeResult struct {
	OK bool
	// Message 是给使用者看的中文结论。
	Message string
	// Models 是探测到的模型名列表（厂商支持列举时返回），供前端直接下拉选择。
	Models []string
	// ModelAvailable 表示指定的模型名在探测结果中存在；
	// 未指定模型名时为 false，不参与判断。
	ModelAvailable bool
	// LatencyMS 是探测耗时。
	LatencyMS int
}

// Provider 是一个内置厂商。
//
// 接口刻意保持窄：只暴露「描述自己 + 探测 + 造实现」。
// 每加一个新厂商是一次实现，不需要改调用方（开闭原则）。
type Provider interface {
	// ID 是厂商标识，落库并出现在 URL 里，一旦发布不要再改。
	ID() string
	// DisplayName 是厂商默认展示名。取「使用者一眼能懂」的说法，
	// 不出现 sidecar / inference server 这类术语。
	DisplayName() string
	// Description 是一句话说明：这个厂商是干什么的、什么时候该用它。
	// 仅凭名字（如 TEI）使用者猜不出用途，配置页需要这句话。
	Description() string
	// Kinds 返回该厂商支持的模型能力类型。
	Kinds() []model.ModelType
	// DefaultBaseURL 返回默认服务地址（用户可覆盖）。
	DefaultBaseURL() string
	// CredentialFields 声明需要填写的凭证字段。
	CredentialFields() []CredentialField
	// Probe 测试连通性，尽可能顺带列出可用模型。
	Probe(ctx context.Context, cfg Config) (ProbeResult, error)
	// NewEmbedder 构造向量化实现；该厂商不支持 embedding 时返回错误。
	NewEmbedder(cfg Config) (enginemodel.Embedder, error)
	// NewReranker 构造重排实现；该厂商不支持重排时返回错误（带可执行的指引）。
	NewReranker(cfg Config) (enginemodel.Reranker, error)
}

// registry 是内置厂商注册表。key 为厂商标识。
var registry = func() map[string]Provider {
	all := []Provider{
		newOllama(),
		newOpenAICompatible(),
		newDeepSeek(),
		newTEI(),
	}
	table := make(map[string]Provider, len(all))
	for _, item := range all {
		table[item.ID()] = item
	}
	return table
}()

// Get 按标识取厂商；不存在时把可选值一并返回，避免只说「不支持」。
func Get(id string) (Provider, error) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return nil, fmt.Errorf("厂商标识不能为空（可选：%s）", strings.Join(IDs(), ", "))
	}
	provider, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("未知的厂商 %q（可选：%s）", id, strings.Join(IDs(), ", "))
	}
	return provider, nil
}

// All 返回全部内置厂商，顺序固定（按 ID 排序），便于接口输出稳定。
func All() []Provider {
	all := make([]Provider, 0, len(registry))
	for _, provider := range registry {
		all = append(all, provider)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID() < all[j].ID() })
	return all
}

// IDs 返回全部厂商标识。
func IDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Supports 判断厂商是否支持某类模型能力。
func Supports(provider Provider, kind model.ModelType) bool {
	for _, item := range provider.Kinds() {
		if item == kind {
			return true
		}
	}
	return false
}

// probeOpenAIStyleModels 是「OpenAI 风格」厂商的通用探测：
// GET {base}/models + Bearer 鉴权，返回模型列表。
//
// 抽出来是因为 DeepSeek、通义、硅基流动这类厂商的探测方式完全一致，
// 差别只在默认地址与错误文案——复制一份不会有任何收益，只会多一处要同步改的地方。
func probeOpenAIStyleModels(
	ctx context.Context, client *http.Client, baseURL, apiKey, wantModel string,
) (ProbeResult, error) {
	return measure(func() (ProbeResult, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
		if err != nil {
			return ProbeResult{}, err
		}
		if apiKey != "" {
			request.Header.Set("Authorization", "Bearer "+apiKey)
		}

		response, err := client.Do(request)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("无法连接（%s）：%w", baseURL, err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("读取响应失败: %w", err)
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return ProbeResult{}, fmt.Errorf("凭证被拒绝（HTTP %d），请检查 API Key", response.StatusCode)
		}
		if response.StatusCode != http.StatusOK {
			return ProbeResult{}, fmt.Errorf("接口返回 HTTP %d：%s", response.StatusCode, truncate(string(body), 200))
		}

		var parsed struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			// 部分兼容端点不实现 /models：地址是通的，只是拿不到列表，不算失败。
			return ProbeResult{
				OK:      true,
				Message: "地址可访问，但该端点未提供模型列表（不影响按模型名调用）",
			}, nil
		}
		if parsed.Error != nil && parsed.Error.Message != "" {
			return ProbeResult{}, fmt.Errorf("接口报错: %s", parsed.Error.Message)
		}

		names := make([]string, 0, len(parsed.Data))
		for _, item := range parsed.Data {
			if item.ID != "" {
				names = append(names, item.ID)
			}
		}
		return buildProbeResult("接口连接正常，凭证有效", names, wantModel), nil
	})
}

// measure 执行一次探测并补齐耗时，统一各厂商的实现方式。
func measure(run func() (ProbeResult, error)) (ProbeResult, error) {
	started := time.Now()
	result, err := run()
	result.LatencyMS = int(time.Since(started).Milliseconds())
	return result, err
}

// truncate 限制错误信息长度，避免把大段响应体带进日志与接口。
func truncate(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
