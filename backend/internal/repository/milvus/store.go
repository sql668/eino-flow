// Package milvus 提供向量库（Milvus）的实现。
//
// 设计要点：
//   - **所有知识库共用一个集合**，用 dataset_id 字段区分。每个知识库单独建集合会带来
//     大量小集合，每个都要建索引、加载、管理，运维成本远高于多存一列；
//   - 只存「主键 + 过滤字段 + 向量」，文本与溯源信息留在 PostgreSQL。
//     向量库擅长算相似度，不擅长当数据库用；
//   - 维度在启动时**校验而非假设**：集合已存在但维度与当前模型不符时直接报错，
//     理由是两种维度的向量混在一个集合里只会得到一堆噪声，且很难定位。
package milvus

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/repository"
)

// 字段名集中定义：建集合、写入、检索三处必须完全一致。
// 散落的字符串字面量一旦写错，表现是「检索永远没有结果」而不是报错，排查成本极高。
const (
	fieldSegmentID  = "segment_id"
	fieldDocumentID = "document_id"
	fieldDatasetID  = "dataset_id"
	fieldEmbedding  = "embedding"
)

// HNSW 索引参数：
//   - m 是每个节点的最大邻居数，越大召回率越高、内存占用越大；
//   - efConstruction 是建索引时的候选队列长度，越大索引质量越好、构建越慢。
//
// 取 16 / 200 是常见折中值：本项目向量规模在十万级以内，构建耗时不是瓶颈。
const (
	hnswM              = 16
	hnswEfConstruction = 200
)

// defaultDatabase 是 Milvus 内置的默认库名。
const defaultDatabase = "default"

// Options 是构造参数。
type Options struct {
	Address    string
	Username   string
	Password   string
	Database   string
	Collection string
}

// Store 实现 repository.VectorStore。
type Store struct {
	client     *milvusclient.Client
	collection string
	// dim 是 EnsureCollection 校验通过的向量维度，供写入时做防御性校验。
	dim int
}

// 编译期确认接口实现，避免实现漂移后到调用处才报错。
var _ repository.VectorStore = (*Store)(nil)

// New 连接 Milvus 并确保目标数据库存在。
//
// 数据库不存在时自动创建：与对象存储自动建 bucket 保持一致的处理，
// 避免部署时漏掉一步手工操作，而失败现象只是「上传文档一直失败」。
func New(ctx context.Context, opts Options) (*Store, error) {
	if strings.TrimSpace(opts.Address) == "" {
		return nil, errors.New("milvus.address 不能为空")
	}
	if strings.TrimSpace(opts.Collection) == "" {
		return nil, errors.New("milvus.collection 不能为空")
	}

	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address:  opts.Address,
		Username: opts.Username,
		Password: opts.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 Milvus 失败（%s）: %w", opts.Address, err)
	}

	if err := ensureDatabase(ctx, client, opts.Database); err != nil {
		// 连接已经建立，失败时同样要回收，否则每次启动重试都会漏一个连接。
		_ = client.Close(ctx)
		return nil, err
	}

	return &Store{client: client, collection: strings.TrimSpace(opts.Collection)}, nil
}

// ensureDatabase 确保客户端切到目标数据库，库不存在则创建。
func ensureDatabase(ctx context.Context, client *milvusclient.Client, database string) error {
	name := strings.TrimSpace(database)
	if name == "" || name == defaultDatabase {
		return nil
	}

	names, err := client.ListDatabase(ctx, milvusclient.NewListDatabaseOption())
	if err != nil {
		return fmt.Errorf("查询 Milvus 数据库列表失败: %w", err)
	}

	found := false
	for _, item := range names {
		if item == name {
			found = true
			break
		}
	}
	if !found {
		if err := client.CreateDatabase(ctx, milvusclient.NewCreateDatabaseOption(name)); err != nil {
			return fmt.Errorf("创建 Milvus 数据库 %s 失败: %w", name, err)
		}
	}

	if err := client.UseDatabase(ctx, milvusclient.NewUseDatabaseOption(name)); err != nil {
		return fmt.Errorf("切换 Milvus 数据库 %s 失败: %w", name, err)
	}
	return nil
}

// EnsureCollection 实现 repository.VectorStore：集合不存在则创建，存在则校验维度并确保已加载。
func (s *Store) EnsureCollection(ctx context.Context, dim int) error {
	if dim <= 0 {
		return fmt.Errorf("向量维度必须为正数，当前为 %d", dim)
	}

	exists, err := s.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collection))
	if err != nil {
		return fmt.Errorf("检查向量集合 %s 是否存在失败: %w", s.collection, err)
	}
	if !exists {
		if err := s.createCollection(ctx, dim); err != nil {
			return err
		}
		s.dim = dim
		return nil
	}

	collection, err := s.client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(s.collection))
	if err != nil {
		return fmt.Errorf("读取向量集合 %s 结构失败: %w", s.collection, err)
	}

	currentDim, err := vectorDim(collection)
	if err != nil {
		return err
	}
	if currentDim != dim {
		return fmt.Errorf("%w: 集合 %s 的向量是 %d 维，当前模型输出 %d 维；"+
			"换过 embedding 模型时必须重建集合（同名集合不会被自动覆盖）",
			model.ErrDimensionMismatch, s.collection, currentDim, dim)
	}

	// 集合加载到内存后才能检索。重启后 Milvus 可能已卸载，这里补一次。
	if !collection.Loaded {
		if err := s.load(ctx); err != nil {
			return err
		}
	}

	s.dim = dim
	return nil
}

// createCollection 建集合，并把向量索引与过滤字段的索引一并建好。
func (s *Store) createCollection(ctx context.Context, dim int) error {
	schema := entity.NewSchema().
		WithName(s.collection).
		WithDescription("EinoFlow 文档分段向量").
		// 主键不由 Milvus 生成：分段主键来自 PostgreSQL，两边的键必须一致才好对账。
		WithAutoID(false).
		WithField(entity.NewField().
			WithName(fieldSegmentID).
			WithDataType(entity.FieldTypeInt64).
			WithIsPrimaryKey(true)).
		WithField(entity.NewField().
			WithName(fieldDocumentID).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().
			WithName(fieldDatasetID).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().
			WithName(fieldEmbedding).
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(dim)))

	option := milvusclient.NewCreateCollectionOption(s.collection, schema).
		WithPKFieldName(fieldSegmentID).
		WithVectorFieldName(fieldEmbedding).
		// COSINE 对文本向量最稳妥：不依赖向量是否已归一化（bge-m3 输出未归一化）。
		WithMetricType(entity.COSINE).
		WithConsistencyLevel(entity.ClBounded).
		WithIndexOptions(
			milvusclient.NewCreateIndexOption(
				s.collection, fieldEmbedding, index.NewHNSWIndex(entity.COSINE, hnswM, hnswEfConstruction)),
			// 标量字段建倒排索引：检索时按 dataset_id 过滤、重建时按 document_id 删除，
			// 没有索引这两类操作会退化成全量扫描。
			milvusclient.NewCreateIndexOption(s.collection, fieldDatasetID, index.NewInvertedIndex()),
			milvusclient.NewCreateIndexOption(s.collection, fieldDocumentID, index.NewInvertedIndex()),
		)

	if err := s.client.CreateCollection(ctx, option); err != nil {
		return fmt.Errorf("创建向量集合 %s 失败: %w", s.collection, err)
	}
	return s.load(ctx)
}

// load 把集合加载进内存。
func (s *Store) load(ctx context.Context) error {
	task, err := s.client.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(s.collection))
	if err != nil {
		return fmt.Errorf("加载向量集合 %s 失败: %w", s.collection, err)
	}
	if err := task.Await(ctx); err != nil {
		return fmt.Errorf("等待向量集合 %s 加载完成失败: %w", s.collection, err)
	}
	return nil
}

// vectorDim 从集合结构中读出向量字段的维度。
func vectorDim(collection *entity.Collection) (int, error) {
	if collection == nil || collection.Schema == nil {
		return 0, errors.New("向量集合结构为空，无法读取维度")
	}
	for _, field := range collection.Schema.Fields {
		if field.DataType != entity.FieldTypeFloatVector {
			continue
		}
		dim, err := field.GetDim()
		if err != nil {
			return 0, fmt.Errorf("向量字段 %s 的维度不可读: %w", field.Name, err)
		}
		return int(dim), nil
	}
	return 0, errors.New("向量集合里没有浮点向量字段")
}

// Upsert 实现 repository.VectorStore。
//
// 用 Upsert 而不是 Insert：同一分段重复入库（重试、重新解析）时，
// Insert 会因主键重复直接失败，Upsert 则覆盖成最新向量。
func (s *Store) Upsert(ctx context.Context, records []repository.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s.dim <= 0 {
		return errors.New("向量维度未知，必须先调用 EnsureCollection")
	}

	segmentIDs := make([]int64, 0, len(records))
	documentIDs := make([]int64, 0, len(records))
	datasetIDs := make([]int64, 0, len(records))
	vectors := make([][]float32, 0, len(records))
	for i, record := range records {
		if len(record.Vector) != s.dim {
			return fmt.Errorf("%w: 第 %d 条向量是 %d 维，集合是 %d 维",
				model.ErrDimensionMismatch, i, len(record.Vector), s.dim)
		}
		segmentIDs = append(segmentIDs, int64(record.SegmentID))
		documentIDs = append(documentIDs, int64(record.DocumentID))
		datasetIDs = append(datasetIDs, int64(record.DatasetID))
		vectors = append(vectors, record.Vector)
	}

	option := milvusclient.NewColumnBasedInsertOption(
		s.collection,
		column.NewColumnInt64(fieldSegmentID, segmentIDs),
		column.NewColumnInt64(fieldDocumentID, documentIDs),
		column.NewColumnInt64(fieldDatasetID, datasetIDs),
		column.NewColumnFloatVector(fieldEmbedding, s.dim, vectors),
	)

	result, err := s.client.Upsert(ctx, option)
	if err != nil {
		return fmt.Errorf("写入向量失败: %w", err)
	}
	if int(result.UpsertCount) != len(records) {
		// 数量对不上说明有记录没写进去，必须报出来而不是当作成功：
		// 否则会出现「文档显示已就绪但检索不到」的静默缺数据。
		return fmt.Errorf("写入向量数量不符：期望 %d 条，实际写入 %d 条", len(records), result.UpsertCount)
	}
	return nil
}

// DeleteByDocument 实现 repository.VectorStore。
func (s *Store) DeleteByDocument(ctx context.Context, documentID uint64) (int64, error) {
	return s.deleteByExpr(ctx, fmt.Sprintf("%s == %d", fieldDocumentID, documentID))
}

// Delete 实现 repository.VectorStore。
func (s *Store) Delete(ctx context.Context, segmentIDs []uint64) (int64, error) {
	if len(segmentIDs) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(segmentIDs))
	for _, id := range segmentIDs {
		ids = append(ids, fmt.Sprintf("%d", id))
	}
	expr := fmt.Sprintf("%s in [%s]", fieldSegmentID, strings.Join(ids, ", "))
	return s.deleteByExpr(ctx, expr)
}

// deleteByExpr 按表达式删除，返回删除条数。
func (s *Store) deleteByExpr(ctx context.Context, expr string) (int64, error) {
	result, err := s.client.Delete(ctx, milvusclient.NewDeleteOption(s.collection).WithExpr(expr))
	if err != nil {
		return 0, fmt.Errorf("删除向量失败（条件 %s）: %w", expr, err)
	}
	return result.DeleteCount, nil
}

// Search 实现 repository.VectorStore。
func (s *Store) Search(
	ctx context.Context, datasetID uint64, vector []float32, topK int,
) ([]repository.VectorMatch, error) {
	if len(vector) == 0 {
		return nil, errors.New("查询向量为空")
	}
	if topK <= 0 {
		return nil, fmt.Errorf("topK 必须为正数，当前为 %d", topK)
	}
	if s.dim > 0 && len(vector) != s.dim {
		return nil, fmt.Errorf("%w: 查询向量是 %d 维，集合是 %d 维", model.ErrDimensionMismatch, len(vector), s.dim)
	}

	option := milvusclient.NewSearchOption(s.collection, topK, []entity.Vector{entity.FloatVector(vector)}).
		WithANNSField(fieldEmbedding).
		// 只检索当前知识库：不做这个过滤就会跨库召回，把别的知识库内容答给用户。
		WithFilter(fmt.Sprintf("%s == %d", fieldDatasetID, datasetID)).
		// Strong 一致性：入库后立刻检索要能看到刚写入的向量。
		// 默认的 Bounded 会有秒级可见性延迟，演示与测试时会表现为「刚上传完搜不到」。
		WithConsistencyLevel(entity.ClStrong)

	resultSets, err := s.client.Search(ctx, option)
	if err != nil {
		return nil, fmt.Errorf("向量检索失败: %w", err)
	}

	matches := make([]repository.VectorMatch, 0, topK)
	for _, set := range resultSets {
		if set.Err != nil {
			return nil, fmt.Errorf("向量检索返回错误: %w", set.Err)
		}
		for i := 0; i < set.Len(); i++ {
			id, err := set.IDs.GetAsInt64(i)
			if err != nil {
				return nil, fmt.Errorf("读取命中主键失败: %w", err)
			}
			var score float32
			if i < len(set.Scores) {
				score = set.Scores[i]
			}
			matches = append(matches, repository.VectorMatch{SegmentID: uint64(id), Score: score})
		}
	}
	return matches, nil
}

// Close 实现 repository.VectorStore。
func (s *Store) Close(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Close(ctx)
}

// Name 实现 repository.Dependency。
func (s *Store) Name() string { return "Milvus" }

// Ping 探测向量库连通性，供就绪检查使用。
//
// 用 ListDatabase 而不是查集合：集合不存在也是「连得上」，不该被判成不可用。
// 就绪探针要回答的是「服务能不能用」，不是「数据准备好没有」。
func (s *Store) Ping(ctx context.Context) error {
	if _, err := s.client.ListDatabase(ctx, milvusclient.NewListDatabaseOption()); err != nil {
		return fmt.Errorf("向量库不可用: %w", err)
	}
	return nil
}

// DropCollection 删除整个集合。
//
// 刻意不出现在 repository.VectorStore 里：删集合是运维动作（换 embedding 模型后重建索引、
// 清理环境），不属于业务链路的日常能力，放进接口会诱使业务代码去调它。
func (s *Store) DropCollection(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("集合名不能为空")
	}
	if err := s.client.DropCollection(ctx, milvusclient.NewDropCollectionOption(name)); err != nil {
		return fmt.Errorf("删除向量集合 %s 失败: %w", name, err)
	}
	return nil
}
