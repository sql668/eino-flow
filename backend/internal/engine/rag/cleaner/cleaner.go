// Package cleaner 在切片之前对解析结果做文本清洗。
//
// 为什么单独一层而不是塞进切片器：清洗改变的是「文本内容」，切片决定的是「怎么切」。
// 两者正交——同一份清洗结果可以用不同切法试验，混在一起会让参数互相干扰，
// 也会让工作台上的「清洗前 / 清洗后」对比失去意义。
//
// 设计要点：
//   - **每条规则必须报告影响**：命中多少次、删掉多少字符。界面靠它告诉使用者
//     「这条规则删了什么」，否则勾选规则就是盲操作（Dify 那种只有复选框的做法正是问题所在）。
//   - **规则是代码、开关是数据**（与厂商注册表同思路）：新增规则只需实现接口，
//     不用改表；配置里只记「哪些开着」。
//   - **默认全部不启用**：清洗是不可逆的语义修改，应该由使用者显式选择。
package cleaner

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/model"
)

// 规则键。配置里用它标识启用了哪条规则。
const (
	RuleNormalizeWhitespace = "normalize_whitespace"
	RuleRemoveURLs          = "remove_urls"
	RuleRemoveHTMLTags      = "remove_html_tags"
	RuleRemoveSymbolLines   = "remove_symbol_only_lines"
	RuleCollapseRepeated    = "collapse_repeated_lines"
	RuleCustomRegex         = "custom_regex"
)

// 重复行判定参数。
//
// 页眉页脚的典型特征就是「短、且反复出现」：一份 20 页的 PDF，页眉会出现 20 次。
// 阈值取 3 次是为了容忍「正文里恰好重复两三次的短句」。
const (
	repeatedLineMinHits  = 3
	repeatedLineMaxRunes = 60
	// maxSamples 是每条规则保留的删除样本上限。
	maxSamples = 5
	// sampleRunes 是样本的展示长度上限（超长截断）。
	sampleRunes = 60
)

// rememberSample 记录一条被删除的内容样本（去重、限量、截断）。
func rememberSample(result *RuleResult, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if runes := []rune(trimmed); len(runes) > sampleRunes {
		trimmed = string(runes[:sampleRunes]) + "…"
	}
	for _, existing := range result.Samples {
		if existing == trimmed {
			return
		}
	}
	if len(result.Samples) >= maxSamples {
		return
	}
	result.Samples = append(result.Samples, trimmed)
}

// RuleInfo 描述一条规则，供界面渲染勾选项。
type RuleInfo struct {
	Key   string
	Label string
	// Description 说明这条规则做什么、以及可能的副作用。
	Description string
	// NeedsPattern 为 true 时界面要显示正则输入框（仅自定义规则）。
	NeedsPattern bool
	// Risk 提示风险等级：high 表示容易误删（界面加显式提醒，默认不勾）。
	Risk string
}

// rules 是内置规则注册表。顺序即界面展示顺序（按风险从低到高）。
var rules = []RuleInfo{
	{
		Key:         RuleNormalizeWhitespace,
		Label:       "规范化空白",
		Description: "把连续的空格、换行与制表符合并：多个空行压成一个空行，行尾空白去掉。只改排版，不改语义。",
	},
	{
		Key:         RuleRemoveSymbolLines,
		Label:       "删除纯符号行",
		Description: "删除整行只有符号的内容（如 ----、====、……），这类行多来自排版分隔线，检索价值很低。",
	},
	{
		Key:         RuleCollapseRepeated,
		Label:       "删除重复出现的短行",
		Description: "把全文出现 3 次以上、且长度不超过 60 字的短行视为页眉/页脚并删除。会改变内容，建议对比预览后再启用。",
		Risk:        "high",
	},
	{
		Key:         RuleRemoveHTMLTags,
		Label:       "删除 HTML 标签",
		Description: "删除正文里的 HTML 标签（表格块会跳过：表格内容本身就是 HTML，删标签会把表格结构毁掉）。",
		Risk:        "high",
	},
	{
		Key:         RuleRemoveURLs,
		Label:       "删除 URL 与邮箱",
		Description: "删除链接与邮箱地址。若你的文档需要按链接检索，不要启用。",
		Risk:        "high",
	},
	{
		Key:          RuleCustomRegex,
		Label:        "自定义正则删除",
		Description:  "按你填写的正则表达式删除匹配内容，适合处理特定模板里的固定噪声（如「第 X 页 共 Y 页」）。",
		NeedsPattern: true,
		Risk:         "high",
	},
}

// Available 返回全部内置规则，供界面渲染。
func Available() []RuleInfo {
	out := make([]RuleInfo, len(rules))
	copy(out, rules)
	return out
}

// RuleResult 是单条规则的执行结果。
type RuleResult struct {
	Key   string
	Label string
	// Hits 是命中的次数（删了多少处）。
	Hits int
	// RemovedRunes 是删掉的字符数。
	RemovedRunes int
	// Note 是补充说明，例如「已跳过 N 个表格块」。
	Note string
	// Samples 是被删除内容的样本（最多 maxSamples 条，每条截断）。
	//
	// 只给统计数字（删了 1204 字符）使用者无法判断规则是否误伤；
	// 给出「删掉的都是这些」才敢放心勾选。这里不做完整 diff：
	// 完整 diff 需要前端逐字符比对，成本高且对判断帮助有限。
	Samples []string
}

// Report 是整次清洗的结果。
type Report struct {
	Rules []RuleResult
	// RemovedRunes 是所有规则合计删掉的字符数。
	RemovedRunes int
	// BeforeRunes / AfterRunes 是清洗前后的正文字符数（用于界面展示「从 x 字到 y 字」）。
	BeforeRunes int
	AfterRunes  int
}

// Clean 按配置清洗文档，返回清洗后的**副本**与报告。
//
// 返回副本而不是原地修改：预览要反复调用同一个配置多次（改一个参数再预览），
// 原地改会让第二次预览基于上一次的结果，参数实验就不可重复了。
func Clean(doc *loader.ParsedDocument, configs []model.CleaningRuleConfig) (*loader.ParsedDocument, Report, error) {
	report := Report{Rules: []RuleResult{}}
	if doc == nil {
		return nil, report, nil
	}

	// 深拷贝到可修改的形态：只保留需要改的部分（块的文本）。
	blocks := make([]loader.Block, len(doc.Blocks))
	copy(blocks, doc.Blocks)
	for i := range blocks {
		inlines := make([]loader.Inline, len(blocks[i].Inlines))
		copy(inlines, blocks[i].Inlines)
		blocks[i].Inlines = inlines
	}
	working := &loader.ParsedDocument{Blocks: blocks, Meta: doc.Meta, Warnings: append([]string{}, doc.Warnings...)}
	report.BeforeRunes = totalRunes(working)

	// 重复行规则需要全文视野，先算一遍行频。
	lineCounts := map[string]int{}
	if hasRule(configs, RuleCollapseRepeated) {
		lineCounts = countLines(working)
	}

	for _, config := range configs {
		info, ok := lookup(config.Key)
		if !ok {
			// 未知规则不报错：配置可能来自更新的前端，旧后端忽略掉即可，
			// 但要在报告里说明，避免使用者以为它生效了。
			report.Rules = append(report.Rules, RuleResult{
				Key:   config.Key,
				Label: config.Key,
				Note:  "当前版本不支持该规则，已忽略",
			})
			continue
		}

		result, err := apply(config, info, working, lineCounts)
		if err != nil {
			return nil, report, err
		}
		report.Rules = append(report.Rules, result)
		report.RemovedRunes += result.RemovedRunes
	}

	report.AfterRunes = totalRunes(working)
	return working, report, nil
}

// apply 执行单条规则。
func apply(
	config model.CleaningRuleConfig,
	info RuleInfo,
	doc *loader.ParsedDocument,
	lineCounts map[string]int,
) (RuleResult, error) {
	result := RuleResult{Key: config.Key, Label: info.Label}

	switch config.Key {
	case RuleNormalizeWhitespace:
		transform(doc, func(text string) (string, int) {
			cleaned := normalizeWhitespace(text)
			removed := runeLen(text) - runeLen(cleaned)
			// 这条规则不是「按模式删除」，命中数按「处理掉了多少处多余空白」计：
			// 行内连续空白 + 被压掉的空行。不给命中数的话，界面上会出现
			// 「命中 0 次却删掉 9 字」这种自相矛盾的展示。
			result.Hits += len(inlineSpacePattern.FindAllString(text, -1)) +
				countCollapsedBlankLines(text)
			return cleaned, removed
		}, &result)

	case RuleRemoveSymbolLines:
		transformLines(doc, func(line string) bool {
			hit := isSymbolOnlyLine(line)
			if hit {
				rememberSample(&result, line)
			}
			return hit
		}, &result)

	case RuleCollapseRepeated:
		removed := map[string]struct{}{}
		transformLines(doc, func(line string) bool {
			trimmed := strings.TrimSpace(line)
			if len([]rune(trimmed)) > repeatedLineMaxRunes || trimmed == "" {
				return false
			}
			if lineCounts[trimmed] < repeatedLineMinHits {
				return false
			}
			// Hits 记「不同重复行数」，RemovedRunes 记实际删掉的字符数：
			// 界面上「删了 2 类重复行、共 1,204 字符」比「命中 40 次」更好理解。
			if _, seen := removed[trimmed]; !seen {
				removed[trimmed] = struct{}{}
				rememberSample(&result, trimmed)
			}
			return true
		}, &result)
		result.Hits = len(removed)

	case RuleRemoveHTMLTags:
		skipped := 0
		transform(doc, func(text string) (string, int) {
			cleaned := htmlTagPattern.ReplaceAllString(text, "")
			cleaned = htmlEntityPattern.ReplaceAllString(cleaned, " ")
			return cleaned, runeLen(text) - runeLen(cleaned)
		}, &result, skipTableBlocksWithCount(&skipped))
		if skipped > 0 {
			result.Note = fmt.Sprintf("已跳过 %d 个表格块（表格内容本身是 HTML）", skipped)
		}

	case RuleRemoveURLs:
		transform(doc, func(text string) (string, int) {
			cleaned := urlPattern.ReplaceAllString(text, "")
			cleaned = emailPattern.ReplaceAllString(cleaned, "")
			rememberHits(&result, urlPattern, text)
			for _, hit := range urlPattern.FindAllString(text, maxSamples) {
				rememberSample(&result, hit)
			}
			for _, hit := range emailPattern.FindAllString(text, maxSamples) {
				rememberSample(&result, hit)
			}
			return cleaned, runeLen(text) - runeLen(cleaned)
		}, &result)

	case RuleCustomRegex:
		if strings.TrimSpace(config.Pattern) == "" {
			result.Note = "未填写正则，已跳过"
			return result, nil
		}
		pattern, err := regexp.Compile(config.Pattern)
		if err != nil {
			return result, fmt.Errorf("自定义正则不合法: %w", err)
		}
		transform(doc, func(text string) (string, int) {
			rememberHits(&result, pattern, text)
			for _, hit := range pattern.FindAllString(text, maxSamples) {
				rememberSample(&result, hit)
			}
			cleaned := pattern.ReplaceAllString(text, "")
			return cleaned, runeLen(text) - runeLen(cleaned)
		}, &result)
	}

	return result, nil
}

// transformOption 是 transform 的可选行为。
type transformOption func(block loader.Block) bool

// skipTableBlocksWithCount 返回一个「跳过表格块」的判定函数，并统计跳过数量。
func skipTableBlocksWithCount(counter *int) transformOption {
	return func(block loader.Block) bool {
		if block.Type == loader.BlockTable {
			*counter++
			return false
		}
		return true
	}
}

// transform 逐块变换文本；只改文本类内联元素，表格/图片等结构内容保持原样。
func transform(doc *loader.ParsedDocument, fn func(string) (string, int), result *RuleResult, opts ...transformOption) {
	shouldProcess := func(block loader.Block) bool { return true }
	if len(opts) > 0 {
		shouldProcess = opts[0]
	}

	for i := range doc.Blocks {
		block := &doc.Blocks[i]
		if !shouldProcess(*block) {
			continue
		}
		for j := range block.Inlines {
			inline := &block.Inlines[j]
			if inline.Type != loader.InlineText || inline.Text == "" {
				continue
			}
			cleaned, removed := fn(inline.Text)
			if removed == 0 {
				continue
			}
			inline.Text = cleaned
			result.RemovedRunes += removed
		}
	}
}

// transformLines 按行处理文本块：remove 返回 true 的行被整行删除。
func transformLines(doc *loader.ParsedDocument, remove func(string) bool, result *RuleResult) {
	transform(doc, func(text string) (string, int) {
		lines := strings.Split(text, "\n")
		kept := make([]string, 0, len(lines))
		removedRunes := 0
		for _, line := range lines {
			if remove(line) {
				removedRunes += runeLen(line) + 1 // +1 是行尾换行符
				continue
			}
			kept = append(kept, line)
		}
		if removedRunes == 0 {
			return text, 0
		}
		return strings.Join(kept, "\n"), removedRunes
	}, result)
}

// rememberHits 统计正则在文本里的出现次数。
func rememberHits(result *RuleResult, pattern *regexp.Regexp, text string) {
	result.Hits += len(pattern.FindAllString(text, -1))
}

// countLines 统计全文各行（去空白后）出现次数，用于识别页眉页脚。
func countLines(doc *loader.ParsedDocument) map[string]int {
	counts := map[string]int{}
	for _, block := range doc.Blocks {
		if block.Type.IsNoise() {
			continue
		}
		for _, line := range strings.Split(block.Text(), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			counts[trimmed]++
		}
	}
	return counts
}

// totalRunes 统计文档正文字符数。
func totalRunes(doc *loader.ParsedDocument) int {
	total := 0
	for _, block := range doc.Blocks {
		total += runeLen(block.Text())
	}
	return total
}

// hasRule 判断某条规则是否启用。
func hasRule(configs []model.CleaningRuleConfig, key string) bool {
	for _, config := range configs {
		if config.Key == key {
			return true
		}
	}
	return false
}

// lookup 在注册表里找规则。
func lookup(key string) (RuleInfo, bool) {
	for _, info := range rules {
		if info.Key == key {
			return info, true
		}
	}
	return RuleInfo{}, false
}

// normalizeWhitespace 合并连续空白：多个空行压成一个，行尾空格去掉。
func normalizeWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r\u00a0")
		if strings.TrimSpace(trimmed) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		// 行内的连续空格/制表符合并成一个空格。
		out = append(out, inlineSpacePattern.ReplaceAllString(trimmed, " "))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// countCollapsedBlankLines 统计被压掉的空行数量。
func countCollapsedBlankLines(text string) int {
	lines := strings.Split(text, "\n")
	collapsed := 0
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				collapsed++
			}
			blank = true
			continue
		}
		blank = false
	}
	return collapsed
}

// isSymbolOnlyLine 判断整行是否只由符号构成（分隔线之类）。
func isSymbolOnlyLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if symbolOnlyPattern.MatchString(trimmed) {
		return true
	}
	return false
}

// runeLen 返回字符数（不是字节数）。
func runeLen(text string) int { return len([]rune(text)) }

var (
	// 用 \x{00A0} 而不是 \u00a0：Go 的 regexp（RE2）不支持 \uXXXX 转义。
	inlineSpacePattern = regexp.MustCompile(`[ \t\x{00A0}]{2,}`)
	urlPattern         = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	emailPattern       = regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.-]+\b`)
	htmlTagPattern     = regexp.MustCompile(`<[^>]+>`)
	htmlEntityPattern  = regexp.MustCompile(`&[a-zA-Z#0-9]{2,8};`)
	// 纯符号行：至少 3 个符号，且不含字母数字与中日韩文字。
	symbolOnlyPattern = regexp.MustCompile(`^[\p{P}\p{S}\s]{3,}$`)
)

// SortedKeys 返回注册表里的规则键（排序），便于测试与文档生成。
func SortedKeys() []string {
	keys := make([]string, 0, len(rules))
	for _, info := range rules {
		keys = append(keys, info.Key)
	}
	sort.Strings(keys)
	return keys
}
