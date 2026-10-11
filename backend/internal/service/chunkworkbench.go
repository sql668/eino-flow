package service

import (
	"context"
	"fmt"

	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/cleaner"
	"einoflow-backend/internal/model"
)

// 分段工作台：预览、入库、重切，以及知识库级默认配置的读写。

// ChunkOptions 是工作台需要的「可选项」：界面靠它渲染下拉与勾选框，
// 不再把选项写死在前端（前端写死的话，后端加规则就得改两处）。
type ChunkOptions struct {
	// Modes 是可选的分段模式。
	Modes []ChunkModeOption
	// CleaningRules 是可选的清洗规则（含说明与风险等级）。
	CleaningRules []cleaner.RuleInfo
	// Defaults 是当前知识库的默认配置（新建文档时预填）。
	Defaults model.ChunkConfig
	// Limits 是取值边界，前端据此限制输入范围并与后端校验保持一致。
	Limits ChunkLimits
}

// ChunkModeOption 是一个可选的分段模式。
type ChunkModeOption struct {
	Mode        model.ChunkMode
	Label       string
	Description string
	// Implemented 为 false 表示尚未实现（界面展示但禁用，避免使用者配了半天才发现不支持）。
	Implemented bool
}

// ChunkLimits 是配置的取值边界。
type ChunkLimits struct {
	MinMaxRunes     int
	MaxMaxRunes     int
	MaxMinRunes     int
	DefaultMaxRunes int
	DefaultMinRunes int
	// MaxOverlapPercent 是重叠比例上限（相对分段长度）。
	MaxOverlapPercent int
}

// CleaningRuleOption 是清洗规则的对外结构（复用 cleaner 的定义）。
type CleaningRuleOption = cleaner.RuleInfo

// ChunkOptionsFor 返回工作台的可选项与知识库的默认配置。
func (s *DocumentService) ChunkOptionsFor(ctx context.Context, datasetID uint64) (ChunkOptions, error) {
	if datasetID == 0 {
		return ChunkOptions{}, fmt.Errorf("%w: datasetId 不能为空", ErrInvalidInput)
	}
	dataset, err := s.datasets.GetByID(ctx, datasetID)
	if err != nil {
		return ChunkOptions{}, translateError(err)
	}

	return ChunkOptions{
		Modes: []ChunkModeOption{
			{
				Mode:        model.ChunkModeTitle,
				Label:       model.ChunkModeTitle.Label(),
				Description: "按标题层级切分，标题开新片段并带上章节路径。适合手册、论文这类有结构的文档。",
				Implemented: true,
			},
			{
				Mode:        model.ChunkModeLength,
				Label:       model.ChunkModeLength.Label(),
				Description: "忽略标题，只按长度切。适合聊天导出、日志这类没有可靠层级的内容。",
				Implemented: true,
			},
			{
				Mode:        model.ChunkModeSeparator,
				Label:       model.ChunkModeSeparator.Label(),
				Description: "先按你指定的分隔符拆开，再按长度切。适合「一段一条记录」的文本。",
				Implemented: true,
			},
			{
				Mode:        model.ChunkModeParentChild,
				Label:       model.ChunkModeParentChild.Label(),
				Description: "子块用于检索、父块作为上下文。功能尚未完成，敬请期待。",
				Implemented: false,
			},
		},
		CleaningRules: cleaner.Available(),
		Defaults:      chunkConfigOrDefault(dataset.ChunkConfig),
		Limits: ChunkLimits{
			MinMaxRunes:       model.MinChunkMaxRunes,
			MaxMaxRunes:       model.MaxChunkMaxRunes,
			MaxMinRunes:       model.MaxChunkMinRunes,
			DefaultMaxRunes:   model.DefaultChunkMaxRunes,
			DefaultMinRunes:   model.DefaultChunkMinRunes,
			MaxOverlapPercent: 50,
		},
	}, nil
}

// PreviewChunks 按给定配置预览分段结果（不写库）。
func (s *DocumentService) PreviewChunks(
	ctx context.Context, documentID uint64, config model.ChunkConfig,
) (rag.PreviewResult, error) {
	if documentID == 0 {
		return rag.PreviewResult{}, fmt.Errorf("%w: documentId 不能为空", ErrInvalidInput)
	}
	if err := validateChunkConfig(config); err != nil {
		return rag.PreviewResult{}, err
	}

	doc, err := s.documents.GetByID(ctx, documentID)
	if err != nil {
		return rag.PreviewResult{}, translateError(err)
	}
	// 只有「已解析」的文档能预览：解析产物不在就没法算分段，明确提示而不是给空结果。
	if doc.Status != model.DocumentStatusDraft && doc.Status != model.DocumentStatusReady {
		return rag.PreviewResult{}, fmt.Errorf("%w: 文档尚未解析完成（当前状态 %s），请稍候重试",
			ErrInvalidInput, doc.Status)
	}

	result, err := s.pipeline.Preview(ctx, documentID, config)
	if err != nil {
		return rag.PreviewResult{}, translateError(err)
	}
	return result, nil
}

// IngestDocument 按给定配置把文档切分入库。
//
// saveAsDefault 为 true 时同时把配置写回知识库默认，下次上传直接复用——
// 让人不必每次上传都重新调一遍参数。
func (s *DocumentService) IngestDocument(
	ctx context.Context, documentID uint64, config model.ChunkConfig, saveAsDefault bool,
) error {
	if documentID == 0 {
		return fmt.Errorf("%w: documentId 不能为空", ErrInvalidInput)
	}
	if err := validateChunkConfig(config); err != nil {
		return err
	}

	doc, err := s.documents.GetByID(ctx, documentID)
	if err != nil {
		return translateError(err)
	}

	normalized := config.Normalize()
	if saveAsDefault {
		dataset, err := s.datasets.GetByID(ctx, doc.DatasetID)
		if err != nil {
			return translateError(err)
		}
		dataset.ChunkConfig = &normalized
		if err := s.datasets.Update(ctx, dataset); err != nil {
			return translateError(err)
		}
	}

	// 入库是异步的（要调 embedding，几百段可能要几十秒），先返回再处理。
	s.enqueue(ingestJob{documentID: doc.ID, kind: jobIngest, config: normalized})
	return nil
}

// RechunkDocument 用知识库当前默认配置重新切分并入库。
//
// 场景：改了默认配置后，希望老文档也按新配置重建索引。
func (s *DocumentService) RechunkDocument(ctx context.Context, documentID uint64) error {
	if documentID == 0 {
		return fmt.Errorf("%w: documentId 不能为空", ErrInvalidInput)
	}
	doc, err := s.documents.GetByID(ctx, documentID)
	if err != nil {
		return translateError(err)
	}
	dataset, err := s.datasets.GetByID(ctx, doc.DatasetID)
	if err != nil {
		return translateError(err)
	}

	s.enqueue(ingestJob{
		documentID: doc.ID,
		kind:       jobIngest,
		config:     chunkConfigOrDefault(dataset.ChunkConfig),
	})
	return nil
}

// ReparseDocument 重新解析文档。
//
// 解析同样是异步的：MinerU 解析一份 PDF 要几十秒到几分钟，不能挂在 HTTP 请求上。
func (s *DocumentService) ReparseDocument(ctx context.Context, documentID uint64) error {
	if documentID == 0 {
		return fmt.Errorf("%w: documentId 不能为空", ErrInvalidInput)
	}
	if _, err := s.documents.GetByID(ctx, documentID); err != nil {
		return translateError(err)
	}
	s.enqueue(ingestJob{documentID: documentID, kind: jobParse})
	return nil
}

// GetChunkConfig 返回知识库的分段配置。
func (s *DocumentService) GetChunkConfig(ctx context.Context, datasetID uint64) (model.ChunkConfig, error) {
	dataset, err := s.datasets.GetByID(ctx, datasetID)
	if err != nil {
		return model.ChunkConfig{}, translateError(err)
	}
	return chunkConfigOrDefault(dataset.ChunkConfig), nil
}

// SaveChunkConfig 保存知识库的分段配置（作为后续上传的默认值）。
func (s *DocumentService) SaveChunkConfig(
	ctx context.Context, datasetID uint64, config model.ChunkConfig,
) (model.ChunkConfig, error) {
	if err := validateChunkConfig(config); err != nil {
		return model.ChunkConfig{}, err
	}
	dataset, err := s.datasets.GetByID(ctx, datasetID)
	if err != nil {
		return model.ChunkConfig{}, translateError(err)
	}

	normalized := config.Normalize()
	dataset.ChunkConfig = &normalized
	if err := s.datasets.Update(ctx, dataset); err != nil {
		return model.ChunkConfig{}, translateError(err)
	}
	return normalized, nil
}

// GetRetrievalConfig 返回知识库的检索配置。
func (s *DocumentService) GetRetrievalConfig(
	ctx context.Context, datasetID uint64,
) (model.RetrievalConfig, error) {
	dataset, err := s.datasets.GetByID(ctx, datasetID)
	if err != nil {
		return model.RetrievalConfig{}, translateError(err)
	}
	return retrievalConfigOrDefault(dataset.RetrievalConfig, s.retrievalDefaults), nil
}

// SaveRetrievalConfig 保存知识库的检索配置。
func (s *DocumentService) SaveRetrievalConfig(
	ctx context.Context, datasetID uint64, config model.RetrievalConfig,
) (model.RetrievalConfig, error) {
	dataset, err := s.datasets.GetByID(ctx, datasetID)
	if err != nil {
		return model.RetrievalConfig{}, translateError(err)
	}

	normalized := config.Normalize(s.retrievalDefaults.TopK, s.retrievalDefaults.CandidateTopK)
	dataset.RetrievalConfig = &normalized
	if err := s.datasets.Update(ctx, dataset); err != nil {
		return model.RetrievalConfig{}, translateError(err)
	}
	return normalized, nil
}

// validateChunkConfig 校验分段配置。
//
// 只挡「明显不可能成立」的组合；越界的数值交给 Normalize 收敛，
// 这样界面滑块拖到头也不会报错，而真正错误的组合（例如未实现的模式）会明确失败。
func validateChunkConfig(config model.ChunkConfig) error {
	if config.Mode != "" && !config.Mode.IsValid() {
		return fmt.Errorf("%w: 不支持的分段模式 %q（可选：title / length / separator / parent_child）",
			ErrInvalidInput, config.Mode)
	}
	if config.Mode == model.ChunkModeParentChild {
		// 明确拒绝而不是悄悄退化成默认模式：使用者选了父子分块却拿到普通分段，
		// 会以为功能生效了。
		return fmt.Errorf("%w: 父子分段尚未实现，请先选择其它模式", ErrInvalidInput)
	}
	if config.Mode == model.ChunkModeSeparator && config.Separator == "" {
		return fmt.Errorf("%w: 选择按分隔符分段时必须填写分隔符", ErrInvalidInput)
	}
	if config.MaxRunes < 0 || config.MaxRunes > model.MaxChunkMaxRunes {
		return fmt.Errorf("%w: 分段长度上限需在 %d~%d 之间",
			ErrInvalidInput, model.MinChunkMaxRunes, model.MaxChunkMaxRunes)
	}
	if config.OverlapRunes < 0 {
		return fmt.Errorf("%w: 重叠长度不能为负数", ErrInvalidInput)
	}
	if config.MaxRunes > 0 && config.OverlapRunes > config.MaxRunes/2 {
		return fmt.Errorf("%w: 重叠长度不能超过分段长度上限的一半（%d）",
			ErrInvalidInput, config.MaxRunes/2)
	}
	return nil
}

// chunkConfigOrDefault 把数据库里的配置（可能为空）补齐成可用配置。
func chunkConfigOrDefault(config *model.ChunkConfig) model.ChunkConfig {
	if config == nil || config.Mode == "" {
		return model.DefaultChunkConfig()
	}
	normalized := config.Normalize()
	// MaxRunes 为 0 说明这条记录来自「还没配过分段」的老数据，用默认值补齐。
	if config.MaxRunes == 0 {
		defaults := model.DefaultChunkConfig()
		normalized.MaxRunes = defaults.MaxRunes
		if normalized.OverlapRunes == 0 {
			normalized.OverlapRunes = defaults.OverlapRunes
		}
		normalized.MinRunes = defaults.MinRunes
	}
	return normalized
}

// retrievalConfigOrDefault 把数据库里的检索配置补齐成可用配置。
//
// 「全零」视为没配过：列的默认值是 '{}'，反序列化出来是一个所有字段为零值的结构体。
// 如果直接 Normalize，Rerank 这个 bool 的零值 false 会被当成「使用者显式关闭了重排」，
// 结果是所有知识库静默不重排——实测踩到过，排查起来很绕。
func retrievalConfigOrDefault(config *model.RetrievalConfig, defaults RetrievalDefaults) model.RetrievalConfig {
	if config == nil || isZeroRetrievalConfig(*config) {
		return model.RetrievalConfig{
			TopK:          defaults.TopK,
			CandidateTopK: defaults.CandidateTopK,
			Rerank:        true,
			VectorWeight:  0.7,
			KeywordWeight: 0.3,
		}
	}
	return config.Normalize(defaults.TopK, defaults.CandidateTopK)
}

// isZeroRetrievalConfig 判断配置是否为空（从没保存过）。
func isZeroRetrievalConfig(config model.RetrievalConfig) bool {
	return config.TopK == 0 && config.CandidateTopK == 0 && config.ScoreThreshold == 0 &&
		!config.Rerank && config.VectorWeight == 0 && config.KeywordWeight == 0
}

// 说明：chunker 的 Stats 类型在预览结果里直接透出给前端，
// 这里显式引用一次以表明它属于对外契约的一部分（字段变更要同步前端类型）。
var _ = chunker.Stats{}
