package keyword

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/go-ego/gse"
)

// defaultDomainWords 是通用词典未必收录、但本项目文档里常见的领域词。
//
// 先代码内置（design D4）：当前唯一的维护者是项目自己，
// 做成配置项会引出"存哪、谁维护、版本怎么对齐"的问题；等出现真实需求再升级。
var defaultDomainWords = []string{
	"万用表", "示波器", "函数发生器", "表笔", "量程", "档位",
	"断开电源", "更换电池", "电池仓", "保险丝",
	// 对比实测里被通用词典切碎的仪器术语（见 PROGRESS 9.10）。
	"背光灯", "背光灯键", "功能转盘", "短路蜂鸣", "二极管测试",
	"小数点", "待测物", "被测物", "显示屏", "电池盖", "后盖",
	"螺丝刀", "指示灯", "高分辨率", "测量值",
}

// DictionaryTokenizer 是基于词典的中文分词器。
//
// 与 LocalTokenizer 的区别只在"候选怎么来"：这里由 gse 的内嵌词表给出词边界，
// 后置过滤（子串抑制、虚词边界、停用词）仍然复用同一套规则，
// 保证换实现不会把已经压掉的噪声重新放出来。
type DictionaryTokenizer struct {
	seg gse.Segmenter
}

// NewDictionaryTokenizer 构造词典分词器。
//
// 初始化只做一次（应用启动时）：加载内嵌词表 + 追加领域词要几十毫秒到几百毫秒，
// 放在请求路径上会让每次入库都多一段固定延迟，也会让并发请求互相抢初始化。
func NewDictionaryTokenizer(customWords ...string) (*DictionaryTokenizer, error) {
	started := time.Now()
	// "zh_s" 走内嵌的简体中文词表（比 zh 少一份繁体词表，启动更快）；
	// AlphaNum 让 ASCII 串按字母数字边界处理。
	//
	// MinTokenFreq 过滤生僻词：完整词表有 35 万个词，常驻内存约 +220MB；
	// 提到 30 后保留 6.8 万个常用词，内存与启动时间明显下降，
	// 同时仍能覆盖手册里的专业词（实测 100 会把「前请」「先断」这类相邻词粘在一起）。
	var seg gse.Segmenter
	seg.AlphaNum = true
	seg.MinTokenFreq = 30
	if err := seg.LoadDictEmbed("zh_s"); err != nil {
		return nil, fmt.Errorf("初始化词典分词器失败: %w", err)
	}
	words := make([]string, 0, len(defaultDomainWords)+len(customWords))
	words = append(words, defaultDomainWords...)
	words = append(words, customWords...)
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if err := seg.AddToken(word, 1000, "n"); err != nil {
			return nil, fmt.Errorf("追加自定义词 %q 失败: %w", word, err)
		}
	}
	// 所有领域词加完后统一重算一次词典长度信息：
	// 逐个 AddTokenForce 会每次都全量重算，启动时间随词数线性增长。
	seg.CalcToken()
	log.Printf("[keyword] 词典分词器初始化完成（%d 个词典词 + %d 个领域词），耗时 %d ms",
		seg.Dict.NumTokens(), len(words), time.Since(started).Milliseconds())
	return &DictionaryTokenizer{seg: seg}, nil
}

// Tokenize 实现 Tokenizer。
func (t *DictionaryTokenizer) Tokenize(ctx context.Context, texts []string) ([][]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	groups := make([][]string, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 与 LocalTokenizer 相同的预处理：剥掉 MinerU 的 HTML 标签。
		clean := stripMarkup(text)
		tokens := make([]string, 0, 32)
		for _, token := range t.seg.Cut(clean, true) {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" || !hasHan(trimmed) {
				// ASCII 统一交给 splitASCIITokens：与旧实现保持完全一致的词边界。
				continue
			}
			// 只保留词典里真实存在的词：gse 的 HMM 会把生僻组合切出来
			// （实测「测物断」「盖装」「紧螺丝」），它们不是词典词，
			// 继续当候选会把旧实现的截断片段问题原样带回来。
			if !knownWord(t.seg, trimmed) {
				continue
			}
			// 词典词仍然过一遍首尾虚词过滤，保持与旧实现相同的后置规则。
			if hasFunctionEdge(trimmed) {
				continue
			}
			tokens = append(tokens, trimmed)
		}
		tokens = append(tokens, splitASCIITokens(clean)...)
		groups[i] = tokens
	}
	return suppressCoveredTokens(groups), nil
}

// knownWord 判断词是否在词典里。
func knownWord(seg gse.Segmenter, word string) bool {
	_, _, ok := seg.Dict.Find([]byte(word))
	return ok
}

// hasHan 判断 token 是否包含汉字。
func hasHan(token string) bool {
	for _, r := range token {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// suppressCoveredTokens 去掉总被更长词包住的短词。
//
// 与 LocalTokenizer 的"长词优先"同一条规则，只是统计对象从"候选片段"变成"词典词"：
// 短词若被更长词包含，且长词频次至少有短词的一半，就认为长词才是这个词。
// 例：文档里大量出现「直流电压」时，单独保留「电压」会把权重落到更泛的粒度上。
func suppressCoveredTokens(groups [][]string) [][]string {
	freq := make(map[string]int, 128)
	for _, group := range groups {
		for _, token := range group {
			freq[token]++
		}
	}
	unique := make([]string, 0, len(freq))
	for token := range freq {
		unique = append(unique, token)
	}
	sortByLengthDesc(unique)

	kept := make(map[string]struct{}, len(unique))
	for _, token := range unique {
		covered := false
		for longer := range kept {
			if !strings.Contains(longer, token) {
				continue
			}
			if freq[longer]*2 >= freq[token] {
				covered = true
				break
			}
		}
		if !covered {
			kept[token] = struct{}{}
		}
	}

	out := make([][]string, len(groups))
	for i, group := range groups {
		tokens := make([]string, 0, len(group))
		for _, token := range group {
			if _, ok := kept[token]; ok {
				tokens = append(tokens, token)
			}
		}
		out[i] = tokens
	}
	return out
}
