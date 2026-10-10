// EinoFlow 后端服务入口。
//
// 启动流程：加载配置 → 初始化数据库（可选自动迁移）→ 构造 Hertz server → 注册路由
// → 阻塞运行（内部处理退出信号与优雅关闭）。
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"

	"einoflow-backend/internal/config"
	// 别名：下面还有一个解析器注册表变量名叫 resolver（文档解析器），避免混淆。
	enginemodel "einoflow-backend/internal/engine/model"
	modelresolver "einoflow-backend/internal/engine/model/resolver"
	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/engine/rag/loader/mineru"
	"einoflow-backend/internal/engine/rag/loader/plaintext"
	"einoflow-backend/internal/handler"
	milvusstore "einoflow-backend/internal/repository/milvus"
	miniostore "einoflow-backend/internal/repository/minio"
	"einoflow-backend/internal/repository/postgres"
	"einoflow-backend/internal/security"
	"einoflow-backend/internal/service"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认读取环境变量 EINOFLOW_CONFIG 或 configs/config.yaml）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	ctx := context.Background()

	// 启动阶段就校验依赖：宁可起不来，也不要「看起来在跑但实际不可用」。
	if cfg.Postgres.AutoMigrate {
		appliedCount, err := postgres.Migrate(ctx, cfg.Postgres.DSN())
		if err != nil {
			log.Fatalf("执行数据库迁移失败: %v", err)
		}
		if appliedCount > 0 {
			log.Printf("数据库迁移完成：新应用 %d 个脚本", appliedCount)
		} else {
			log.Print("数据库结构已是最新，无需迁移")
		}
	}

	db, err := postgres.Open(ctx, cfg.Postgres.DSN(), postgres.PoolConfig{
		MaxOpenConns:    cfg.Postgres.MaxOpenConns,
		MaxIdleConns:    cfg.Postgres.MaxIdleConns,
		ConnMaxLifetime: cfg.Postgres.ConnMaxLifetime.Std(),
	})
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer func() {
		// gorm.DB 没有 Close，需要取回底层连接池再关闭。
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	// 仓储统一持有 Client（而不仅是 *sql.DB），这样事务能跨仓储传递。
	client := postgres.NewClient(db)
	appRepository := postgres.NewAppRepository(client)
	conversationRepository := postgres.NewConversationRepository(client)
	datasetRepository := postgres.NewDatasetRepository(client)
	documentRepository := postgres.NewDocumentRepository(client)
	segmentRepository := postgres.NewSegmentRepository(client)
	retrievalRepository := postgres.NewRetrievalRepository(client)
	modelProviderRepository := postgres.NewModelProviderRepository(client)
	modelInstanceRepository := postgres.NewModelInstanceRepository(client)
	modelBindingRepository := postgres.NewModelBindingRepository(client)

	appService := service.NewAppService(appRepository)
	datasetService := service.NewDatasetService(datasetRepository)
	conversationService := service.NewConversationService(
		conversationRepository,
		postgres.NewMessageRepository(client),
		appRepository,
		client,
	)

	// 对象存储：存放文档原始文件。
	// 启动即初始化，bucket 不存在会自动创建，避免部署时漏掉手工步骤。
	objectStore, err := miniostore.New(ctx, miniostore.Options{
		Endpoint:  cfg.MinIO.Endpoint,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		Bucket:    cfg.MinIO.Bucket,
		UseSSL:    cfg.MinIO.UseSSL,
	})
	if err != nil {
		log.Fatalf("初始化对象存储失败: %v", err)
	}

	// 解析器注册表，顺序即优先级：
	// MinerU 负责 PDF / 图片 / Office 等需要版面分析与 OCR 的格式，
	// 纯文本解析器负责 txt / md，同时兼作解析服务不可用时的降级路径。
	resolver := loader.NewResolver(
		mineru.New(mineru.NewClient(mineru.ClientOptions{
			BaseURL:     cfg.MinerU.BaseURL,
			Token:       cfg.MinerU.Token,
			PollTimeout: cfg.MinerU.Timeout.Std(),
		}), cfg.MinerU.Tier),
		plaintext.New(),
	)

	// 向量化模型：构造时即完成「可用性 + 维度」探测（见 resolver 包注释）。
	// 维度不在配置里写死，避免换模型后留下错值。
	// 配置兜底解析器：数据库里还没有任何模型绑定时用它，保证「刚部署完」也能跑起来。
	fallbackResolver, err := modelresolver.New(ctx, modelresolver.Options{
		EmbeddingProvider:  cfg.Embedding.Provider,
		EmbeddingBaseURL:   cfg.Embedding.BaseURL,
		EmbeddingModel:     cfg.Embedding.Model,
		EmbeddingBatchSize: cfg.Embedding.BatchSize,
		RerankProvider:     cfg.Rerank.Provider,
		RerankBaseURL:      cfg.Rerank.BaseURL,
		RerankModel:        cfg.Rerank.Model,
	})
	if err != nil {
		log.Fatalf("初始化兜底向量化模型失败: %v", err)
	}

	// 凭证加密器：密钥只来自环境变量，缺失时启动失败（见 config 校验）。
	secretKey, err := security.ParseKey(cfg.Security.SecretKey)
	if err != nil {
		log.Fatalf("解析加密密钥失败: %v", err)
	}
	credentialCipher, err := security.NewCipher(secretKey)
	if err != nil {
		log.Fatalf("初始化凭证加密器失败: %v", err)
	}

	// 模型解析：数据库优先（全局 / 应用 / 知识库三级覆盖），配置兜底。
	modelResolver, err := modelresolver.NewDatabase(modelresolver.DatabaseOptions{
		Cipher:    credentialCipher,
		Providers: modelProviderRepository,
		Instances: modelInstanceRepository,
		Bindings:  modelBindingRepository,
		Fallback:  fallbackResolver,
	})
	if err != nil {
		log.Fatalf("初始化模型解析器失败: %v", err)
	}

	// 启动时的向量维度以「当前生效模型」为准，并与已有集合校验。
	effective, err := modelResolver.EmbedderFor(ctx, enginemodel.ScopeRef{})
	if err != nil {
		log.Fatalf("获取当前生效的向量化模型失败: %v", err)
	}
	embedder := effective.Embedder
	if effective.InstanceID == 0 {
		log.Printf("当前向量化模型来自配置文件（尚未在「模型配置」中绑定）: %s", embedder.Name())
	}

	// 向量库：连不上或维度与已有集合不一致时直接启动失败。
	// 维度不一致必须在这里拦下——静默写入两种维度的向量只会得到一堆噪声，且极难排查。
	vectorStore, err := milvusstore.New(ctx, milvusstore.Options{
		Address:    cfg.Milvus.Address,
		Username:   cfg.Milvus.Username,
		Password:   cfg.Milvus.Password,
		Database:   cfg.Milvus.Database,
		Collection: cfg.Milvus.Collection,
	})
	if err != nil {
		log.Fatalf("初始化向量库失败: %v", err)
	}
	defer func() {
		if err := vectorStore.Close(ctx); err != nil {
			log.Printf("关闭向量库连接失败: %v", err)
		}
	}()
	if err := vectorStore.EnsureCollection(ctx, embedder.Dimension()); err != nil {
		log.Fatalf("初始化向量集合失败: %v", err)
	}
	log.Printf("向量化模型就绪: %s（%d 维），向量集合: %s",
		embedder.Name(), embedder.Dimension(), cfg.Milvus.Collection)

	// 就绪检查覆盖全部外部依赖：RAG 链路同时依赖数据库、对象存储与向量库，
	// 只探数据库会出现「/readyz 正常但上传文档必失败」的误导性绿灯。
	healthService := service.NewHealthService(
		postgres.NewHealthRepository(client),
		objectStore,
		vectorStore,
	)

	pipeline := rag.NewPipeline(rag.Deps{
		Storage:   objectStore,
		Documents: documentRepository,
		Segments:  segmentRepository,
		Tx:        client,
		Resolver:  resolver,
		ChunkOpts: chunker.Options{
			MaxRunes: cfg.MinerU.ChunkMaxRunes,
			MinRunes: cfg.MinerU.ChunkMinRunes,
		},
		Models:  modelResolver,
		Vectors: vectorStore,
	})

	documentService := service.NewDocumentService(
		datasetRepository, documentRepository, segmentRepository, objectStore, pipeline)

	modelConfigService := service.NewModelConfigService(service.ModelConfigDeps{
		Cipher:    credentialCipher,
		Providers: modelProviderRepository,
		Instances: modelInstanceRepository,
		Bindings:  modelBindingRepository,
		Apps:      appRepository,
		Datasets:  datasetRepository,
		Resolver:  modelResolver,
	})

	retriever := rag.NewRetriever(rag.RetrieverDeps{
		Models:               modelResolver,
		Vectors:              vectorStore,
		Segments:             segmentRepository,
		DefaultTopK:          cfg.Retrieval.TopK,
		DefaultCandidateTopK: cfg.Retrieval.CandidateTopK,
		RerankEnabled:        cfg.Retrieval.RerankEnabled(),
		RerankStrict:         cfg.Retrieval.RerankStrict(),
	})
	retrievalService := service.NewRetrievalService(
		datasetRepository,
		retriever,
		retrievalRepository,
		service.RetrievalDefaults{TopK: cfg.Retrieval.TopK},
	)

	// 问答：Eino 编排（检索 → 提示 → 对话模型），事件通过 SSE 推送。
	chatService := service.NewChatService(
		appRepository,
		conversationRepository,
		postgres.NewMessageRepository(client),
		modelResolver,
		retrievalService,
	)

	// 入库是异步的：解析一份百页文档要几分钟，同步等待会挂住请求。
	ingestCtx, stopIngest := context.WithCancel(ctx)
	documentService.Start(ingestCtx)
	defer func() {
		// 先取消在途解析，再等协程退出，避免退出时长时间阻塞。
		stopIngest()
		documentService.Stop()
	}()

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr()),
		// 收到退出信号后等待在途请求处理完再关闭，避免流式响应被硬切断。
		server.WithExitWaitTime(10*time.Second),
	)

	handler.Register(h, handler.Deps{
		Health:        healthService,
		Apps:          appService,
		Datasets:      datasetService,
		Conversations: conversationService,
		Documents:     documentService,
		Searches:      retrievalService,
		ModelConfig:   modelConfigService,
		Chats:         chatService,
	})

	log.Printf("EinoFlow 后端启动中: env=%s addr=%s", cfg.App.Env, cfg.Server.Addr())
	// Spin 内部处理 SIGINT / SIGTERM，并在退出时优雅关闭 server。
	h.Spin()
}
