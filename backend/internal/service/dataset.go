package service

import (
	"context"
	"fmt"
	"strings"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

const maxDatasetNameRunes = 128

// CreateDatasetInput 是创建知识库的入参。
type CreateDatasetInput struct {
	Name           string
	Description    string
	EmbeddingModel string
}

// UpdateDatasetInput 是更新知识库的入参；指针字段用于区分「未传」与「传空」。
type UpdateDatasetInput struct {
	Name           *string
	Description    *string
	EmbeddingModel *string
	Status         *model.DatasetStatus
}

// ListDatasetsInput 是知识库列表查询入参。
type ListDatasetsInput struct {
	Keyword  string
	Page     int
	PageSize int
}

// DatasetListResult 是知识库列表查询结果。
type DatasetListResult struct {
	Items    []*model.Dataset
	Total    int64
	Page     int
	PageSize int
}

// DatasetService 承载知识库管理的业务规则。
type DatasetService struct {
	datasets repository.DatasetRepository
}

// NewDatasetService 构造知识库服务。
func NewDatasetService(datasets repository.DatasetRepository) *DatasetService {
	return &DatasetService{datasets: datasets}
}

// Create 创建知识库。
func (s *DatasetService) Create(ctx context.Context, in CreateDatasetInput) (*model.Dataset, error) {
	name, err := validateRequiredName(in.Name, "知识库名称", maxDatasetNameRunes)
	if err != nil {
		return nil, err
	}

	dataset := &model.Dataset{
		Name:           name,
		Description:    strings.TrimSpace(in.Description),
		EmbeddingModel: strings.TrimSpace(in.EmbeddingModel),
		Status:         model.DatasetStatusActive,
	}
	if err := s.datasets.Create(ctx, dataset); err != nil {
		return nil, err
	}
	return dataset, nil
}

// Get 查询单个知识库。
func (s *DatasetService) Get(ctx context.Context, id uint64) (*model.Dataset, error) {
	dataset, err := s.datasets.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}
	return dataset, nil
}

// List 分页查询知识库。
func (s *DatasetService) List(ctx context.Context, in ListDatasetsInput) (*DatasetListResult, error) {
	page, pageSize := normalizePaging(in.Page, in.PageSize)

	datasets, total, err := s.datasets.List(ctx, repository.DatasetListFilter{
		Keyword: strings.TrimSpace(in.Keyword),
		Offset:  (page - 1) * pageSize,
		Limit:   pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &DatasetListResult{Items: datasets, Total: total, Page: page, PageSize: pageSize}, nil
}

// Update 更新知识库；未传字段保持原值。
func (s *DatasetService) Update(ctx context.Context, id uint64, in UpdateDatasetInput) (*model.Dataset, error) {
	dataset, err := s.datasets.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}

	if in.Name != nil {
		name, err := validateRequiredName(*in.Name, "知识库名称", maxDatasetNameRunes)
		if err != nil {
			return nil, err
		}
		dataset.Name = name
	}
	if in.Description != nil {
		dataset.Description = strings.TrimSpace(*in.Description)
	}
	if in.EmbeddingModel != nil {
		dataset.EmbeddingModel = strings.TrimSpace(*in.EmbeddingModel)
	}
	if in.Status != nil {
		if !in.Status.IsValid() {
			return nil, fmt.Errorf("%w: 不支持的状态 %q", ErrInvalidInput, *in.Status)
		}
		dataset.Status = *in.Status
	}

	if err := s.datasets.Update(ctx, dataset); err != nil {
		return nil, translateError(err)
	}
	return dataset, nil
}

// Delete 软删除知识库。
func (s *DatasetService) Delete(ctx context.Context, id uint64) error {
	return translateError(s.datasets.SoftDelete(ctx, id))
}
