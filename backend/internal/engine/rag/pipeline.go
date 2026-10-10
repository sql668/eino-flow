// Package rag 编排文档入库与检索。
//
// 入库流水线：取回原文件 → 解析 → 切分 → 写分段 → 向量化 → 写向量库。
// 检索链路见 retriever.go。
package rag

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"

	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/loader"
	// 别名：本文件同时用到领域模型（internal/model），两个包同名必须区分开。
	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// errorMessageLimit 与 document.error_message 列长度对齐（VARCHAR(512)），
// 按字符截断避免超长错误信息写库失败。
const errorMessageLimit = 500

// Pipeline 编排文档入库。
type Pipeline struct {
	storage   repository.ObjectStore
	documents repository.DocumentRepository
	segments  repository.SegmentRepository
	tx        repository.TxManager
	resolver  *loader.Resolver
	chunkOpts chunker.Options
	// models / vectors 是向量化与向量库。
	// 前者用接口而不是具体实现：阶段 2 换成「数据库驱动的模型配置」时这里不用改。
	models  enginemodel.Resolver
	vectors repository.VectorStore
}

// Deps 是流水线依赖。
type Deps struct {
	Storage   repository.ObjectStore
	Documents repository.DocumentRepository
	Segments  repository.SegmentRepository
	Tx        repository.TxManager
	Resolver  *loader.Resolver
	ChunkOpts chunker.Options
	Models    enginemodel.Resolver
	Vectors   repository.VectorStore
}

// NewPipeline 构造流水线。
func NewPipeline(deps Deps) *Pipeline {
	return &Pipeline{
		storage:   deps.Storage,
		documents: deps.Documents,
		segments:  deps.Segments,
		tx:        deps.Tx,
		resolver:  deps.Resolver,
		chunkOpts: deps.ChunkOpts,
		models:    deps.Models,
		vectors:   deps.Vectors,
	}
}

// Ingest 执行一次完整入库，并把结果写入文档状态。
//
// 无论成功还是失败都会更新状态：失败也必须落库，否则前端会永远看到「处理中」。
func (p *Pipeline) Ingest(ctx context.Context, documentID uint64) error {
	doc, err := p.documents.GetByID(ctx, documentID)
	if err != nil {
		return fmt.Errorf("读取文档记录失败: %w", err)
	}

	if err := p.documents.UpdateIndexState(ctx, doc.ID, model.DocumentStatusIndexing, 0, ""); err != nil {
		return err
	}

	if err := p.run(ctx, doc); err != nil {
		// 落库失败本身也要报出来，但不要把原始错误吞掉，因此两个错误一起返回。
		markErr := p.documents.UpdateIndexState(
			ctx, doc.ID, model.DocumentStatusFailed, 0, truncateRunes(err.Error(), errorMessageLimit))
		if markErr != nil {
			return errors.Join(err, fmt.Errorf("写入失败状态也失败: %w", markErr))
		}
		return err
	}
	return nil
}

// run 执行实际的处理步骤。
func (p *Pipeline) run(ctx context.Context, doc *model.Document) error {
	content, err := p.storage.Get(ctx, doc.Source)
	if err != nil {
		return fmt.Errorf("取回原始文件失败: %w", err)
	}

	item, err := p.resolver.Resolve(doc.Name)
	if err != nil {
		return err
	}

	parsed, err := item.Load(ctx, loader.LoadRequest{
		Filename: doc.Name,
		Content:  content,
		MIMEType: mimeByFilename(doc.Name),
	})
	if err != nil {
		return fmt.Errorf("解析文档失败: %w", err)
	}

	chunks := chunker.Split(parsed, p.chunkOpts)
	if len(chunks) == 0 {
		return fmt.Errorf("切分结果为空，文档可能没有可提取的文本内容")
	}

	segments := make([]*model.Segment, 0, len(chunks))
	for _, item := range chunks {
		segments = append(segments, &model.Segment{
			DatasetID:  doc.DatasetID,
			DocumentID: doc.ID,
			ChunkIndex: item.Index,
			Content:    item.Content,
			TokenCount: item.TokenCount,
			Page:       item.Page,
			Locator:    item.Locator,
			Kind:       item.Kind,
			TitlePath:  nonNilStrings(item.TitlePath),
			// VectorID 留空，等向量阶段回填。
		})
	}

	// 清旧分段与写新分段必须原子完成：中途失败会留下半套分段，
	// 检索结果就会指向已不存在的内容。事务边界在流水线这里划定。
	err = p.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := p.segments.DeleteByDocument(txCtx, doc.ID); err != nil {
			return err
		}
		return p.segments.CreateBatch(txCtx, segments)
	})
	if err != nil {
		return fmt.Errorf("写入分段失败: %w", err)
	}

	// 分段主键由数据库生成，向量主键与它一致，因此要等写入完成后再回填。
	if _, err := p.segments.AttachVectorIDs(ctx, doc.ID); err != nil {
		return err
	}

	if err := p.vectorize(ctx, doc, segments); err != nil {
		return err
	}

	if err := p.documents.UpdateIndexState(ctx, doc.ID, model.DocumentStatusReady, len(segments), ""); err != nil {
		return err
	}
	return nil
}

// vectorize 把分段向量化并写入向量库。
//
// 顺序很关键，且不能靠事务保证（PostgreSQL 与 Milvus 是两个系统，没有跨库事务）：
//  1. 先按文档删除旧向量，再写入新向量。反过来会把刚写入的向量连带删掉——
//     两步的条件都是 document_id，而旧向量与已删除的旧分段主键绑定，只写不删会留下悬空向量；
//  2. 失败时文档会被标记为 failed。检索只认「已就绪」的文档，
//     因此中途失败留下的半套数据不会污染检索结果。
func (p *Pipeline) vectorize(ctx context.Context, doc *model.Document, segments []*model.Segment) error {
	if len(segments) == 0 {
		return nil
	}

	embedder, err := p.models.Embedder(ctx)
	if err != nil {
		return fmt.Errorf("获取向量化模型失败: %w", err)
	}
	if err := p.vectors.EnsureCollection(ctx, embedder.Dimension()); err != nil {
		return err
	}

	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.Content)
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("向量化分段失败: %w", err)
	}
	if len(vectors) != len(segments) {
		return fmt.Errorf("向量数量（%d）与分段数量（%d）不一致", len(vectors), len(segments))
	}

	records := make([]repository.VectorRecord, 0, len(segments))
	for i, segment := range segments {
		records = append(records, repository.VectorRecord{
			SegmentID:  segment.ID,
			DatasetID:  doc.DatasetID,
			DocumentID: doc.ID,
			Vector:     vectors[i],
		})
	}

	if _, err := p.vectors.DeleteByDocument(ctx, doc.ID); err != nil {
		return err
	}
	return p.vectors.Upsert(ctx, records)
}

// mimeByFilename 按扩展名推断 MIME 类型，供解析器判断文件类型使用。
func mimeByFilename(filename string) string {
	dot := strings.LastIndex(filename, ".")
	if dot < 0 {
		return "application/octet-stream"
	}
	if inferred := mime.TypeByExtension(filename[dot:]); inferred != "" {
		return inferred
	}
	return "application/octet-stream"
}

// truncateRunes 按字符数截断，避免中文被按字节切断产生半个字。
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// nonNilStrings 把 nil 切片规范成空切片。
//
// 必须做这层规范化：title_path 是 NOT NULL 的 jsonb 列，而 json.Marshal 对 nil 切片
// 输出的是 null（不是 []），直接写入会违反非空约束。
// 语义上「没有标题路径」本来就该是空数组，而不是 null。
func nonNilStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
