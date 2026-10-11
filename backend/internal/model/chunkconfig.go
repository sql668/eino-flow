package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ChunkMode 是分段模式。
type ChunkMode string

const (
	// ChunkModeTitle 按标题切：标题开新片，超长再按长度切。适合手册、论文。
	ChunkModeTitle ChunkMode = "title"
	// ChunkModeLength 纯按长度切，忽略标题层级。适合作业记录、聊天导出等无结构文本。
	ChunkModeLength ChunkMode = "length"
	// ChunkModeSeparator 按自定义分隔符切（例如 "\n\n"），再按长度切。
	ChunkModeSeparator ChunkMode = "separator"
	// ChunkModeParentChild 父子分块：子块用于检索，父块作为上下文。
	// **尚未实现**，仅作为占位（选了会明确报错，不做静默降级）。
	ChunkModeParentChild ChunkMode = "parent_child"
)

// IsValid 校验分段模式取值。
func (m ChunkMode) IsValid() bool {
	switch m {
	case ChunkModeTitle, ChunkModeLength, ChunkModeSeparator, ChunkModeParentChild:
		return true
	default:
		return false
	}
}

// Label 返回给界面看的中文名。
func (m ChunkMode) Label() string {
	switch m {
	case ChunkModeTitle:
		return "按标题分段"
	case ChunkModeLength:
		return "按固定长度分段"
	case ChunkModeSeparator:
		return "按自定义分隔符分段"
	case ChunkModeParentChild:
		return "父子分段"
	default:
		return string(m)
	}
}

// 分段长度的取值范围。上限按字符计：中文一个字算一个字符，
// 800 字左右是「一段能被完整回答的量」，太大模型会稀释重点，太小会切碎语义。
const (
	DefaultChunkMaxRunes = 800
	MinChunkMaxRunes     = 100
	MaxChunkMaxRunes     = 4000
	DefaultChunkMinRunes = 40
	MaxChunkMinRunes     = 500
	// DefaultOverlapPercent 是窗口重叠的默认比例（分段上限的 10%）。
	DefaultOverlapPercent = 10

	// MaxSeparators 是自定义分隔符的条数上限。
	//
	// 分隔符多了之后「到底按哪个切」会变得难以预期（每条都可能是别人的前缀），
	// 而且切得越碎越不像「一条记录」。给个上限比事后解释便宜。
	MaxSeparators = 10
	// MaxSeparatorRunes 是单个分隔符的长度上限（字符数）。
	// 分隔符是「边界标记」，超过这个长度更可能是使用者误把整段内容粘了进来。
	MaxSeparatorRunes = 64
)

// ChunkConfig 是知识库级的分段与清洗配置。
type ChunkConfig struct {
	Mode ChunkMode `json:"mode"`
	// Separators 是自定义分隔符列表，仅在 Mode=separator 时使用。
	//
	// 用列表而不是单个字符串：实际文档里「一条记录」的边界往往有几种写法
	// （空行 + 分隔线 + 「第 N 条」），只给一个分隔符就得反复重切。
	// 界面上按「英文逗号分隔」输入，字面逗号用 \, 转义（见 parseSeparators）。
	Separators []string `json:"separators"`
	// Separator 是**已废弃**的单值字段，只为读得懂老配置而保留。
	//
	// 老数据的 chunk_config 里存的是 "separator": "..."；Normalize 会把它
	// 折叠进 Separators，之后不再写回。新代码请只用 Separators。
	Separator string `json:"separator,omitempty"`
	// MaxRunes 是单个片段的目标长度上限（字符数）。
	MaxRunes int `json:"maxRunes"`
	// OverlapRunes 是相邻片段的重叠窗口长度（字符数），默认取上限的 10%。
	//
	// 只在「同一章节内被长度切开的相邻片段」之间生效：
	// 标题切换处不重叠（语义单元已变，重叠只会引入无关内容），表格也不重叠。
	OverlapRunes int `json:"overlapRunes"`
	// MinRunes 是独立成段的最小长度，短于此长度的正文会与相邻内容合并。
	MinRunes int `json:"minRunes"`
	// KeepNoise 为 true 时保留页眉/页脚/页码等噪声块（默认丢弃）。
	KeepNoise bool `json:"keepNoise"`
	// Cleaning 是启用的清洗规则列表；默认全部不启用。
	Cleaning []CleaningRuleConfig `json:"cleaning"`
	// Keywords 是关键词抽取配置。
	//
	// 用指针是为了区分「没配过」和「显式关掉」：字段是后加的，
	// 用值类型的话老配置反序列化出来是「关」，会让存量知识库静默停止生成关键词。
	Keywords *KeywordConfig `json:"keywords,omitempty"`
}

// 关键词抽取的取值范围。
const (
	// DefaultKeywordMaxCount 是单段关键词数量的默认上限。
	// 5 个足够覆盖一段的主旨，再多在界面上展示不下、对检索的边际收益也很低。
	DefaultKeywordMaxCount = 5
	// MaxKeywordMaxCount 是单段关键词数量上限。
	MaxKeywordMaxCount = 10
)

// KeywordConfig 是入库时自动抽取关键词的配置。
//
// 关键词有两个用途：界面上展示（可人工修改），以及作为 BM25 全文检索的输入文本之一。
// 因此「关掉关键词」不只是少了一列展示，也会让全文检索失去这部分权重。
type KeywordConfig struct {
	// Enabled 是否在入库时自动抽取关键词。
	Enabled bool `json:"enabled"`
	// MaxCount 是单段关键词数量上限。
	MaxCount int `json:"maxCount"`
}

// Normalize 收敛关键词配置的边界。
func (c KeywordConfig) Normalize() KeywordConfig {
	out := c
	if out.MaxCount <= 0 {
		out.MaxCount = DefaultKeywordMaxCount
	}
	if out.MaxCount > MaxKeywordMaxCount {
		out.MaxCount = MaxKeywordMaxCount
	}
	return out
}

// KeywordConfigDefault 返回关键词抽取的默认配置（开启，最多 5 个）。
func KeywordConfigDefault() KeywordConfig {
	return KeywordConfig{Enabled: true, MaxCount: DefaultKeywordMaxCount}
}

// CleaningRuleConfig 是一条清洗规则的配置。
type CleaningRuleConfig struct {
	// Key 对应代码里注册的规则（见 engine/rag/cleaner），配置只记「哪些开着」。
	Key string `json:"key"`
	// Pattern 仅自定义正则规则使用。
	Pattern string `json:"pattern,omitempty"`
}

// Normalize 补齐默认值并做边界收敛，返回规范化后的副本。
//
// 在服务层入口调用：配置来自界面，缺项、越界都可能出现，
// 与其在每个使用点各判一次，不如统一在这里收敛成一份可信的配置。
func (c ChunkConfig) Normalize() ChunkConfig {
	out := c
	if out.Mode == "" {
		out.Mode = ChunkModeTitle
	}
	out.Separators = normalizeSeparators(out.Separator, out.Separators)
	// 折叠完成后不再保留单值字段：否则同一份配置里出现两个「分隔符」，
	// 改了一个忘了另一个，切分结果就对不上了。
	out.Separator = ""
	if out.MaxRunes <= 0 {
		out.MaxRunes = DefaultChunkMaxRunes
	}
	if out.MaxRunes < MinChunkMaxRunes {
		out.MaxRunes = MinChunkMaxRunes
	}
	if out.MaxRunes > MaxChunkMaxRunes {
		out.MaxRunes = MaxChunkMaxRunes
	}
	if out.OverlapRunes < 0 {
		out.OverlapRunes = 0
	}
	// 重叠不能超过上限的一半：再大就会出现「后一段一半是前一段」的冗余，
	// 检索结果里大量重复内容，反而降低可用性。
	if maxOverlap := out.MaxRunes / 2; out.OverlapRunes > maxOverlap {
		out.OverlapRunes = maxOverlap
	}
	if out.MinRunes < 0 {
		out.MinRunes = 0
	}
	if out.MinRunes > MaxChunkMinRunes {
		out.MinRunes = MaxChunkMinRunes
	}
	if out.MinRunes >= out.MaxRunes {
		out.MinRunes = out.MaxRunes / 4
	}
	out.Cleaning = normalizeCleaning(out.Cleaning)
	// 关键词配置缺省即默认开启：老配置（没有这个字段）与新建文档都走同一套默认值。
	keywords := KeywordConfigDefault()
	if out.Keywords != nil {
		keywords = out.Keywords.Normalize()
	}
	out.Keywords = &keywords
	return out
}

// normalizeSeparators 把分隔符输入整理成可信的列表。
//
// 规则（与界面上的提示保持一致）：
//   - 列表形态：界面按英文逗号分隔多条，字面逗号写成 `\,`；接口传的是数组，
//     所以这里只负责逐项清理（老配置的单个字符串也走同一条路径）；
//   - 转义：`\n` `\r` `\t` `\\` 还原成真实字符。界面上填的是「看得见的写法」——
//     直接粘一个真实换行进去，既看不出粘对没有，也容易在保存时被裁掉；
//   - 去空白、丢空项、去重、限条数与长度；
//   - **按长度降序**：切分是按顺序逐个套用的（见 chunker.expandSeparator），
//     长分隔符排在前面，`\n\n` 才不会被 `\n` 抢先切成空段。
func normalizeSeparators(legacy string, list []string) []string {
	raw := make([]string, 0, len(list)+1)
	raw = append(raw, list...)
	// 老配置（单值字段）视为「一个分隔符」，不按逗号拆：
	// 老界面里逗号是字面量，拆开会把「用逗号切分 CSV」的配置改坏。
	if len(raw) == 0 && strings.TrimSpace(legacy) != "" {
		raw = append(raw, legacy)
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		value := unescapeSeparator(strings.TrimSpace(item))
		if value == "" {
			continue
		}
		if runes := []rune(value); len(runes) > MaxSeparatorRunes {
			value = string(runes[:MaxSeparatorRunes])
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := []rune(out[i]), []rune(out[j])
		if len(left) != len(right) {
			return len(left) > len(right)
		}
		return out[i] < out[j]
	})
	if len(out) > MaxSeparators {
		out = out[:MaxSeparators]
	}
	return out
}

// unescapeSeparator 还原分隔符里的转义写法。
//
// 只认这几个常见序列，其它反斜杠一律按字面保留：
// 把「不认识的反斜杠」也吃掉，会让想用反斜杠当分隔符的人永远配不出来。
func unescapeSeparator(value string) string {
	if !strings.ContainsRune(value, '\\') {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value))
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' || i+1 >= len(runes) {
			builder.WriteRune(runes[i])
			continue
		}
		switch runes[i+1] {
		case 'n':
			builder.WriteRune('\n')
		case 'r':
			builder.WriteRune('\r')
		case 't':
			builder.WriteRune('\t')
		case '\\':
			builder.WriteRune('\\')
		case ',':
			builder.WriteRune(',')
		default:
			// 认不出的转义按原样保留（含那个反斜杠）。
			builder.WriteRune(runes[i])
			continue
		}
		i++
	}
	// 末尾落单的反斜杠已经在上面的分支里写进去了。
	return builder.String()
}

// HasSeparator 判断配置里是否真的填了分隔符。
//
// 服务的校验与指纹都要问同一个问题，各写一次判断迟早会漂移。
// 也要认得出老配置的单值字段：校验拿到的可能是**未规范化**的原始配置
// （界面还在传 "separator": "\n\n" 时），只认列表会把老前端挡在门外。
func (c ChunkConfig) HasSeparator() bool {
	for _, item := range c.Separators {
		if item != "" {
			return true
		}
	}
	return unescapeSeparator(strings.TrimSpace(c.Separator)) != ""
}

// normalizeCleaning 去重、排序并丢弃空规则，保证同样的勾选得到同样的指纹。
func normalizeCleaning(rules []CleaningRuleConfig) []CleaningRuleConfig {
	seen := make(map[string]struct{}, len(rules))
	out := make([]CleaningRuleConfig, 0, len(rules))
	for _, rule := range rules {
		key := strings.TrimSpace(rule.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, CleaningRuleConfig{Key: key, Pattern: strings.TrimSpace(rule.Pattern)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// EnabledCleaningKeys 返回启用的规则键，便于日志与展示。
func (c ChunkConfig) EnabledCleaningKeys() []string {
	keys := make([]string, 0, len(c.Cleaning))
	for _, rule := range c.Cleaning {
		keys = append(keys, rule.Key)
	}
	return keys
}

// Version 生成配置指纹。
//
// 用途：片段落库时记录「是哪套配置切的」，换配置重切后能分辨新旧；
// 指纹只覆盖会改变切分结果的字段（模式/标识符/长度/重叠/清洗），
// 页眉页脚开关也算——它直接影响内容。
// 关键词配置**不在**指纹里：它不改变切分结果，只是给同一批片段附加的标注。
func (c ChunkConfig) Version() string {
	parts := []string{
		string(c.Mode),
		strings.Join(c.Separators, "\x1f"),
		fmt.Sprint(c.MaxRunes),
		fmt.Sprint(c.OverlapRunes),
		fmt.Sprint(c.MinRunes),
		fmt.Sprint(c.KeepNoise),
	}
	for _, rule := range c.Cleaning {
		parts = append(parts, rule.Key+"="+rule.Pattern)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

// DefaultChunkConfig 返回默认配置（标题分段 + 10% 重叠 + 不启用清洗）。
func DefaultChunkConfig() ChunkConfig {
	cfg := ChunkConfig{
		Mode:      ChunkModeTitle,
		MaxRunes:  DefaultChunkMaxRunes,
		MinRunes:  DefaultChunkMinRunes,
		KeepNoise: false,
	}
	cfg.OverlapRunes = cfg.MaxRunes * DefaultOverlapPercent / 100
	keywords := KeywordConfigDefault()
	cfg.Keywords = &keywords
	return cfg
}

// RetrievalConfig 是知识库级的检索配置。
//
// 放在知识库而不是应用上：检索范围与质量参数跟「这批文档」强相关
// （手册类要小 TopK 高阈值，聊天记录类相反），换应用不该改检索策略。
type RetrievalConfig struct {
	// SearchMode 是检索方式：vector 向量 / fulltext 全文 / hybrid 混合。
	//
	// 空值按向量处理：字段是后加的，老配置里没有它，
	// 而「老配置 = 一直用向量检索」才是它们的真实语义，不能当成 hybrid 处理。
	SearchMode SearchMode `json:"searchMode"`
	// HybridRank 是混合检索两路结果的融合方式：weighted 加权 / rrf 倒数排名融合。
	// 仅在 SearchMode=hybrid 时有意义。
	HybridRank HybridRank `json:"hybridRank"`
	// TopK 是最终返回条数。
	TopK int `json:"topK"`
	// CandidateTopK 是重排前的召回条数。
	CandidateTopK int `json:"candidateTopK"`
	// ScoreThreshold 是相似度阈值：低于它的命中直接丢弃。0 表示不启用。
	//
	// bge-m3 对无关内容仍会给 0.45 左右的分，不设阈值时「问什么都有一段最像的」，
	// 会导致模型拿着无关片段硬答。
	ScoreThreshold float32 `json:"scoreThreshold"`
	// Rerank 表示是否启用重排（未配置重排模型时会被忽略）。
	Rerank bool `json:"rerank"`
	// VectorWeight 是语义（向量）权重，KeywordWeight 是关键词（全文）权重。
	// 仅在 SearchMode=hybrid 且 HybridRank=weighted 时生效。
	VectorWeight  float32 `json:"vectorWeight"`
	KeywordWeight float32 `json:"keywordWeight"`
}

// Normalize 补齐默认值并收敛边界。
func (c RetrievalConfig) Normalize(defaultTopK, defaultCandidateTopK int) RetrievalConfig {
	out := c
	if out.SearchMode == "" {
		out.SearchMode = SearchModeVector
	}
	if !out.SearchMode.IsValid() {
		out.SearchMode = SearchModeVector
	}
	// 融合方式只在混合检索下有意义，其它模式统一收敛成加权，
	// 避免「切回向量检索后还留着一个 rrf」这种自相矛盾的组合被存进库。
	if out.SearchMode != SearchModeHybrid {
		out.HybridRank = ""
	} else if out.HybridRank == "" || !out.HybridRank.IsValid() {
		out.HybridRank = HybridRankWeighted
	}
	if out.TopK <= 0 {
		out.TopK = defaultTopK
	}
	if out.TopK > MaxRetrievalTopK {
		out.TopK = MaxRetrievalTopK
	}
	if out.CandidateTopK <= 0 {
		out.CandidateTopK = defaultCandidateTopK
	}
	if out.CandidateTopK < out.TopK {
		out.CandidateTopK = out.TopK
	}
	if out.CandidateTopK > MaxCandidateTopK {
		out.CandidateTopK = MaxCandidateTopK
	}
	if out.ScoreThreshold < 0 {
		out.ScoreThreshold = 0
	}
	if out.ScoreThreshold > 1 {
		out.ScoreThreshold = 1
	}
	if out.VectorWeight <= 0 && out.KeywordWeight <= 0 {
		// 默认偏语义：中文短查询下向量检索比关键词更稳。
		out.VectorWeight, out.KeywordWeight = 0.7, 0.3
	}
	// 权重归一化：界面滑块给出的是相对比例，这里落库成和为 1 的值，
	// 免得后续「权重和是 5」这种配置一路传到向量库。
	if total := out.VectorWeight + out.KeywordWeight; total > 0 {
		out.VectorWeight /= total
		out.KeywordWeight /= total
	}
	return out
}
