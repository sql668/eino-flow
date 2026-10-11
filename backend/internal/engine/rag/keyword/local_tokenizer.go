package keyword

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// markupTagRE / htmlEntityRE 用来剥离分段里的 HTML 片段。
//
// MinerU 对表格会直接输出 <table><tr><td> 之类的标签，标签名（td、rowspan）
// 与转义实体（&gt;、&nbsp;）都不是文档内容，留在文本里就会变成关键词。
// 上限 200 字符是为了避免遇到未闭合的「<」时把整段文字吃掉。
var (
	markupTagRE  = regexp.MustCompile(`<[^>]{0,200}>`)
	htmlEntityRE = regexp.MustCompile(`&[a-zA-Z#0-9]{1,8};`)
)

// stripMarkup 去掉 HTML 标签与转义实体，保留标签之间的正文。
func stripMarkup(text string) string {
	if !strings.ContainsRune(text, '<') && !strings.ContainsRune(text, '&') {
		return text
	}
	cleaned := markupTagRE.ReplaceAllString(text, " ")
	return htmlEntityRE.ReplaceAllString(cleaned, " ")
}

// LocalTokenizer 是不依赖任何外部服务的分词器：无词典的「新词发现」式切词。
//
// 为什么不用 Milvus 的中文分析器（原方案）：Milvus 2.5.8 没有对外开放分析器接口，
// 调用 RunAnalyzer 直接被服务端拒绝（Unimplemented —— 该 RPC 是更高版本才有的），
// 而为了一句关键词抽取去升级整个向量库（还要在腾讯云那台 2C2G 机器上跑）不划算。
//
// 算法（在**整篇文档**上做一次，这也正是 TF-IDF 需要的统计口径）：
//  1. 文本按字符类别切成「CJK 串」与「ASCII 串」；
//  2. CJK 串内穷举 2~6 字的候选串，统计出现次数与左右相邻字符集合；
//  3. 只保留重复出现（≥2 次）且左右邻字足够多样的候选：前者排除一次性措辞，
//     后者排除「请在使用本表之前」这类固定长片段（它的左右邻字永远只有一种）；
//  4. 去掉总被更长候选包住的短串：「万用」几乎只出现在「万用表」里 ⇒ 留「万用表」；
//  5. ASCII 串按非字母数字直接切词（型号、缩写本来就有词边界）。
//
// 与 BM25 的分词口径**不需要一致**：关键词会被写进 BM25 的输入文本，
// 服务端仍用自己的中文分析器切一遍；查询侧同样由服务端切词，
// 真正要紧的「索引侧与查询侧同口径」这条约束没有被破坏。
// 代价是抽取质量不如带词典的分词器（只出现一次的词抽不出来），
// 需要更强时要做的事只是换一个 Tokenizer 实现（接口就是为它留的）。
type LocalTokenizer struct{}

// NewLocalTokenizer 构造本地分词器。
func NewLocalTokenizer() *LocalTokenizer {
	return &LocalTokenizer{}
}

// 候选词的字符数区间。
//
// 下界 2：中文单字（的、表、值）区分度太低。
// 上界 6：再长多半是短语甚至半句话，不像关键词，而且穷举开销平方级增长。
const (
	minTermRunes = 2
	maxTermRunes = 6
	// minTermFreq 是候选词在整篇文档中的最小出现次数。
	//
	// 取 2：出现两次已经排除了「一次性措辞」（绝大多数句子片段只出现一次），
	// 再往上提会让小文档（几页的手册）几乎抽不出词。
	// 那些「恰好在两处出现的片段」由邻字多样性与首尾虚词两条规则兜住。
	minTermFreq = 3
	// maxCandidates 是候选词表容量上限。
	//
	// 无上限时，一份几百页的文档可能攒出几十万个候选，内存会被吃光；
	// 超过上限后新候选不再进入（先到先得），对关键词质量的影响可以忽略。
	maxCandidates = 200000
	// minNeighborFreedom 是候选词左右相邻字符的**多样性**下限（边界也算一种相邻）。
	//
	// 取 4 是实测折中：固定长片段（「请在使用本表」总被「之」接着、且总在句首）
	// 只有 2 种相邻，会被挡在门外；而真正的词会出现在多种上下文里。
	minNeighborFreedom = 4
)

// functionRunes 是**不能出现在词首或词尾**的虚词/功能字。
//
// 无词典分词最容易产出的垃圾就是「的安全」「的量程」「使用本」这类
// 「实词 + 虚词」的跨界片段：它们统计特征不错，但显然不是词。
// 中文的虚词是个封闭集合，用一张小表卡住首尾即可以很低的代价去掉这类噪声。
// 只判首尾、不判中间：像「安全手册」这种词中间本来就可能出现功能字。
var functionRunes = map[rune]struct{}{
	'的': {}, '地': {}, '得': {}, '了': {}, '着': {}, '过': {},
	'和': {}, '与': {}, '及': {}, '或': {}, '是': {}, '在': {}, '为': {}, '对': {}, '以': {},
	'并': {}, '而': {}, '等': {}, '之': {}, '其': {}, '该': {}, '各': {}, '将': {}, '被': {},
	'把': {}, '从': {}, '到': {}, '这': {}, '那': {}, '您': {}, '我': {}, '他': {}, '她': {},
	'它': {}, '们': {}, '请': {}, '本': {}, '此': {}, '若': {}, '按': {}, '由': {}, '向': {},
	'于': {}, '则': {}, '均': {}, '亦': {}, '也': {}, '都': {}, '还': {}, '再': {}, '又': {},
	'且': {}, '如': {},
}

// termStats 是候选词在整篇文档里的统计。
type termStats struct {
	freq int
	// left / right 是左右相邻字符集合：集合越大说明这个词越独立
	// （总被同一串包着说明它是更长固定片段的一部分，不该单独成词）。
	left  map[rune]struct{}
	right map[rune]struct{}
	// docs 记录它出现在哪些文本（段）里，避免后面用 strings.Contains 逐段回扫。
	docs map[int]struct{}
}

// Tokenize 实现 Tokenizer。
//
// 入参是一篇文档的全部片段：统计口径必须是整篇文档，否则「重复出现」这个判据
// 会退化成「在这一段里重复」，抽出来的词会明显变差。
func (t *LocalTokenizer) Tokenize(ctx context.Context, texts []string) ([][]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	stats := make(map[string]*termStats, 1024)
	asciiTokens := make([][]string, len(texts))
	// 统计口径是**整篇文档**：把各段拼成一段再挖词。
	//
	// 只在单段（几百字）里统计的话，「重复出现」这个判据几乎永远不成立，
	// 抽出来的就是一堆句子碎片；拼成整篇后，术语的重复性才显现出来。
	// 段与段之间用换行分隔：它不是汉字，天然切断 CJK 串，不会跨段拼出假词。
	var full strings.Builder
	offsets := make([]int, len(texts)+1)
	for index, text := range texts {
		// 表格类分段里带着 MinerU 输出的 HTML（<td>、rowspan…）：
		// 那不是内容，先剥掉再分词，否则「td / tr / rowspan」会挤满关键词。
		clean := stripMarkup(text)
		asciiTokens[index] = splitASCIITokens(clean)
		offsets[index] = full.Len()
		full.WriteString(clean)
		full.WriteString("\n")
	}
	offsets[len(texts)] = full.Len()
	// 长文档上这一步是纯 CPU：留一个取消点，别让用户取消上传后还在算。
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mineTerms(full.String(), offsets, stats)

	terms := selectTerms(stats)

	// 组装每段的词表：保留下来的候选词（出现在该段的）+ 该段的 ASCII 词。
	out := make([][]string, len(texts))
	perDoc := make([][]string, len(texts))
	// 先按固定顺序排好再分发：直接遍历 map 的话顺序随机，
	// 「同样的输入得到同样的输出」这条契约就没了（调用方虽然会再排序，
	// 但一个会随机换序的分词器本身就是个坑）。
	ordered := make([]string, 0, len(terms))
	for term := range terms {
		ordered = append(ordered, term)
	}
	sortByLengthDesc(ordered)
	for _, term := range ordered {
		for docIndex := range terms[term].docs {
			perDoc[docIndex] = append(perDoc[docIndex], term)
		}
	}
	for index := range texts {
		tokens := make([]string, 0, len(perDoc[index])+len(asciiTokens[index]))
		tokens = append(tokens, perDoc[index]...)
		tokens = append(tokens, asciiTokens[index]...)
		out[index] = tokens
	}
	return out, nil
}

// mineTerms 穷举整篇文本里的候选词并累计统计。
//
// offsets 是各段在拼接文本中的起始位置（末尾补一个总长度），
// 用来把候选词出现的位置换算成「属于第几段」——这样既能拿到文档级的词频，
// 又能知道某个词出现在哪些段里（TF-IDF 的文档频率就靠它）。
func mineTerms(text string, offsets []int, stats map[string]*termStats) {
	runes := []rune(text)
	// offsets 是按字节算的，转成按字符的边界表，后面才能与 rune 下标比较。
	docStarts := make([]int, len(offsets))
	byteToRune := func(bytePos int) int { return utf8.RuneCountInString(text[:bytePos]) }
	for i, pos := range offsets {
		docStarts[i] = byteToRune(pos)
	}
	// 先按「CJK 串」分段：只有同一串内部的相邻字符才算邻字，
	// 跨过标点/数字去统计邻字会把「句末 + 句首」误当成构词证据。
	type span struct{ start, end int }
	spans := make([]span, 0, 16)
	start := -1
	for i, r := range runes {
		if isHan(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			spans = append(spans, span{start, i})
			start = -1
		}
	}
	if start >= 0 {
		spans = append(spans, span{start, len(runes)})
	}

	for _, item := range spans {
		length := item.end - item.start
		for size := minTermRunes; size <= maxTermRunes && size <= length; size++ {
			for i := item.start; i+size <= item.end; i++ {
				term := string(runes[i : i+size])
				stat, ok := stats[term]
				if !ok {
					if len(stats) >= maxCandidates {
						continue
					}
					stat = &termStats{
						left:  make(map[rune]struct{}, 2),
						right: make(map[rune]struct{}, 2),
						docs:  make(map[int]struct{}, 1),
					}
					stats[term] = stat
				}
				stat.freq++
				stat.docs[docIndexAt(docStarts, i)] = struct{}{}
				// 越出 CJK 串的邻位记为「边界」，边界也是一种有效邻字：
				// 一个词总出现在句首同样说明它可以独立成词。
				if i-1 >= item.start {
					stat.left[runes[i-1]] = struct{}{}
				} else {
					stat.left['\x00'] = struct{}{}
				}
				if i+size < item.end {
					stat.right[runes[i+size]] = struct{}{}
				} else {
					stat.right['\x00'] = struct{}{}
				}
			}
		}
	}
}

// docIndexAt 把字符下标换算成所属段号。
//
// 用线性扫描而不是二分：候选词的相邻几次出现位置通常集中，
// 而且 docStarts 很短（等于段数），实际开销可以忽略。
func docIndexAt(docStarts []int, runeIndex int) int {
	index := 0
	for i := 1; i < len(docStarts); i++ {
		if runeIndex < docStarts[i] {
			break
		}
		index = i
	}
	return index
}

// selectTerms 从候选里挑出最终保留的词。
func selectTerms(stats map[string]*termStats) map[string]*termStats {
	// 第一轮：出现次数与邻字多样性。
	kept := make([]string, 0, len(stats))
	for term, stat := range stats {
		if stat.freq < minTermFreq {
			continue
		}
		// 左右邻字多样性不足的候选不是词（见 minNeighborFreedom 的说明）。
		if len(stat.left)+len(stat.right) < minNeighborFreedom {
			continue
		}
		// 首尾是虚词的候选是跨界片段（见 functionRunes 的说明）。
		if hasFunctionEdge(term) {
			continue
		}
		kept = append(kept, term)
	}

	// 第二轮：长词优先，短词若总被更长的词包住（且频次不高于它）就丢掉。
	// 例：「万用」「用表」的频次与「万用表」接近 ⇒ 只留「万用表」。
	sortByLengthDesc(kept)
	result := make(map[string]*termStats, len(kept))
	for _, term := range kept {
		covered := false
		for longer := range result {
			if !strings.Contains(longer, term) {
				continue
			}
			// 阈值取 0.5 而不是「差不多同频」：截断词是最刺眼的噪声
			// （「断开电」「交流电压测」），只要更长的候选有它一半的频次，
			// 就认为长的那个才是词。
			if result[longer].freq*2 >= stats[term].freq {
				covered = true
				break
			}
		}
		if !covered {
			result[term] = stats[term]
		}
	}
	return result
}

// hasFunctionEdge 判断候选词的首尾字符是不是虚词。
func hasFunctionEdge(term string) bool {
	runes := []rune(term)
	if len(runes) == 0 {
		return true
	}
	if _, ok := functionRunes[runes[0]]; ok {
		return true
	}
	_, ok := functionRunes[runes[len(runes)-1]]
	return ok
}

// sortByLengthDesc 按「长度降序 + 字典序」排序，保证同一份文档每次得到同样的结果。
func sortByLengthDesc(terms []string) {
	// 手写插入排序会退化；用标准库排序并显式给稳定比较函数。
	sort.Slice(terms, func(i, j int) bool {
		left, right := []rune(terms[i]), []rune(terms[j])
		if len(left) != len(right) {
			return len(left) > len(right)
		}
		return terms[i] < terms[j]
	})
}

// splitASCIITokens 把文本里的连续字母/数字切出来。
//
// 只保留长度 ≥2 的片段：单个字母（型号里的 a、b）没有信息量，
// 而纯数字片段会被后面的 normalizeTokens 过滤掉（页码不是关键词）。
func splitASCIITokens(text string) []string {
	tokens := make([]string, 0, 8)
	var builder strings.Builder
	flush := func() {
		if builder.Len() >= 2 {
			tokens = append(tokens, builder.String())
		}
		builder.Reset()
	}
	for _, r := range text {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			builder.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// isHan 判断是否为汉字。
func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
}
