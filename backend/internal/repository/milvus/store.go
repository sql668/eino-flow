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
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

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
	// fieldChunkID 是业务生成的片段标识（UUID），作为集合主键。
	// 用它而不是业务库自增 ID：向量库与业务库各自独立演进，换库/分库/重建索引
	// 都不用改主键语义；检索拿到它回查 PostgreSQL 取正文。
	fieldChunkID    = "chunk_id"
	fieldDocumentID = "document_id"
	fieldDatasetID  = "dataset_id"
	fieldUserID     = "user_id"
	fieldTitlePath  = "title_path"
	fieldPage       = "page"
	fieldKind       = "kind"
	fieldEmbedding  = "embedding"
	// fieldBM25Text 是 BM25 函数的唯一输入字段。
	//
	// Milvus 的 BM25 函数只接受**一个** VARCHAR 输入字段（传两个直接建集合失败），
	// 所以写入时把「正文 + 关键词」拼成一段文本，交给服务端的中文分析器切词。
	// 关键词因此真实参与词频与打分——人工调整关键词能改变全文/混合检索的命中。
	// 正文与关键词的权威副本仍在 PostgreSQL，向量库里不重复保留它们。
	fieldBM25Text  = "bm25_text"
	fieldSparse    = "sparse"
	bm25FunctionID = "bm25_text_index"
)

// 字符串字段长度上限。Milvus 要求 VarChar 显式声明 max_length。
const (
	chunkIDMaxLen   = 64
	userIDMaxLen    = 64
	titlePathMaxLen = 512
	kindMaxLen      = 16
	// contentMaxLen 是正文在 Milvus 里的长度上限（字节）。
	// Milvus 的 VarChar 上限是 65535；这里按最大切片长度留足余量：
	// 4000 字中文约 12 KB，取 32 KB 足够，也避免误配成超大值。
	contentMaxLen = 32768
	// keywordsMaxLen 是关键词部分的长度上限（5 个词，留足余量）。
	keywordsMaxLen = 512
	// bm25TextMaxLen 是 BM25 输入字段上限：正文上限 + 分隔符 + 关键词上限。
	bm25TextMaxLen = contentMaxLen + keywordsMaxLen + 1
)

// HNSW 索引参数：
//   - m 是每个节点的最大邻居数，越大召回率越高、内存占用越大；
//   - efConstruction 是建索引时的候选队列长度，越大索引质量越好、构建越慢。
//
// 取 16 / 200 是常见折中值：本项目向量规模在十万级以内，构建耗时不是瓶颈。
const (
	hnswM              = 16
	hnswEfConstruction = 200
	// sparseDropRatio 是稀疏索引在构建时丢弃低权重词项的比例。
	// 0.2 是常见折中：索引更小、构建更快，召回损失有限。
	sparseDropRatio = 0.2
)

// chineseAnalyzerParams 是中文分析器参数。
// 用 Milvus 内建的中文分析器：自己接 jieba 在 Go 里要么 CGO、要么引词典服务，
// 而这里分词只需与 BM25 索引保持一致即可。
var chineseAnalyzerParams = map[string]any{"type": "chinese"}

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
			WithName(fieldChunkID).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(chunkIDMaxLen).
			WithIsPrimaryKey(true)).
		WithField(entity.NewField().
			WithName(fieldDocumentID).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().
			WithName(fieldDatasetID).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().
			WithName(fieldUserID).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(userIDMaxLen)).
		WithField(entity.NewField().
			WithName(fieldTitlePath).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(titlePathMaxLen)).
		WithField(entity.NewField().
			WithName(fieldPage).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().
			WithName(fieldKind).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(kindMaxLen)).
		// BM25 的输入字段必须开中文分析器：词元必须与查询侧同口径，
		// 否则「索引按一种切法、查询按另一种切法」，检索会莫名其妙地漏。
		WithField(entity.NewField().
			WithName(fieldBM25Text).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(bm25TextMaxLen).
			WithEnableAnalyzer(true).
			WithAnalyzerParams(chineseAnalyzerParams)).
		// 稀疏向量字段由 BM25 函数（下面注册）自动生成，写入时不提供。
		WithField(entity.NewField().
			WithName(fieldSparse).
			WithDataType(entity.FieldTypeSparseVector)).
		// BM25 函数必须注册在 schema 上（集合选项里没有对应方法）：
		// 输入是拼接后的正文+关键词，输出到稀疏字段，词频与 IDF 由 Milvus 维护——
		// 这正是我们决定「正文也进向量库」的唯一原因。
		WithFunction(entity.NewFunction().
			WithName(bm25FunctionID).
			WithType(entity.FunctionTypeBM25).
			WithInputFields(fieldBM25Text).
			WithOutputFields(fieldSparse)).
		WithField(entity.NewField().
			WithName(fieldEmbedding).
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(dim)))

	option := milvusclient.NewCreateCollectionOption(s.collection, schema).
		WithPKFieldName(fieldChunkID).
		WithVectorFieldName(fieldEmbedding).
		// COSINE 对文本向量最稳妥：不依赖向量是否已归一化（bge-m3 输出未归一化）。
		WithMetricType(entity.COSINE).
		WithConsistencyLevel(entity.ClBounded).
		WithIndexOptions(
			milvusclient.NewCreateIndexOption(
				s.collection, fieldEmbedding, index.NewHNSWIndex(entity.COSINE, hnswM, hnswEfConstruction)),
			// 标量字段建倒排索引：检索时按 dataset_id / document_id / user_id 过滤、
			// 重建时按 document_id 删除，没有索引这些操作会退化成全量扫描。
			milvusclient.NewCreateIndexOption(s.collection, fieldDatasetID, index.NewInvertedIndex()),
			milvusclient.NewCreateIndexOption(s.collection, fieldDocumentID, index.NewInvertedIndex()),
			milvusclient.NewCreateIndexOption(s.collection, fieldUserID, index.NewInvertedIndex()),
			// 稀疏向量索引：全文检索与混合检索走它。
			milvusclient.NewCreateIndexOption(
				s.collection, fieldSparse, index.NewSparseInvertedIndex(entity.BM25, sparseDropRatio)),
		)

	if err := s.client.CreateCollection(ctx, option); err != nil {
		return fmt.Errorf("创建向量集合 %s 失败: %w", s.collection, err)
	}
	return s.load(ctx)
}

// load 把集合加载进内存。
func (s *Store) load(ctx context.Context) error {
	// 只加载检索真正需要的字段（见 loadFields 注释）：正文与关键词可能很大，
	// 全量加载会让查询节点内存随语料线性增长——2C2G 的机器上这是致命的。
	task, err := s.client.LoadCollection(ctx,
		milvusclient.NewLoadCollectionOption(s.collection).WithLoadFields(loadFields...))
	if err != nil {
		return fmt.Errorf("加载向量集合 %s 失败: %w", s.collection, err)
	}
	if err := task.Await(ctx); err != nil {
		return fmt.Errorf("等待向量集合 %s 加载完成失败: %w", s.collection, err)
	}
	return nil
}

// loadFields 是加载到查询节点的字段白名单。
//
// 刻意排除 bm25_text：它在**写入时**被 BM25 函数读取（分词与统计落在稀疏倒排里），
// 检索阶段我们不需要原文——正文一律回 PostgreSQL 取。排除之后，
// 查询节点的内存占用与「原文体积」脱钩（10 万篇文档不会因此多占 1 GB）。
var loadFields = []string{
	fieldChunkID,
	fieldDocumentID,
	fieldDatasetID,
	fieldUserID,
	fieldTitlePath,
	fieldPage,
	fieldKind,
	fieldEmbedding,
	fieldSparse,
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
// 用 Upsert 而不是 Insert：同一片段重复入库（重试、重切）时，
// Insert 会因主键重复直接失败，Upsert 则覆盖成最新向量与元数据。
func (s *Store) Upsert(ctx context.Context, records []repository.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s.dim <= 0 {
		return errors.New("向量维度未知，必须先调用 EnsureCollection")
	}

	chunkIDs := make([]string, 0, len(records))
	documentIDs := make([]int64, 0, len(records))
	datasetIDs := make([]int64, 0, len(records))
	userIDs := make([]string, 0, len(records))
	titlePaths := make([]string, 0, len(records))
	pages := make([]int64, 0, len(records))
	kinds := make([]string, 0, len(records))
	bm25Texts := make([]string, 0, len(records))
	vectors := make([][]float32, 0, len(records))

	for i, record := range records {
		if len(record.Vector) != s.dim {
			return fmt.Errorf("%w: 第 %d 条向量是 %d 维，集合是 %d 维",
				model.ErrDimensionMismatch, i, len(record.Vector), s.dim)
		}
		chunkIDs = append(chunkIDs, record.ChunkID)
		documentIDs = append(documentIDs, int64(record.DocumentID))
		datasetIDs = append(datasetIDs, int64(record.DatasetID))
		userIDs = append(userIDs, record.UserID)
		titlePaths = append(titlePaths, truncateVarchar(record.TitlePath, titlePathMaxLen))
		pages = append(pages, int64(record.Page))
		kinds = append(kinds, truncateVarchar(record.Kind, kindMaxLen))
		bm25Texts = append(bm25Texts, buildBM25Text(record.Content, record.Keywords))
		vectors = append(vectors, record.Vector)
	}

	option := milvusclient.NewColumnBasedInsertOption(
		s.collection,
		column.NewColumnVarChar(fieldChunkID, chunkIDs),
		column.NewColumnInt64(fieldDocumentID, documentIDs),
		column.NewColumnInt64(fieldDatasetID, datasetIDs),
		column.NewColumnVarChar(fieldUserID, userIDs),
		column.NewColumnVarChar(fieldTitlePath, titlePaths),
		column.NewColumnInt64(fieldPage, pages),
		column.NewColumnVarChar(fieldKind, kinds),
		column.NewColumnVarChar(fieldBM25Text, bm25Texts),
		// 稀疏列不提供：BM25 函数会自动生成，手工传反而会冲突。
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
func (s *Store) Delete(ctx context.Context, chunkIDs []string) (int64, error) {
	if len(chunkIDs) == 0 {
		return 0, nil
	}
	quoted := make([]string, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		// 主键是字符串，必须加引号并按 Milvus 表达式语法转义单引号。
		quoted = append(quoted, "'"+strings.ReplaceAll(id, "'", `\'`)+"'")
	}
	expr := fmt.Sprintf("%s in [%s]", fieldChunkID, strings.Join(quoted, ", "))
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
//
// 三种检索方式都走「服务端算分」：
//   - vector：dense 向量近邻；
//   - fulltext：Milvus 用集合的中文分析器把查询文本切词，对着 BM25 稀疏字段算分；
//   - hybrid：两路并行召回，再按权重或 RRF 融合。
//
// 这样做的好处是分数量纲、去重、截断都由 Milvus 一次完成，
// 我们不用在 Go 侧再实现一遍「按主键合并两路结果」这种容易写错的逻辑。
func (s *Store) Search(
	ctx context.Context, filter repository.SearchFilter, query repository.SearchQuery,
) ([]repository.VectorMatch, error) {
	if filter.TopK <= 0 {
		return nil, fmt.Errorf("TopK 必须为正数，当前为 %d", filter.TopK)
	}
	mode := query.Mode
	if mode == "" {
		mode = repository.SearchModeVector
	}
	if !mode.IsValid() {
		return nil, fmt.Errorf("不支持的检索方式 %q（可选：vector / fulltext / hybrid）", mode)
	}

	// 按模式校验各自必需的入参：不校验的话会退化成「用空文本或空向量去查」，
	// 表现为「检索没有结果」，而不是明确的参数错误。
	switch mode {
	case repository.SearchModeVector, repository.SearchModeHybrid:
		if len(query.Vector) == 0 {
			return nil, errors.New("查询向量为空")
		}
		if s.dim > 0 && len(query.Vector) != s.dim {
			return nil, fmt.Errorf("%w: 查询向量是 %d 维，集合是 %d 维",
				model.ErrDimensionMismatch, len(query.Vector), s.dim)
		}
	}
	if mode != repository.SearchModeVector && strings.TrimSpace(query.Text) == "" {
		return nil, errors.New("全文检索的查询文本为空")
	}

	expression, err := buildFilterExpression(filter)
	if err != nil {
		return nil, err
	}

	switch mode {
	case repository.SearchModeFullText:
		return s.searchSparse(ctx, filter.TopK, expression, query.Text)

	case repository.SearchModeHybrid:
		return s.searchHybrid(ctx, filter.TopK, expression, query)
	}

	vectorMatches, err := s.searchVector(ctx, filter.TopK, expression, query.Vector)
	if err != nil {
		return nil, err
	}
	return vectorMatches, nil
}

// searchVector 走向量（dense）路召回。
func (s *Store) searchVector(
	ctx context.Context, limit int, expression string, vector []float32,
) ([]repository.VectorMatch, error) {
	resultSets, err := s.client.Search(ctx, milvusclient.
		NewSearchOption(s.collection, limit, []entity.Vector{entity.FloatVector(vector)}).
		WithANNSField(fieldEmbedding).
		WithFilter(expression).
		WithConsistencyLevel(entity.ClStrong))
	if err != nil {
		return nil, fmt.Errorf("向量检索失败: %w", err)
	}
	return collectMatches(resultSets, limit, scoreRoute{vector: true})
}

// searchSparse 走全文（BM25 稀疏）路召回：查询文本交给服务端的中文分析器切词。
func (s *Store) searchSparse(
	ctx context.Context, limit int, expression string, text string,
) ([]repository.VectorMatch, error) {
	resultSets, err := s.client.Search(ctx, milvusclient.
		NewSearchOption(s.collection, limit, []entity.Vector{entity.Text(text)}).
		WithANNSField(fieldSparse).
		WithFilter(expression).
		WithConsistencyLevel(entity.ClStrong))
	if err != nil {
		return nil, fmt.Errorf("全文检索失败: %w", err)
	}
	return collectMatches(resultSets, limit, scoreRoute{fullText: true})
}

// searchHybrid 两路各召回 TopK 条，再在本地融合。
//
// 为什么不在服务端融合（Milvus 有 HybridSearch + WeightedReranker/RRFReranker）：
//   - 服务端的融合只回传一个融合分，两路各自的分数拿不到，于是
//     「相似度阈值只卡向量那一路」这件事在服务端之外做不到（实测 range 搜索参数
//     在 2.5.8 的 HNSW 索引上被静默忽略，既不过滤也不报错）；
//   - 本地融合还有个额外好处：留痕能把两路原始分都记下来，
//     使用者能看出「这条是靠语义还是靠关键词进来的」。
//
// 代价是两次 RPC（本机各几毫秒），换来的是可解释、可测、可控制的排序。
func (s *Store) searchHybrid(
	ctx context.Context, limit int, expression string, query repository.SearchQuery,
) ([]repository.VectorMatch, error) {
	// 两路都按 limit 召回：融合只能从两路候选里挑，每路召回少于最终条数会不够数。
	vectorMatches, err := s.searchVector(ctx, limit, expression, query.Vector)
	if err != nil {
		return nil, err
	}
	textMatches, err := s.searchSparse(ctx, limit, expression, query.Text)
	if err != nil {
		return nil, err
	}
	return fuse(query, limit, vectorMatches, textMatches), nil
}

// fuse 把两路结果合成一个排序。
//
// 两种方式：
//   - RRF：只用名次（1/(k+rank)），免疫两路分数量纲差异，不需要调参；
//   - 加权：各自先做 min-max 归一化再按权重相加，可解释、可调，
//     但归一化依赖候选集合（某一路出现极端高分时其余会被压低）。
func fuse(
	query repository.SearchQuery, limit int,
	vectorMatches []repository.VectorMatch, textMatches []repository.VectorMatch,
) []repository.VectorMatch {
	type entry struct {
		match repository.VectorMatch
		score float64
	}
	merged := make(map[string]*entry, len(vectorMatches)+len(textMatches))
	order := make([]string, 0, len(vectorMatches)+len(textMatches))
	upsert := func(chunkID string) *entry {
		if item, ok := merged[chunkID]; ok {
			return item
		}
		item := &entry{match: repository.VectorMatch{ChunkID: chunkID}}
		merged[chunkID] = item
		order = append(order, chunkID)
		return item
	}

	for _, match := range vectorMatches {
		score := match.Score
		item := upsert(match.ChunkID)
		scoreCopy := score
		item.match.VectorScore = &scoreCopy
		item.match.Score = score
	}
	for _, match := range textMatches {
		score := match.Score
		item := upsert(match.ChunkID)
		scoreCopy := score
		item.match.FullTextScore = &scoreCopy
	}

	weights := normalizeWeights(query)
	vectorNorm := minMaxScales(vectorMatches)
	textNorm := minMaxScales(textMatches)

	for _, chunkID := range order {
		item := merged[chunkID]
		switch query.Rank {
		case repository.HybridRankRRF:
			item.score = rankScore(chunkID, vectorMatches, textMatches)
		default:
			var score float64
			if item.match.VectorScore != nil {
				score += weights[0] * vectorNorm(*item.match.VectorScore)
			}
			if item.match.FullTextScore != nil {
				score += weights[1] * textNorm(*item.match.FullTextScore)
			}
			item.score = score
		}
	}

	matches := make([]repository.VectorMatch, 0, len(order))
	for _, chunkID := range order {
		item := merged[chunkID]
		item.match.Score = float32(item.score)
		matches = append(matches, item.match)
	}
	// 分数降序；同分按 chunk_id 兜底，保证同一份数据每次得到同样的顺序。
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].ChunkID < matches[j].ChunkID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// normalizeWeights 把权重收敛成和为 1 的 [向量权重, 全文权重]。
func normalizeWeights(query repository.SearchQuery) [2]float64 {
	vectorWeight, textWeight := query.VectorWeight, query.FullTextWeight
	if vectorWeight <= 0 && textWeight <= 0 {
		// 没给权重就按语义优先：中文短查询下向量检索通常更稳。
		return [2]float64{0.5, 0.5}
	}
	total := vectorWeight + textWeight
	return [2]float64{vectorWeight / total, textWeight / total}
}

// minMaxScales 生成某一路分数的归一化函数（把分数线性映射到 [0,1]）。
//
// 单一候选时按满分处理：否则「只有一条候选」会被归一化成 0 分，
// 融合后被另一边彻底压过去，看起来像「这路没起作用」。
func minMaxScales(matches []repository.VectorMatch) func(float32) float64 {
	if len(matches) == 0 {
		return func(float32) float64 { return 0 }
	}
	minScore, maxScore := matches[0].Score, matches[0].Score
	for _, match := range matches[1:] {
		if match.Score < minScore {
			minScore = match.Score
		}
		if match.Score > maxScore {
			maxScore = match.Score
		}
	}
	if maxScore == minScore {
		return func(float32) float64 { return 1 }
	}
	span := float64(maxScore - minScore)
	return func(score float32) float64 {
		return float64(score-minScore) / span
	}
}

// rrfK 是 RRF 的平滑常数，取 60（Milvus 与常见实现的默认值）。
const rrfK = 60.0

// rankScore 计算某个候选的 RRF 分数：两路名次各贡献 1/(k+rank)，未出现的路不计。
func rankScore(
	chunkID string,
	vectorMatches []repository.VectorMatch, textMatches []repository.VectorMatch,
) float64 {
	var score float64
	if rank := rankOf(vectorMatches, chunkID); rank > 0 {
		score += 1 / (rrfK + float64(rank))
	}
	if rank := rankOf(textMatches, chunkID); rank > 0 {
		score += 1 / (rrfK + float64(rank))
	}
	return score
}

// rankOf 返回候选在某一路里的名次（从 1 开始），没出现返回 0。
func rankOf(matches []repository.VectorMatch, chunkID string) int {
	for index, match := range matches {
		if match.ChunkID == chunkID {
			return index + 1
		}
	}
	return 0
}

// scoreRoute 描述本次检索走了哪一路，决定命中的分数往哪个字段填。
type scoreRoute struct {
	vector   bool
	fullText bool
}

// collectMatches 把 Milvus 的检索结果整理成命中列表。
//
// 取结果集的第一组：单路检索只有一个结果集，混合检索的融合结果也在第一组。
// 顺带把分数按路线填好，调用方据此展示「这条是向量命中的还是关键词命中的」。
func collectMatches(
	resultSets []milvusclient.ResultSet, limit int, route scoreRoute,
) ([]repository.VectorMatch, error) {
	matches := make([]repository.VectorMatch, 0, limit)
	for _, set := range resultSets {
		if set.Err != nil {
			return nil, fmt.Errorf("检索返回错误: %w", set.Err)
		}
		for i := 0; i < set.Len(); i++ {
			chunkID, err := set.IDs.GetAsString(i)
			if err != nil {
				return nil, fmt.Errorf("读取命中主键失败: %w", err)
			}
			var score float32
			if i < len(set.Scores) {
				score = set.Scores[i]
			}
			match := repository.VectorMatch{ChunkID: chunkID, Score: score}
			switch {
			case route.vector:
				value := score
				match.VectorScore = &value
			case route.fullText:
				value := score
				match.FullTextScore = &value
			}
			matches = append(matches, match)
		}
	}
	return matches, nil
}

// buildFilterExpression 拼装 Milvus 过滤表达式。
//
// 过滤必须在这里完成（而不是取回结果再筛）：先取 TopK 再筛会出现结果塌陷——
// 库里明明有匹配内容，却因为被筛掉而返回不足条数。
func buildFilterExpression(filter repository.SearchFilter) (string, error) {
	parts := []string{fmt.Sprintf("%s == %d", fieldDatasetID, filter.DatasetID)}
	if len(filter.DocumentIDs) > 0 {
		parts = append(parts, fmt.Sprintf("%s in [%s]", fieldDocumentID, joinInt64(filter.DocumentIDs)))
	}
	if len(filter.UserIDs) > 0 {
		quoted := make([]string, 0, len(filter.UserIDs))
		for _, id := range filter.UserIDs {
			quoted = append(quoted, "'"+strings.ReplaceAll(id, "'", `\'`)+"'")
		}
		parts = append(parts, fmt.Sprintf("%s in [%s]", fieldUserID, strings.Join(quoted, ", ")))
	}
	return strings.Join(parts, " and "), nil
}

// joinInt64 把 ID 列表拼成表达式里的数组字面量。
func joinInt64(values []uint64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatUint(value, 10))
	}
	return strings.Join(parts, ", ")
}

// truncateVarchar 按字节截断字符串以适配 Milvus 的 max_length。
//
// 按字节而不是字符：Milvus 的 max_length 以字节计。
// 截断时避免把多字节字符切成半个（否则写入会失败），因此回退到合法边界。
func truncateVarchar(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	truncated := text[:maxLen]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

// buildBM25Text 把正文与关键词拼成 BM25 的输入文本。
//
// 顺序固定为「正文在前、关键词在后」：分析器不看顺序，但排在前面的正文
// 更长、更容易被截断，关键词放在尾部才能保证不被挤掉。
// 关键词重复出现在尾部会让它的词频略高——这正是我们想要的加权效果。
func buildBM25Text(content string, keywords []string) string {
	text := truncateVarchar(content, contentMaxLen)
	if len(keywords) == 0 {
		return text
	}
	joined := truncateVarchar(strings.Join(keywords, " "), keywordsMaxLen)
	return text + " " + joined
}

// Close 实现 repository.VectorStore。
func (s *Store) Close(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Close(ctx)
}

// 说明：这里**没有**提供「调用服务端分析器分词」的方法。
//
// 曾经打算用 Milvus 的中文分析器给关键词抽取供词（这样索引与关键词同口径），
// 实测 Milvus 2.5.8 没有实现这个 RPC：调用 RunAnalyzer 返回
// `Unimplemented ... unknown method RunAnalyzer`（该接口是更高版本才加的）。
// 为了不把「本地能跑」变成「必须升级向量库」，关键词改由本地分词器处理
// （见 engine/rag/keyword.LocalTokenizer）。检索侧不受影响：
// 查询文本仍由服务端分析器切词，与索引口径一致。

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
