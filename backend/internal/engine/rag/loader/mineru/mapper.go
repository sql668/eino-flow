package mineru

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"einoflow-backend/internal/engine/rag/loader"
)

// textContent 兼容 MinerU 内联元素 content 字段的两种形态。
//
// 实测（4.0.11）同一个字段会出现两种形状：
//   - 字符串：text、table_body（HTML）、image_body
//   - 数组：table_footnote、image_footnote、hyperlink，元素形如 {type, content, styles}
//
// 数组形态在这里递归扁平化成纯文本。**内联样式（如 bold）刻意丢弃**：
// 它只影响渲染，对切片与检索没有价值；但文本本身一个都不能丢。
type textContent struct {
	Text string
}

// UnmarshalJSON 实现 string / array / null 三种形态的兼容解析。
func (t *textContent) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		t.Text = ""
		return nil
	}

	switch trimmed[0] {
	case '"':
		return json.Unmarshal(trimmed, &t.Text)
	case '[':
		var items []struct {
			Content textContent `json:"content"`
		}
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return err
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			if text := strings.TrimSpace(item.Content.Text); text != "" {
				parts = append(parts, text)
			}
		}
		t.Text = strings.Join(parts, "")
		return nil
	default:
		// 协议里没有定义其它形态。保留原文而不是报错，
		// 这样上游偶发的变化不会让整篇文档解析失败。
		t.Text = string(trimmed)
		return nil
	}
}

// middleJSON 对应 MinerU 的 docvortex.middle 协议，只声明本项目用到的字段。
// 服务端新增字段不影响反序列化；这里刻意不引入任何 MinerU 的 SDK，
// 保持「协议靠 JSON 约定」这一层轻耦合。
type middleJSON struct {
	Schema         string `json:"schema"`
	SchemaVersion  string `json:"schema_version"`
	IsFullDocument bool   `json:"is_full_document"`
	Metadata       struct {
		FileSuffix string `json:"file_suffix"`
		Producer   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"producer"`
		Document struct {
			PageCount int `json:"page_count"`
		} `json:"document"`
	} `json:"metadata"`
	Extensions struct {
		MinerU struct {
			Tier      string `json:"tier"`
			ParseMode string `json:"parse_mode"`
		} `json:"mineru"`
	} `json:"extensions"`
	Pages []struct {
		PageIdx int `json:"page_idx"`
		Blocks  []struct {
			Type    string    `json:"type"`
			Index   int       `json:"index"`
			Level   int       `json:"level"`
			BBox    []float64 `json:"bbox"`
			Content []struct {
				Type    string      `json:"type"`
				Index   int         `json:"index"`
				BBox    []float64   `json:"bbox"`
				Content textContent `json:"content"`
			} `json:"content"`
		} `json:"blocks"`
	} `json:"pages"`
}

// 协议识别常量。
// MinerU 官方明确要求：JSON 消费端通过 schema 与 schema_version 两个键识别数据格式。
// 因此这里做显式校验——格式变了要立刻报错，而不是猜着解析出一堆错数据。
const (
	expectedSchema      = "docvortex.middle"
	expectedSchemaMajor = 2
)

// toDocument 把 MinerU 的 middle_json 映射成项目自己的 IR。
func toDocument(result *ParseResult, req loadRequestInfo) (*loader.ParsedDocument, error) {
	var raw middleJSON
	if err := json.Unmarshal(result.MiddleJSON, &raw); err != nil {
		return nil, fmt.Errorf("解析 middle_json 失败: %w", err)
	}

	if raw.Schema != expectedSchema {
		return nil, fmt.Errorf("middle_json schema 不匹配：期望 %q，实际 %q（协议可能已变更）",
			expectedSchema, raw.Schema)
	}
	major, minor, err := parseSchemaVersion(raw.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if major != expectedSchemaMajor {
		return nil, fmt.Errorf("middle_json 主版本不兼容：期望 %d.x，实际 %s", expectedSchemaMajor, raw.SchemaVersion)
	}

	doc := &loader.ParsedDocument{
		Meta: loader.DocumentMeta{
			Parser:        "mineru",
			ParserVersion: raw.Metadata.Producer.Version,
			Tier:          raw.Extensions.MinerU.Tier,
			ParseMode:     raw.Extensions.MinerU.ParseMode,
			PageCount:     raw.Metadata.Document.PageCount,
			SourceName:    req.Filename,
			FileType:      req.FileType,
		},
	}
	if doc.Meta.ParserVersion == "" {
		// 产物里没带版本时回落到客户端记录的值，避免元信息缺失。
		doc.Meta.ParserVersion = result.ParserVersion
	}
	if doc.Meta.Tier == "" {
		doc.Meta.Tier = result.Tier
	}

	// 小版本变化只提示不报错：同主版本内服务端承诺向后兼容。
	if minor != expectedSchemaMinor {
		doc.Warnings = append(doc.Warnings,
			fmt.Sprintf("middle_json 小版本为 %s，本项目按 %d.%d 适配，字段可能有变化",
				raw.SchemaVersion, expectedSchemaMajor, expectedSchemaMinor))
	}
	if !raw.IsFullDocument {
		doc.Warnings = append(doc.Warnings, "服务端返回的是部分页解析结果（is_full_document=false）")
	}
	doc.Warnings = append(doc.Warnings, result.Warnings...)

	unknownBlocks := map[string]bool{}
	unknownInlines := map[string]bool{}

	for _, page := range raw.Pages {
		for _, block := range page.Blocks {
			blockType := loader.BlockType(block.Type)
			if !blockType.IsKnown() {
				unknownBlocks[block.Type] = true
			}

			inlines := make([]loader.Inline, 0, len(block.Content))
			for _, item := range block.Content {
				inlineType := loader.InlineType(item.Type)
				if !inlineType.IsKnown() {
					unknownInlines[item.Type] = true
				}
				inlines = append(inlines, loader.Inline{
					Type:  inlineType,
					Text:  item.Content.Text,
					BBox:  item.BBox,
					Index: item.Index,
				})
			}

			doc.Blocks = append(doc.Blocks, loader.Block{
				Type: blockType,
				// 空内容块（例如只有图片的块）也保留：它带着页码与坐标，
				// 去掉就丢失了「这里有张图」的信息，是否跳过交给切片策略决定。
				Inlines: inlines,
				Level:   block.Level,
				// MinerU 的 page_idx 是 0 基，对外统一转成 1 基页码。
				Page:  page.PageIdx + 1,
				Index: block.Index,
				BBox:  block.BBox,
			})
		}
	}

	// 未知类型只提示不失败：上游新增块类型时我们应当仍能入库，
	// 但要在日志里看得见，而不是静默当成正文处理。
	if len(unknownBlocks) > 0 {
		doc.Warnings = append(doc.Warnings,
			"出现未识别的块类型（已按原样透传）: "+joinSortedKeys(unknownBlocks))
	}
	if len(unknownInlines) > 0 {
		doc.Warnings = append(doc.Warnings,
			"出现未识别的内联类型（已按原样透传）: "+joinSortedKeys(unknownInlines))
	}

	if len(doc.Blocks) == 0 {
		return nil, fmt.Errorf("解析结果没有任何内容块（文件 %q）", req.Filename)
	}
	return doc, nil
}

// expectedSchemaMinor 是与 expectedSchemaMajor 对应的适配小版本。
const expectedSchemaMinor = 0

// parseSchemaVersion 解析 "2.0" 形式的协议版本。
func parseSchemaVersion(version string) (major, minor int, err error) {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 2)
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("无法解析 schema_version %q: %w", version, err)
	}
	if len(parts) == 1 {
		return major, 0, nil
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("无法解析 schema_version %q: %w", version, err)
	}
	return major, minor, nil
}

// joinSortedKeys 把集合拼成稳定的字符串，便于日志比对与测试断言。
func joinSortedKeys(set map[string]bool) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
