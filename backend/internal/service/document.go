package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

const (
	// maxDocumentNameRunes 与 document.name 列长度对齐。
	maxDocumentNameRunes = 255
	// maxUploadBytes 限制单次上传大小：解析是 CPU 密集型，超大文件会把队列堵死。
	maxUploadBytes = 100 << 20 // 100 MiB
	// ingestQueueSize 是入库队列容量。
	// 这是**进程内**队列，重启会丢。生产应换成 Redis 之类的持久队列，
	// 当前形态只保证「上传请求不被解析阻塞」。
	ingestQueueSize = 256
	// ingestWorkers 是并发解析数。解析很吃 CPU，开太多反而互相拖慢。
	ingestWorkers = 1
)

// UploadDocumentInput 是上传文档的入参。
type UploadDocumentInput struct {
	DatasetID uint64
	Filename  string
	Content   []byte
	MIMEType  string
	// UserID 是上传人标识：随分段写入向量库元数据（权限过滤预留）。
	// V0.1 还没有用户体系，由调用方给占位值。
	UserID string
}

// ListDocumentsInput 是文档列表查询入参。
type ListDocumentsInput struct {
	DatasetID uint64
	Status    model.DocumentStatus
	Page      int
	PageSize  int
}

// DocumentListResult 是文档列表查询结果。
type DocumentListResult struct {
	Items    []*model.Document
	Total    int64
	Page     int
	PageSize int
}

// ListSegmentsInput 是分段列表查询入参。
type ListSegmentsInput struct {
	DocumentID uint64
	Page       int
	PageSize   int
}

// SegmentListResult 是分段列表查询结果。
type SegmentListResult struct {
	Items    []*model.Segment
	Total    int64
	Page     int
	PageSize int
}

// ingestJob 是后台作业。
//
// 解析与入库分成两类：解析完停在「待确认」，由使用者确认分段配置后才会入库。
type ingestJob struct {
	documentID uint64
	// kind 为 parse（解析）或 ingest（按配置入库）。
	kind string
	// config 仅在 kind=ingest 时使用。
	config model.ChunkConfig
}

// 作业类型。
const (
	jobParse  = "parse"
	jobIngest = "ingest"
)

// DocumentService 承载文档上传、查询与流水线调度。
type DocumentService struct {
	datasets  repository.DatasetRepository
	documents repository.DocumentRepository
	segments  repository.SegmentRepository
	storage   repository.ObjectStore
	pipeline  *rag.Pipeline
	// retrievalDefaults 是检索配置的兜底值（知识库没配过时用）。
	retrievalDefaults RetrievalDefaults

	queue   chan ingestJob
	workers sync.WaitGroup
}

// NewDocumentService 构造文档服务。
func NewDocumentService(
	datasets repository.DatasetRepository,
	documents repository.DocumentRepository,
	segments repository.SegmentRepository,
	storage repository.ObjectStore,
	pipeline *rag.Pipeline,
	retrievalDefaults RetrievalDefaults,
) *DocumentService {
	if retrievalDefaults.TopK <= 0 {
		retrievalDefaults.TopK = model.DefaultRetrievalTopK
	}
	if retrievalDefaults.CandidateTopK <= 0 {
		retrievalDefaults.CandidateTopK = retrievalDefaults.TopK * 4
	}
	return &DocumentService{
		datasets:          datasets,
		documents:         documents,
		segments:          segments,
		storage:           storage,
		pipeline:          pipeline,
		retrievalDefaults: retrievalDefaults,
		queue:             make(chan ingestJob, ingestQueueSize),
	}
}

// Start 启动后台入库协程。ctx 取消时在途解析会被打断。
func (s *DocumentService) Start(ctx context.Context) {
	for i := 0; i < ingestWorkers; i++ {
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-s.queue:
					if !ok {
						return
					}
					if err := s.runJob(ctx, job); err != nil {
						// 失败状态已由流水线写入文档记录，这里记日志便于排查。
						log.Printf("文档 %d %s 失败: %v", job.documentID, job.kind, err)
					}
				}
			}
		}()
	}
}

// Stop 关闭队列并等待在途任务结束。
func (s *DocumentService) Stop() {
	close(s.queue)
	s.workers.Wait()
}

// Upload 保存文件、建立文档记录，并投递后台解析任务。
func (s *DocumentService) Upload(ctx context.Context, in UploadDocumentInput) (*model.Document, error) {
	if in.DatasetID == 0 {
		return nil, fmt.Errorf("%w: datasetId 不能为空", ErrInvalidInput)
	}
	name := filepath.Base(strings.TrimSpace(in.Filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return nil, fmt.Errorf("%w: 文件名不能为空", ErrInvalidInput)
	}
	if len([]rune(name)) > maxDocumentNameRunes {
		return nil, fmt.Errorf("%w: 文件名不能超过 %d 个字符", ErrInvalidInput, maxDocumentNameRunes)
	}
	if len(in.Content) == 0 {
		return nil, fmt.Errorf("%w: 文件内容为空", ErrInvalidInput)
	}
	if len(in.Content) > maxUploadBytes {
		return nil, fmt.Errorf("%w: 文件超过 %d MB 上限", ErrInvalidInput, maxUploadBytes>>20)
	}

	// 数据表没有外键，所以在这里显式校验知识库存在，避免产生孤儿文档。
	if _, err := s.datasets.GetByID(ctx, in.DatasetID); err != nil {
		return nil, translateError(err)
	}

	// 先落对象存储再写元数据：反过来的话，元数据会指向一个并不存在的对象。
	key, err := objectKey(in.DatasetID, name)
	if err != nil {
		return nil, err
	}
	if err := s.storage.Put(ctx, key, in.Content, in.MIMEType); err != nil {
		return nil, err
	}

	doc := &model.Document{
		DatasetID: in.DatasetID,
		Name:      name,
		Source:    key,
		FileSize:  int64(len(in.Content)),
		Status:    model.DocumentStatusPending,
		UserID:    in.UserID,
	}
	if err := s.documents.Create(ctx, doc); err != nil {
		// 元数据写失败时清理已上传的对象，避免留下无人引用的孤儿文件。
		if deleteErr := s.storage.Delete(ctx, key); deleteErr != nil {
			log.Printf("清理孤儿对象 %s 失败: %v", key, deleteErr)
		}
		return nil, err
	}

	// 上传后先解析（解析产物落库），之后停「待确认」等使用者确认分段配置。
	s.enqueue(ingestJob{documentID: doc.ID, kind: jobParse})
	return doc, nil
}

// runJob 执行一个后台作业。
func (s *DocumentService) runJob(ctx context.Context, job ingestJob) error {
	switch job.kind {
	case jobParse:
		return s.pipeline.Parse(ctx, job.documentID)
	case jobIngest:
		return s.pipeline.Ingest(ctx, job.documentID, job.config)
	default:
		return fmt.Errorf("未知作业类型 %q", job.kind)
	}
}

// enqueue 投递后台作业。
//
// 队列满时不阻塞请求：宁可明确报错，也不要让 HTTP 请求挂住。
// 代价是该文档会停在当前状态，需要重试兜底——这属于持久队列要解决的问题。
func (s *DocumentService) enqueue(job ingestJob) {
	select {
	case s.queue <- job:
	default:
		log.Printf("队列已满，文档 %d 的 %s 作业未投递（需人工重试或补持久队列）",
			job.documentID, job.kind)
	}
}

// Get 查询单个文档。
func (s *DocumentService) Get(ctx context.Context, id uint64) (*model.Document, error) {
	doc, err := s.documents.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}
	return doc, nil
}

// List 分页查询文档。
func (s *DocumentService) List(ctx context.Context, in ListDocumentsInput) (*DocumentListResult, error) {
	page, pageSize := normalizePaging(in.Page, in.PageSize)

	docs, total, err := s.documents.List(ctx, repository.DocumentListFilter{
		DatasetID: in.DatasetID,
		Status:    in.Status,
		Offset:    (page - 1) * pageSize,
		Limit:     pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &DocumentListResult{Items: docs, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListSegments 分页查询文档分段，便于前端展示切片结果、也便于排查解析质量。
func (s *DocumentService) ListSegments(ctx context.Context, in ListSegmentsInput) (*SegmentListResult, error) {
	// 文档不存在时返回 404 而不是空列表，让调用方能区分「没切片」与「文档不存在」。
	if _, err := s.documents.GetByID(ctx, in.DocumentID); err != nil {
		return nil, translateError(err)
	}

	page, pageSize := normalizePaging(in.Page, in.PageSize)
	segments, total, err := s.segments.List(ctx, repository.SegmentListFilter{
		DocumentID: in.DocumentID,
		Offset:     (page - 1) * pageSize,
		Limit:      pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &SegmentListResult{Items: segments, Total: total, Page: page, PageSize: pageSize}, nil
}

// UpdateSegmentKeywordsInput 是「只改分段关键词」的入参。
type UpdateSegmentKeywordsInput struct {
	DocumentID uint64
	SegmentID  uint64
	// Keywords 是覆盖后的完整关键词列表（不是增量）。
	Keywords []string
}

// UpdateSegmentKeywords 覆盖单个分段的关键词，并同步到检索索引。
//
// 同步这一步不能省：关键词会进 BM25 的输入文本，只改业务库的话，
// 界面显示的关键词与实际参与检索的内容就不一致，是最难排查的那类问题。
func (s *DocumentService) UpdateSegmentKeywords(
	ctx context.Context, in UpdateSegmentKeywordsInput,
) (*model.Segment, error) {
	if in.DocumentID == 0 || in.SegmentID == 0 {
		return nil, fmt.Errorf("%w: 文档与分段 ID 不能为空", ErrInvalidInput)
	}
	doc, err := s.documents.GetByID(ctx, in.DocumentID)
	if err != nil {
		return nil, translateError(err)
	}
	if count := countKeywords(in.Keywords); count > model.MaxKeywordMaxCount {
		return nil, fmt.Errorf("%w: 关键词最多 %d 个，当前 %d 个",
			ErrInvalidInput, model.MaxKeywordMaxCount, count)
	}

	segment, err := s.pipeline.UpdateSegmentKeywords(ctx, doc.DatasetID, in.SegmentID, in.Keywords)
	if err != nil {
		return segment, translateError(err)
	}
	// 只允许改本档下的分段：分段表用 dataset_id 收敛了范围，
	// 但「这条分段是不是这篇文档的」还要再对一次，避免跨文档改数据。
	if segment.DocumentID != in.DocumentID {
		return nil, fmt.Errorf("%w: 分段 %d 不属于文档 %d", ErrInvalidInput, in.SegmentID, in.DocumentID)
	}
	return segment, nil
}

// countKeywords 统计去掉空白后剩余的关键词个数。
func countKeywords(keywords []string) int {
	count := 0
	for _, item := range keywords {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}

// Delete 软删除文档。
//
// 分段是派生数据，直接删除（重新解析可重建）；原始文件保留在对象存储里，
// 这样将来「恢复」或「换解析器重跑」都还有原材料。
func (s *DocumentService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.documents.GetByID(ctx, id); err != nil {
		return translateError(err)
	}

	if err := s.documents.SoftDelete(ctx, id); err != nil {
		return translateError(err)
	}

	if _, err := s.segments.DeleteByDocument(ctx, id); err != nil {
		// 分段清理失败不应让删除整体失败：文档已标记删除，残留分段不会再被检索到，
		// 但需要留日志以便后续清理。
		log.Printf("文档 %d 已删除，但分段清理失败: %v", id, err)
	}
	return nil
}

// objectKey 生成对象存储的 key。
// 带随机前缀是为了避免同名文件互相覆盖——同一份文档被重复上传是很常见的操作。
func objectKey(datasetID uint64, filename string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成对象 key 失败: %w", err)
	}
	return fmt.Sprintf("datasets/%d/%s-%s", datasetID, hex.EncodeToString(buf[:]), filename), nil
}
