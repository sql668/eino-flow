package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// RetrievalToolName 是知识库检索工具名。
//
// 名字要能让模型一眼判断用途：agent 同时拿到检索与计算两个工具时，
// 叫 "search" 太泛，叫 "knowledge_retrieval" 才能和"查知识库"这件事对上。
const RetrievalToolName = "knowledge_retrieval"

// 检索范围相关的领域错误。
var (
	// ErrMultipleDatasets 表示一次问答传入了多个知识库。
	//
	// 当前版本刻意只支持一个（见 change add-react-agent-chat 的 D9），
	// 但工具契约按列表设计，V1.0 放开多知识库时不用改参数结构。
	ErrMultipleDatasets = errors.New("当前版本一次问答只支持一个知识库")
	// ErrMissingDataset 表示调用检索工具时没有给出知识库。
	ErrMissingDataset = errors.New("检索工具需要指定一个知识库")
)

// RetrievalHit 是检索工具返回给模型的一条命中。
//
// 定义在工具层而不是直接复用 rag.Hit：工具只依赖"检索能力"这个窄契约，
// service 负责把 rag 的检索结果映射过来（留痕、重排开关仍留在 service）。
type RetrievalHit struct {
	ChunkID      string
	SegmentID    uint64
	DocumentID   uint64
	DocumentName string
	Content      string
	Page         int
	TitlePath    []string
	Kind         string
}

// Searcher 是检索工具依赖的最小能力。
//
// 只要「按知识库与问题返回带出处的片段」这一件事：
// 具体走向量 / 全文 / 混合、是否重排、怎么写留痕，都由注入的实现决定。
type Searcher interface {
	// Search 返回与 query 相关的片段；没有命中时返回空切片而不是错误。
	Search(ctx context.Context, datasetID uint64, query string) ([]RetrievalHit, error)
}

// retrievalArgs 是检索工具的入参。
//
// DatasetIDs 用列表是为了给 V1.0 的多知识库留扩展位；当前版本只接受一个，
// 多于一个明确报错，绝不静默取第一个——那会让用户以为检索范围是 A+B，实际只有 A。
type retrievalArgs struct {
	DatasetIDs []uint64 `json:"dataset_ids"`
	Query      string   `json:"query"`
}

// NewKnowledgeRetrievalTool 构造知识库检索工具。
func NewKnowledgeRetrievalTool(searcher Searcher) (tool.InvokableTool, error) {
	if searcher == nil {
		return nil, fmt.Errorf("构造检索工具失败: 缺少检索实现")
	}

	retrievalTool, err := utils.InferTool(
		RetrievalToolName,
		"检索知识库中的相关资料。需要依据文档回答问题时使用；dataset_ids 当前只能传一个知识库 ID。",
		func(ctx context.Context, args retrievalArgs) (string, error) {
			datasetID, err := singleDatasetID(args.DatasetIDs)
			if err != nil {
				return "", err
			}
			query := strings.TrimSpace(args.Query)
			if query == "" {
				return "", fmt.Errorf("检索内容不能为空")
			}

			hits, err := searcher.Search(ctx, datasetID, query)
			if err != nil {
				return "", fmt.Errorf("知识库检索失败: %w", err)
			}
			return formatRetrievalResult(hits), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("构造检索工具失败: %w", err)
	}
	return retrievalTool, nil
}

// singleDatasetID 校验检索范围。
//
// 校验必须在调用检索之前完成：传入两个知识库时，任何一次检索都不应发生，
// 否则日志里会出现"明明说了不支持还查了一次"的痕迹。
func singleDatasetID(datasetIDs []uint64) (uint64, error) {
	if err := validateDatasetIDs(datasetIDs, true); err != nil {
		return 0, err
	}
	return datasetIDs[0], nil
}

// formatRetrievalResult 把命中片段拼成模型可读的文本。
//
// 这里保留完整片段正文（不回灌展示层用的截断摘要）：
// 模型需要完整上下文才能作答，而界面上的摘要另有截断规则，两者刻意分开。
func formatRetrievalResult(hits []RetrievalHit) string {
	if len(hits) == 0 {
		return "知识库检索没有命中任何片段。可以换一组关键词重试，或如实说明资料中没有相关内容。"
	}

	var builder strings.Builder
	builder.WriteString("知识库检索结果：\n")
	for i, hit := range hits {
		builder.WriteString(fmt.Sprintf("[%d] 来源：%s\n", i+1, describeRetrievalSource(hit)))
		builder.WriteString(strings.TrimSpace(hit.Content))
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

// describeRetrievalSource 把出处拼成一行。
//
// 与问答链路的提示词来源描述是两处不同场景：工具结果要能被模型直接引用，
// 因此始终给编号与文档名；缺少页码 / 标题时直接省略，不写"第 0 页"。
func describeRetrievalSource(hit RetrievalHit) string {
	parts := make([]string, 0, 3)
	documentName := strings.TrimSpace(hit.DocumentName)
	if documentName == "" {
		documentName = "未知文档"
	}
	parts = append(parts, documentName)
	if hit.Page > 0 {
		parts = append(parts, fmt.Sprintf("第 %d 页", hit.Page))
	}
	if len(hit.TitlePath) > 0 {
		parts = append(parts, strings.Join(hit.TitlePath, " / "))
	}
	return strings.Join(parts, " · ")
}

// ToolNamesForChat 返回本次问答应启用的工具名（按注册顺序）。
//
// 检索工具只在本次指定了知识库时启用：未指定时不注册，比"注册后永远返回空结果"诚实，
// 也避免模型白调一次工具。计算工具始终可用，用于验证工具机制本身。
func ToolNamesForChat(datasetIDs []uint64) ([]string, error) {
	if err := validateDatasetIDs(datasetIDs, false); err != nil {
		return nil, err
	}

	names := make([]string, 0, 2)
	if len(datasetIDs) == 1 {
		names = append(names, RetrievalToolName)
	}
	names = append(names, calculatorName)
	return names, nil
}

// validateDatasetIDs 校验检索范围。
//
// required 区分两种调用场景：工具执行时知识库必填；
// 组装工具集时允许为空（空表示本次不提供检索工具）。
func validateDatasetIDs(datasetIDs []uint64, required bool) error {
	if len(datasetIDs) > 1 {
		return fmt.Errorf("%w（本次收到 %d 个：%v）", ErrMultipleDatasets, len(datasetIDs), datasetIDs)
	}
	if len(datasetIDs) == 1 && datasetIDs[0] == 0 {
		return fmt.Errorf("%w: 知识库 ID 不能为 0", ErrMissingDataset)
	}
	if required && len(datasetIDs) == 0 {
		return ErrMissingDataset
	}
	return nil
}
