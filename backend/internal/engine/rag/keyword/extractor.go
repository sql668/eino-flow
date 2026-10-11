// Package keyword 从分段正文里抽取关键词。
//
// 方案（2026-10-11 讨论定稿）：
//   - **分词由 Tokenizer 提供**：默认实现是本地无词典分词（见 LocalTokenizer），
//     因为它不依赖外部服务；关键词会写进 BM25 的输入文本、由服务端再切一次，
//     所以「与索引同口径」这件事在检索链路上依然成立；
//   - **打分用文档内 TF-IDF**：把一份文档的所有分段看作文档集，词频来自本段、
//     逆文档频率来自该文档内出现过该词的段落比例；
//   - **不调 LLM**：入库路径上每段一次模型调用不可行（本地 8B 每段 5~10 秒，
//     几百段就是几十分钟），而关键词只是辅助信息。留出 Extractor 接口，
//     将来要加「高质量（LLM 抽取）」可以并存，不推翻这里的实现。
package keyword

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"
)

// DefaultMaxCount 是默认抽取的关键词数量上限。
const DefaultMaxCount = 5

// minTokenRunes 是中文词的最小长度：单字信息量太低（中文里大量单字是虚词）。
const minTokenRunes = 2

// errTokenCountMismatch 表示分词服务返回的条数与入参不一致。
var errTokenCountMismatch = errors.New("分词结果条数与输入文本数不一致")

// Tokenizer 是分词能力（由 Milvus 分析器实现）。
//
// 定义成窄接口而不是直接依赖向量库：抽取逻辑要能单测，而单测不该需要连 Milvus。
type Tokenizer interface {
	// Tokenize 批量分词，返回结果与入参一一对应。
	Tokenize(ctx context.Context, texts []string) ([][]string, error)
}

// Extractor 抽取关键词。
//
// 接口留在这一层：将来可以加 LLM 抽取实现（更贵但更准），调用方按配置选择，
// 流水线不用改。
type Extractor interface {
	// Extract 为一批同属一篇文档的正文抽关键词，返回结果与入参一一对应。
	Extract(ctx context.Context, texts []string, maxCount int) ([][]string, error)
}

// TFIDFExtractor 是基于分词 + TF-IDF 的实现。
type TFIDFExtractor struct {
	tokenizer Tokenizer
}

// NewTFIDF 构造抽取器。
func NewTFIDF(tokenizer Tokenizer) *TFIDFExtractor {
	return &TFIDFExtractor{tokenizer: tokenizer}
}

// Extract 实现 Extractor。
func (e *TFIDFExtractor) Extract(ctx context.Context, texts []string, maxCount int) ([][]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if maxCount <= 0 {
		maxCount = DefaultMaxCount
	}
	if e.tokenizer == nil {
		return nil, errors.New("缺少分词器")
	}

	tokenGroups, err := e.tokenizer.Tokenize(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(tokenGroups) != len(texts) {
		// 数量不一致说明分词服务行为异常，宁可报错也不要错位配对——
		// 错位会把 A 段的关键词安到 B 段上，而且看起来完全正常。
		return nil, errTokenCountMismatch
	}

	// 文档内统计：某个词在多少段里出现过（作为 IDF 的文档频率）。
	docFreq := make(map[string]int, 256)
	normalized := make([][]string, len(tokenGroups))
	for i, tokens := range tokenGroups {
		normalized[i] = normalizeTokens(tokens)
		seen := make(map[string]struct{}, len(normalized[i]))
		for _, token := range normalized[i] {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			docFreq[token]++
		}
	}

	total := float64(len(texts))
	result := make([][]string, len(normalized))
	for i, tokens := range normalized {
		result[i] = rankTokens(tokens, docFreq, total, maxCount)
	}
	return result, nil
}

// rankTokens 按 TF-IDF 给一段文本的词排序并取前 n 个。
func rankTokens(tokens []string, docFreq map[string]int, total float64, maxCount int) []string {
	termFreq := make(map[string]int, len(tokens))
	for _, token := range tokens {
		termFreq[token]++
	}

	type scored struct {
		token string
		score float64
	}
	ranked := make([]scored, 0, len(termFreq))
	for token, tf := range termFreq {
		// 平滑 IDF：避免「只在本文档出现的词」拿到无穷大权重。
		idf := math.Log((total+1)/(float64(docFreq[token])+1)) + 1
		// 词频取对数：一段里出现 10 次不该比出现 2 次重要 5 倍。
		score := (1 + math.Log(float64(tf))) * idf
		ranked = append(ranked, scored{token: token, score: score})
	}

	// 排序必须稳定：同分时按词本身排，保证同一份文档重复抽取结果一致——
	// 否则每次入库关键词都可能变，人工维护过的关键词会被下一次入库冲掉。
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].token < ranked[j].token
	})

	out := make([]string, 0, maxCount)
	for _, item := range ranked {
		if len(out) >= maxCount {
			break
		}
		out = append(out, item.token)
	}
	return out
}

// normalizeTokens 过滤停用词与无意义 token。
func normalizeTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, stop := stopwords[lower]; stop {
			continue
		}
		if !hasMeaningfulChar(trimmed) {
			// 纯标点、纯数字、纯空白：不进关键词。
			continue
		}
		// 中文单字过滤掉；拉丁词允许 2 个字符（go、ai 这类有实义）。
		if runes := []rune(trimmed); len(runes) < minTokenRunes && !isLatin(trimmed) {
			continue
		}
		out = append(out, lower)
	}
	return out
}

// hasMeaningfulChar 判断 token 里是否有字母或汉字（排除纯数字与标点）。
func hasMeaningfulChar(token string) bool {
	for _, r := range token {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// isLatin 判断是否为拉丁词。
func isLatin(token string) bool {
	for _, r := range token {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
