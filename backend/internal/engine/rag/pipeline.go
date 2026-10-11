// Package rag 编排文档入库与检索。
//
// 现在是**两段式**流程（对应分段工作台）：
//
//	解析：取回原文件 → 解析 → 解析产物落对象存储 → 文档进入「待确认」
//	入库：读解析产物 → 清洗 → 切分 → 写分段 → 向量化 → 写向量库 → 文档就绪
//
// 为什么拆成两段：分段与清洗参数需要反复试。解析一次（PDF 几十秒到几分钟）之后，
// 用落库的解析产物就能反复预览与重切，调参才有意义。
// 检索链路见 retriever.go。
package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"strings"

	"github.com/google/uuid"

	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/cleaner"
	"einoflow-backend/internal/engine/rag/keyword"
	"einoflow-backend/internal/engine/rag/loader"
	// 别名：本文件同时用到领域模型（internal/model），两个包同名必须区分开。
	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

const (
	// errorMessageLimit 与 document.error_message 列长度对齐（VARCHAR(512)），
	// 按字符截断避免超长错误信息写库失败。
	errorMessageLimit = 500
	// previewChunkLimit 是预览返回的片段上限。
	//
	// 界面一次展示不了几百条，而把上千条片段塞进一份 JSON 响应既慢又占内存；
	// 总条数放在统计里返回，界面显示「共 N 段，预览前 M 段」。
	previewChunkLimit = 50
	// vectorBatchSize 是单次向量化与写入向量库的片段数上限。
	//
	// 一次几百段是实测量级：再大请求体与显存/内存占用都会明显上升，
	// 而一次失败要重来的代价也成倍增长（几百页的 PDF 会产生上千段）。
	vectorBatchSize = 500
)

// previewErrorLimit 与 error_message 列长度一致，截断后再落库。
var errDocumentNotFound = repository.ErrNotFound

// Pipeline 编排文档解析与入库。
type Pipeline struct {
	storage   repository.ObjectStore
	documents repository.DocumentRepository
	segments  repository.SegmentRepository
	tx        repository.TxManager
	resolver  *loader.Resolver
	// models / vectors 是向量化与向量库。
	// 前者用接口而不是具体实现：模型来源从配置文件换成数据库时这里不用改。
	models  enginemodel.Resolver
	vectors repository.VectorStore
	// keywords 抽取关键词；为空表示不做抽取（分段关键词留空数组）。
	keywords keyword.Extractor
}

// Deps 是流水线依赖。
type Deps struct {
	Storage   repository.ObjectStore
	Documents repository.DocumentRepository
	Segments  repository.SegmentRepository
	Tx        repository.TxManager
	Resolver  *loader.Resolver
	Models    enginemodel.Resolver
	Vectors   repository.VectorStore
	// Keywords 是关键词抽取器，一般用 keyword.NewTFIDF(向量库) 构造：
	// 分词与 BM25 索引共用同一个分析器，避免「关键词里有的词检索却搜不到」。
	Keywords keyword.Extractor
}

// NewPipeline 构造流水线。
func NewPipeline(deps Deps) *Pipeline {
	return &Pipeline{
		storage:   deps.Storage,
		documents: deps.Documents,
		segments:  deps.Segments,
		tx:        deps.Tx,
		resolver:  deps.Resolver,
		models:    deps.Models,
		vectors:   deps.Vectors,
		keywords:  deps.Keywords,
	}
}

// PreviewResult 是一次分段预览的结果（不落库）。
type PreviewResult struct {
	// Chunks 是预览片段，最多 previewChunkLimit 条。
	Chunks []PreviewChunk
	// Total 是切分出的片段总数（可能大于 Chunks 的长度）。
	Total int
	// Cleaning 是清洗报告：每条规则删了多少、删的是什么样本。
	Cleaning cleaner.Report
	// Stats 是切片统计，用于健康度提示。
	Stats chunker.Stats
	// Hints 是给使用者的可执行建议。
	Hints []string
	// ConfigVersion 是配置指纹，入库时写进分段，便于分辨「这批片段是哪套参数切的」。
	ConfigVersion string
	// Meta 是解析元信息（页数、解析器等），预览页顶部展示。
	Meta loader.DocumentMeta
}

// PreviewChunk 是预览里的一个片段。
type PreviewChunk struct {
	Index        int
	Kind         string
	TitlePath    []string
	Page         int
	Locator      string
	CharCount    int
	OverlapRunes int
	// Content 是最终用于入库与检索的内容（含标题面包屑）。
	Content string
	// OriginalText 是清洗前的同一片段内容（不含面包屑），供界面做「清洗前/后」对比。
	OriginalText string
}

// Parse 解析文档并把解析产物存入对象存储，文档进入「待确认」。
//
// 无论成功失败都会更新状态：失败必须落库，否则前端会永远看到「解析中」。
func (p *Pipeline) Parse(ctx context.Context, documentID uint64) error {
	doc, err := p.documents.GetByID(ctx, documentID)
	if err != nil {
		return fmt.Errorf("读取文档记录失败: %w", err)
	}

	if err := p.documents.UpdateParseState(
		ctx, doc.ID, model.DocumentStatusParsing, doc.ParsedKey, doc.CharCount, ""); err != nil {
		return err
	}

	parsed, err := p.parse(ctx, doc)
	if err != nil {
		markErr := p.documents.UpdateParseState(
			ctx, doc.ID, model.DocumentStatusFailed, "", 0, truncateRunes(err.Error(), errorMessageLimit))
		if markErr != nil {
			return errors.Join(err, fmt.Errorf("写入失败状态也失败: %w", markErr))
		}
		return err
	}

	// 解析产物落对象存储：分段工作台靠它反复预览，不必重复解析。
	key := parsedObjectKey(doc.ID)
	payload, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("序列化解析产物失败: %w", err)
	}
	if err := p.storage.Put(ctx, key, payload, "application/json"); err != nil {
		markErr := p.documents.UpdateParseState(
			ctx, doc.ID, model.DocumentStatusFailed, "", 0, truncateRunes("保存解析产物失败: "+err.Error(), errorMessageLimit))
		if markErr != nil {
			return errors.Join(err, markErr)
		}
		return fmt.Errorf("保存解析产物失败: %w", err)
	}

	// 进入待确认：此时还没有任何向量，必须由使用者确认分段配置后才入库。
	if err := p.documents.UpdateParseState(
		ctx, doc.ID, model.DocumentStatusDraft, key, len([]rune(parsed.Text())), ""); err != nil {
		return err
	}
	return nil
}

// Preview 按给定配置预览分段结果，不写任何数据。
func (p *Pipeline) Preview(
	ctx context.Context, documentID uint64, cfg model.ChunkConfig,
) (PreviewResult, error) {
	config := cfg.Normalize()
	parsed, err := p.loadParsed(ctx, documentID)
	if err != nil {
		return PreviewResult{}, err
	}

	cleaned, cleaningReport, err := cleaner.Clean(parsed, config.Cleaning)
	if err != nil {
		return PreviewResult{}, err
	}

	chunkResult, err := chunker.Split(cleaned, toChunkOptions(config))
	if err != nil {
		return PreviewResult{}, err
	}

	// 同时给出清洗前的版本，供界面做「清洗前 / 清洗后」对比：
	// 只看清洗后的结果无法判断规则是否误删，对比才有判断依据。
	rawChunks, err := chunker.Split(parsed, toChunkOptions(config))
	if err != nil {
		return PreviewResult{}, err
	}

	preview := PreviewResult{
		Chunks:        make([]PreviewChunk, 0, min(len(chunkResult.Chunks), previewChunkLimit)),
		Total:         len(chunkResult.Chunks),
		Cleaning:      cleaningReport,
		Stats:         chunkResult.Stats,
		Hints:         chunkResult.Hints,
		ConfigVersion: config.Version(),
		Meta:          cleaned.Meta,
	}
	for i, item := range chunkResult.Chunks {
		if i >= previewChunkLimit {
			break
		}
		original := ""
		if i < len(rawChunks.Chunks) {
			original = rawChunks.Chunks[i].Text
		}
		preview.Chunks = append(preview.Chunks, PreviewChunk{
			Index:        item.Index,
			Kind:         item.Kind,
			TitlePath:    item.TitlePath,
			Page:         item.Page,
			Locator:      item.Locator,
			CharCount:    item.CharCount,
			OverlapRunes: item.OverlapRunes,
			Content:      item.Content,
			OriginalText: original,
		})
	}
	return preview, nil
}

// Ingest 按给定配置把文档切分入库（重新入库会覆盖旧分段与旧向量）。
func (p *Pipeline) Ingest(ctx context.Context, documentID uint64, cfg model.ChunkConfig) error {
	doc, err := p.documents.GetByID(ctx, documentID)
	if err != nil {
		return fmt.Errorf("读取文档记录失败: %w", err)
	}

	config := cfg.Normalize()
	if err := p.documents.UpdateIndexState(
		ctx, doc.ID, model.DocumentStatusIndexing, 0, string(config.Mode), ""); err != nil {
		return err
	}

	if err := p.run(ctx, doc, config); err != nil {
		markErr := p.documents.UpdateIndexState(
			ctx, doc.ID, model.DocumentStatusFailed, 0, string(config.Mode),
			truncateRunes(err.Error(), errorMessageLimit))
		if markErr != nil {
			return errors.Join(err, fmt.Errorf("写入失败状态也失败: %w", markErr))
		}
		return err
	}
	return nil
}

// run 执行实际的处理步骤。
func (p *Pipeline) run(ctx context.Context, doc *model.Document, config model.ChunkConfig) error {
	parsed, err := p.loadParsed(ctx, doc.ID)
	if err != nil {
		return err
	}

	cleaned, _, err := cleaner.Clean(parsed, config.Cleaning)
	if err != nil {
		return err
	}

	chunkResult, err := chunker.Split(cleaned, toChunkOptions(config))
	if err != nil {
		return err
	}
	if len(chunkResult.Chunks) == 0 {
		return fmt.Errorf("切分结果为空：内容可能被清洗规则或噪声过滤全部删掉了，请调整配置")
	}

	version := config.Version()
	// 关键词按「整篇文档一次抽」：TF-IDF 的逆文档频率要在本文档的所有片段上统计，
	// 一段一段抽会退化成「词频排序」，手册里满篇出现的词反而会被选成关键词。
	keywords := p.extractKeywords(ctx, chunkResult.Chunks, config)
	segments := make([]*model.Segment, 0, len(chunkResult.Chunks))
	for i, item := range chunkResult.Chunks {
		segments = append(segments, &model.Segment{
			// chunk_id 由业务生成：它同时是向量库主键，检索拿它回查本表。
			ChunkID:            uuid.NewString(),
			DatasetID:          doc.DatasetID,
			DocumentID:         doc.ID,
			ChunkIndex:         item.Index,
			Content:            item.Content,
			CharCount:          item.CharCount,
			TokenCount:         item.TokenCount,
			OverlapRunes:       item.OverlapRunes,
			ContentHash:        contentHash(item.Content),
			ChunkConfigVersion: version,
			UserID:             doc.UserID,
			Page:               item.Page,
			Locator:            item.Locator,
			Kind:               item.Kind,
			TitlePath:          nonNilStrings(item.TitlePath),
			Keywords:           nonNilStrings(keywords[i]),
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

	if err := p.vectorize(ctx, doc, segments); err != nil {
		return err
	}

	if err := p.documents.UpdateIndexState(
		ctx, doc.ID, model.DocumentStatusReady, len(segments), string(config.Mode), ""); err != nil {
		return err
	}
	return nil
}

// loadParsed 读取文档的解析产物。
//
// 解析产物缺失时明确报错并提示重新解析，而不是静默重新解析：
// 重新解析要几十秒到几分钟，静默做会让使用者以为界面卡住了。
func (p *Pipeline) loadParsed(ctx context.Context, documentID uint64) (*loader.ParsedDocument, error) {
	doc, err := p.documents.GetByID(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("读取文档记录失败: %w", err)
	}
	if doc.ParsedKey == "" {
		return nil, fmt.Errorf("文档尚未解析（状态 %s），请先完成解析", doc.Status)
	}

	raw, err := p.storage.Get(ctx, doc.ParsedKey)
	if err != nil {
		return nil, fmt.Errorf("读取解析产物失败: %w", err)
	}
	var parsed loader.ParsedDocument
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析产物格式不正确（可能来自旧版本）: %w", err)
	}
	if parsed.Meta.SourceName == "" {
		parsed.Meta.SourceName = doc.Name
	}
	return &parsed, nil
}

// parse 取回原文件并解析。
func (p *Pipeline) parse(ctx context.Context, doc *model.Document) (*loader.ParsedDocument, error) {
	content, err := p.storage.Get(ctx, doc.Source)
	if err != nil {
		return nil, fmt.Errorf("取回原始文件失败: %w", err)
	}

	item, err := p.resolver.Resolve(doc.Name)
	if err != nil {
		return nil, err
	}

	parsed, err := item.Load(ctx, loader.LoadRequest{
		Filename: doc.Name,
		Content:  content,
		MIMEType: mimeByFilename(doc.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("解析文档失败: %w", err)
	}
	return parsed, nil
}

// vectorize 把分段向量化并写入向量库。
//
// 顺序很关键，且不能靠事务保证（PostgreSQL 与 Milvus 是两个系统，没有跨库事务）：
//  1. 先按文档删除旧向量，再写入新向量。反过来会把刚写入的向量连带删掉；
//  2. 失败时文档会被标记为 failed。检索只认「已就绪」的文档，
//     因此中途失败留下的半套数据不会污染检索结果。
func (p *Pipeline) vectorize(ctx context.Context, doc *model.Document, segments []*model.Segment) error {
	if len(segments) == 0 {
		return nil
	}

	// 按知识库解析模型：知识库可以有自己的向量模型（同 Dify），
	// 未配置时 resolver 会回落到全局默认。
	embedder, err := p.models.Embedder(ctx, enginemodel.ScopeRef{DatasetID: doc.DatasetID})
	if err != nil {
		return fmt.Errorf("获取向量化模型失败: %w", err)
	}
	if err := p.vectors.EnsureCollection(ctx, embedder.Dimension()); err != nil {
		return err
	}

	// 先删旧向量再写新的：反过来会把刚写入的向量连带删掉（同文档重建索引的场景）。
	if _, err := p.vectors.DeleteByDocument(ctx, doc.ID); err != nil {
		return err
	}

	// 分批向量化 + 分批写入：一次几千段会把请求体、内存与超时风险都放大几十倍，
	// 而分批后进度可观察、失败可定位（错误信息里带上批次区间）。
	for start := 0; start < len(segments); start += vectorBatchSize {
		end := min(start+vectorBatchSize, len(segments))
		batch := segments[start:end]

		texts := make([]string, 0, len(batch))
		for _, segment := range batch {
			texts = append(texts, segment.Content)
		}
		vectors, err := embedder.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("向量化分段失败（第 %d~%d 段）: %w", start+1, end, err)
		}
		if len(vectors) != len(batch) {
			return fmt.Errorf("向量数量（%d）与分段数量（%d）不一致（第 %d~%d 段）",
				len(vectors), len(batch), start+1, end)
		}

		records := make([]repository.VectorRecord, 0, len(batch))
		for i, segment := range batch {
			records = append(records, repository.VectorRecord{
				ChunkID:    segment.ChunkID,
				DatasetID:  doc.DatasetID,
				DocumentID: doc.ID,
				UserID:     doc.UserID,
				// 章节路径在向量库里存拼接形式：检索结果直接可展示，也能用于过滤。
				TitlePath: strings.Join(segment.TitlePath, " / "),
				Page:      segment.Page,
				Kind:      segment.Kind,
				// 正文与关键词一起进 BM25 输入字段：关键词在尾部重复出现，
				// 词频略高即等于给关键词加权，这正是「改关键词能影响检索」的实现方式。
				Content:  segment.Content,
				Keywords: segment.Keywords,
				Vector:   vectors[i],
			})
		}
		if err := p.vectors.Upsert(ctx, records); err != nil {
			return fmt.Errorf("写入向量失败（第 %d~%d 段）: %w", start+1, end, err)
		}
	}
	return nil
}

// extractKeywords 为整篇文档的片段抽取关键词，返回与入参一一对应的结果。
//
// 抽取失败**不让入库失败**：关键词是辅助信息（展示 + 全文检索的一路权重），
// 它挂掉时用户拿到的仍是一份可检索的文档。但失败必须留下日志，
// 否则「关键词为什么是空的」会变成新的谜题。
func (p *Pipeline) extractKeywords(
	ctx context.Context, chunks []chunker.Chunk, config model.ChunkConfig,
) [][]string {
	empty := make([][]string, len(chunks))
	if p.keywords == nil {
		return empty
	}
	// 配置为 nil 视为默认开启：字段是后加的，老配置里没有它（见 model.KeywordConfig）。
	keywordConfig := model.KeywordConfigDefault()
	if config.Keywords != nil {
		keywordConfig = config.Keywords.Normalize()
	}
	if !keywordConfig.Enabled {
		return empty
	}

	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Content)
	}
	extracted, err := p.keywords.Extract(ctx, texts, keywordConfig.MaxCount)
	if err != nil {
		log.Printf("抽取分段关键词失败（%d 段），本次入库不生成关键词: %v", len(chunks), err)
		return empty
	}
	if len(extracted) != len(chunks) {
		// 条数不一致说明抽取器行为异常，宁可全部留空也不要错位配对：
		// 错位会把 A 段的关键词写到 B 段上，而且看起来完全正常。
		log.Printf("抽取分段关键词返回 %d 条，期望 %d 条，本次入库不生成关键词", len(extracted), len(chunks))
		return empty
	}
	for i := range extracted {
		if extracted[i] == nil {
			extracted[i] = []string{}
		}
	}
	return extracted
}

// UpdateSegmentKeywords 覆盖单个分段的关键词，并同步到向量库的 BM25 输入文本。
//
// 之所以还要动向量库：关键词是全文/混合检索的输入之一，只改业务库的话，
// 界面显示的关键词与实际参与检索的内容就不一致了——这种不一致最难排查。
// 同步方式是按新关键词重新向量化这一条（单条开销可忽略），再覆盖写入。
func (p *Pipeline) UpdateSegmentKeywords(
	ctx context.Context, datasetID uint64, segmentID uint64, keywords []string,
) (*model.Segment, error) {
	segment, err := p.segments.GetByID(ctx, datasetID, segmentID)
	if err != nil {
		return nil, err
	}
	cleaned := normalizeKeywords(keywords)
	if err := p.segments.UpdateKeywords(ctx, datasetID, segmentID, cleaned); err != nil {
		return nil, err
	}
	segment.Keywords = cleaned

	if err := p.syncSegmentVector(ctx, segment); err != nil {
		// 业务库已改、向量库没跟上，必须让调用方知道：否则使用者以为改生效了，
		// 而检索仍然按旧关键词走。返回错误让界面明确提示重试。
		return segment, fmt.Errorf("关键词已保存，但同步检索索引失败（请重试）: %w", err)
	}
	return segment, nil
}

// syncSegmentVector 用片段当前的内容与关键词覆盖写一条向量记录。
func (p *Pipeline) syncSegmentVector(ctx context.Context, segment *model.Segment) error {
	doc, err := p.documents.GetByID(ctx, segment.DocumentID)
	if err != nil {
		return fmt.Errorf("读取文档记录失败: %w", err)
	}
	embedder, err := p.models.Embedder(ctx, enginemodel.ScopeRef{DatasetID: segment.DatasetID})
	if err != nil {
		return fmt.Errorf("获取向量化模型失败: %w", err)
	}
	if err := p.vectors.EnsureCollection(ctx, embedder.Dimension()); err != nil {
		return err
	}
	vectors, err := embedder.Embed(ctx, []string{segment.Content})
	if err != nil {
		return fmt.Errorf("向量化分段失败: %w", err)
	}
	if len(vectors) != 1 {
		return fmt.Errorf("向量化分段返回 %d 条向量，期望 1 条", len(vectors))
	}
	return p.vectors.Upsert(ctx, []repository.VectorRecord{{
		ChunkID:    segment.ChunkID,
		DatasetID:  segment.DatasetID,
		DocumentID: segment.DocumentID,
		UserID:     doc.UserID,
		TitlePath:  strings.Join(segment.TitlePath, " / "),
		Page:       segment.Page,
		Kind:       segment.Kind,
		Content:    segment.Content,
		Keywords:   segment.Keywords,
		Vector:     vectors[0],
	}})
}

// normalizeKeywords 清洗人工输入的关键词。
//
// 去空白、去空串、去重、限长：关键词会进入 BM25 输入文本与界面展示，
// 脏数据（空串、重复、超长）会让展示错乱，也会污染词频统计。
func normalizeKeywords(keywords []string) []string {
	seen := make(map[string]struct{}, len(keywords))
	out := make([]string, 0, len(keywords))
	for _, item := range keywords {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if len([]rune(trimmed)) > maxKeywordRunes {
			trimmed = string([]rune(trimmed)[:maxKeywordRunes])
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) > model.MaxKeywordMaxCount {
		out = out[:model.MaxKeywordMaxCount]
	}
	return out
}

// maxKeywordRunes 是单个关键词的长度上限（字符数）。
// 超过这个长度的「关键词」多半是使用者整段粘贴进来的，展示不下也不像关键词。
const maxKeywordRunes = 32

// toChunkOptions 把知识库配置转成切片参数。
func toChunkOptions(config model.ChunkConfig) chunker.Options {
	return chunker.Options{
		Mode:         config.Mode,
		Separator:    config.Separator,
		MaxRunes:     config.MaxRunes,
		OverlapRunes: config.OverlapRunes,
		MinRunes:     config.MinRunes,
		KeepNoise:    config.KeepNoise,
	}
}

// parsedObjectKey 是解析产物在对象存储中的 key。
// 一个文档一个 key，重新解析直接覆盖（旧产物没有保留价值）。
func parsedObjectKey(documentID uint64) string {
	return fmt.Sprintf("parsed/%d.json", documentID)
}

// contentHash 计算片段正文哈希，用于将来的增量重切。
func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
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
