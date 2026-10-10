// Package config 负责加载并校验应用配置。
//
// 约定（见根 AGENTS.md / backend/AGENTS.md）：
//   - 所有可变项统一从配置文件 + 环境变量读取，业务代码禁止硬编码；
//   - 密钥类字段只通过环境变量注入，不写入仓库。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"einoflow-backend/internal/model"
)

const (
	// envConfigPath 是指定配置文件路径的环境变量名。
	envConfigPath = "EINOFLOW_CONFIG"
	// defaultConfigPath 是默认配置文件路径（相对 backend/ 目录）。
	defaultConfigPath = "configs/config.yaml"
)

// Config 是应用顶层配置，结构对应 configs/config.yaml。
type Config struct {
	App      AppConfig      `yaml:"app"`
	Server   ServerConfig   `yaml:"server"`
	Log      LogConfig      `yaml:"log"`
	Postgres PostgresConfig `yaml:"postgres"`
	Redis    RedisConfig    `yaml:"redis"`
	Milvus   MilvusConfig   `yaml:"milvus"`
	MinIO    MinIOConfig    `yaml:"minio"`
	MinerU   MinerUConfig   `yaml:"mineru"`
	// Security 是安全相关配置（凭证加密密钥）。
	Security SecurityConfig `yaml:"security"`
	// Embedding / Retrieval 是模型与检索配置。
	// 阶段 1 先走配置文件（见 docs/PROGRESS.md 第 7 节），阶段 2 会换成数据库驱动，
	// 届时这两节的定位变成「默认值 + 兜底」，消费方（engine/rag）不用改。
	Embedding EmbeddingConfig `yaml:"embedding"`
	Retrieval RetrievalConfig `yaml:"retrieval"`
	// Rerank 是重排模型的**配置兜底**（数据库里没绑定时生效），与 Embedding 同构。
	Rerank RerankConfig `yaml:"rerank"`
}

// AppConfig 描述应用自身元信息。
type AppConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"` // dev / test / prod
}

// IsProd 判断当前是否为生产环境。
func (a AppConfig) IsProd() bool {
	return strings.EqualFold(a.Env, "prod")
}

// ServerConfig 描述 HTTP 服务监听配置。
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Addr 返回 Hertz 需要的监听地址，形如 0.0.0.0:8888。
func (s ServerConfig) Addr() string {
	host := s.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := s.Port
	if port == 0 {
		port = defaultServerPort
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// LogConfig 描述日志配置。
type LogConfig struct {
	Level string `yaml:"level"` // debug / info / warn / error
}

// PostgresConfig 描述业务数据库连接配置。
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	// SSLMode 对应 PostgreSQL 的 sslmode：本地 / 内网用 disable，云上一般用 require。
	SSLMode string `yaml:"sslmode"`

	// 连接池参数
	MaxOpenConns    int      `yaml:"max_open_conns"`
	MaxIdleConns    int      `yaml:"max_idle_conns"`
	ConnMaxLifetime Duration `yaml:"conn_max_lifetime"`

	// AutoMigrate 为 true 时服务启动会自动执行数据库迁移。
	// 本地开发方便；生产建议关闭，改由部署流程显式执行，避免多实例并发迁移。
	AutoMigrate bool `yaml:"auto_migrate"`
}

// DSN 返回 PostgreSQL 连接串。
//
// 两点说明：
//   - 用 URL 形式而不是 "key=value" 形式，是为了让密码中的特殊字符由标准库正确转义；
//   - TimeZone=UTC 把连接会话时区固定为 UTC。配合 timestamptz，时间在存储层就是绝对时刻，
//     不存在 MySQL DATETIME 那种「存进去是 UTC、读出来按本地时区解析」的偏差。
func (p PostgresConfig) DSN() string {
	sslMode := p.SSLMode
	if sslMode == "" {
		sslMode = defaultPostgresSSLMode
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.User, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.Database,
		RawQuery: url.Values{"sslmode": {sslMode}, "TimeZone": {"UTC"}}.Encode(),
	}
	return u.String()
}

// RedisConfig 描述缓存 / 会话存储配置。
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// Addr 返回 Redis 地址，形如 127.0.0.1:6379。
func (r RedisConfig) Addr() string {
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := r.Port
	if port == 0 {
		port = defaultRedisPort
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// MilvusConfig 描述向量库连接配置。
// Milvus 由本项目根目录 docker-compose.yml 统一编排（etcd / Milvus / Attu）。
type MilvusConfig struct {
	Address  string `yaml:"address"` // 形如 127.0.0.1:19530
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	// Collection 是存放分段向量的集合名。
	// 所有知识库**共用一个集合**，靠 dataset_id 字段过滤区分：
	// 「一个知识库一个集合」会产生大量小集合，每个都要单独建索引与加载，运维成本高得多。
	Collection string `yaml:"collection"`
}

// EmbeddingConfig 描述向量化模型（Embedding）配置。
//
// 只描述「用哪个模型、在哪调用」三件事，不含业务参数；
// 模型名与维度不做绑定（维度由启动时探测得到），避免换模型时留下错误的写死值。
type EmbeddingConfig struct {
	// Provider 是厂商标识，目前支持 ollama。
	Provider string `yaml:"provider"`
	// BaseURL 是向量化服务的地址。
	BaseURL string `yaml:"base_url"`
	// Model 是模型名，例如 bge-m3:latest。
	Model string `yaml:"model"`
	// BatchSize 是单次请求的文本条数，<=0 时用实现内的默认值。
	BatchSize int `yaml:"batch_size"`
}

// RetrievalConfig 描述检索的默认参数。
// 这里的值是**兜底默认**：请求未指定 top_k 时生效，请求指定时以请求为准（上限见 model.MaxRetrievalTopK）。
type RetrievalConfig struct {
	TopK int `yaml:"top_k"`
	// CandidateTopK 是重排前的向量召回条数，必须 >= TopK：
	// 召回比最终结果还少，重排就无从选起。
	CandidateTopK int `yaml:"candidate_top_k"`
	// RerankMode 控制重排策略，三档：
	//   auto（默认）—— 配了重排模型就用；没配或**调用失败**都降级为向量顺序，
	//                  并在响应的 rerankSkipped 里写明原因；
	//   off         —— 即使配了也不用（用于对比两阶段检索的效果差异）；
	//   strict      —— 配了就必须成功，调用失败即报错（宁可检索失败也要保证精排质量）。
	//
	// 默认取 auto 是面向开源后的实际部署：重排是**可选组件**（要额外跑一个服务），
	// 它挂了不该把整个检索拖下水；同时降级必须可见（响应字段 + 日志），不能静默。
	RerankMode string `yaml:"rerank"`
}

// RerankConfig 描述重排模型（TEI 边车）的兜底配置。
type RerankConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	Model    string `yaml:"model"`
}

// 重排模式取值。
const (
	// RerankModeAuto 表示「配置了就用」。
	RerankModeAuto = "auto"
	// RerankModeOff 表示显式关闭。
	RerankModeOff = "off"
	// RerankModeStrict 表示重排失败即检索失败。
	RerankModeStrict = "strict"
)

// RerankEnabled 判断默认是否执行重排。
//
// 只决定**默认策略**，请求里可以显式覆盖（见 service.SearchInput.Rerank），
// 这样才能在页面上直接对比「开重排 / 关重排」的效果。
func (r RetrievalConfig) RerankEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(r.RerankMode), RerankModeOff)
}

// RerankStrict 判断重排失败时是否直接让检索失败。
func (r RetrievalConfig) RerankStrict() bool {
	return strings.EqualFold(strings.TrimSpace(r.RerankMode), RerankModeStrict)
}

// SecurityConfig 描述安全相关配置。
type SecurityConfig struct {
	// SecretKey 是模型凭证的加密密钥（AES-256-GCM，32 字节）。
	//
	// 只允许通过环境变量 EINOFLOW_SECRET_KEY 注入：写进配置文件等于把密钥写进仓库。
	// 生成方式见 .env.example（openssl rand -base64 32）。
	// **换了密钥，已存的凭证就解不开了**，需要重新填写——这是刻意的，不做自动降级。
	SecretKey string `yaml:"secret_key"`
}

// MinIOConfig 描述对象存储配置。
type MinIOConfig struct {
	Endpoint  string `yaml:"endpoint"` // 形如 127.0.0.1:9000
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
}

// MinerUConfig 描述文档解析服务（MinerU）配置。
type MinerUConfig struct {
	// BaseURL 是 MinerU V1 API 地址，例如 http://127.0.0.1:8000。
	BaseURL string `yaml:"base_url"`
	// Token 可选：服务端开启鉴权时必填。
	Token string `yaml:"token"`
	// Tier 是默认解析档位：flash / basic / standard / advanced。
	Tier string `yaml:"tier"`
	// Timeout 是单个文档解析的等待上限。扫描件慢，给得比较宽松。
	Timeout Duration `yaml:"timeout"`
	// ChunkMaxRunes / ChunkMinRunes 是切片长度参数，留 0 时由切片器取自己的默认值。
	ChunkMaxRunes int `yaml:"chunk_max_runes"`
	ChunkMinRunes int `yaml:"chunk_min_runes"`
}

const (
	defaultServerPort    = 8888
	defaultPostgresPort  = 5432
	defaultRedisPort     = 6379
	defaultMilvusAddress = "127.0.0.1:19530"
	defaultMinIOEndpoint = "127.0.0.1:9000"
	defaultMinIOBucket   = "einoflow"

	// 分段向量集合名。改名等于换一个库，旧集合不会自动清理，因此不要在部署中途随意改。
	defaultMilvusCollection = "einoflow_segments"

	// Embedding 默认指向本机 Ollama + bge-m3（1024 维），理由见 docs/PROGRESS.md D5。
	defaultEmbeddingProvider  = "ollama"
	defaultEmbeddingBaseURL   = "http://127.0.0.1:11434"
	defaultEmbeddingModel     = "bge-m3:latest"
	defaultEmbeddingBatchSize = 16

	// 检索参数的默认值与上限来自领域定义，避免与请求校验各写一份。
	defaultRetrievalTopK = model.DefaultRetrievalTopK
	// 召回条数默认取最终条数的 4 倍：给重排留挑选空间，又不至于把大量无关内容拉进来。
	defaultRetrievalCandidateTopK = 20

	// 重排默认指向本机 TEI 边车（docker compose --profile rerank up -d tei）。
	defaultRerankProvider = "tei"
	defaultRerankBaseURL  = "http://127.0.0.1:8080"

	defaultPostgresSSLMode         = "disable"
	defaultPostgresMaxOpenConns    = 20
	defaultPostgresMaxIdleConns    = 5
	defaultPostgresConnMaxLifetime = time.Hour

	defaultMinerUBaseURL = "http://127.0.0.1:8000"
	defaultMinerUTier    = "basic"
	defaultMinerUTimeout = 15 * time.Minute
)

// Load 读取配置。路径优先级为：显式入参 > 环境变量 EINOFLOW_CONFIG > 默认 configs/config.yaml。
// 读取后会补默认值、应用环境变量覆盖并做基础校验。
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(envConfigPath)
	}
	if path == "" {
		path = defaultConfigPath
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}

	cfg.applyDefaults()
	cfg.applyEnvOverrides()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("校验配置失败: %w", err)
	}
	return &cfg, nil
}

// applyDefaults 为未显式配置的字段填充默认值。
func (c *Config) applyDefaults() {
	if c.App.Name == "" {
		c.App.Name = "einoflow"
	}
	if c.App.Env == "" {
		c.App.Env = "dev"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = defaultServerPort
	}
	if c.Postgres.Port == 0 {
		c.Postgres.Port = defaultPostgresPort
	}
	if c.Postgres.SSLMode == "" {
		c.Postgres.SSLMode = defaultPostgresSSLMode
	}
	if c.Redis.Port == 0 {
		c.Redis.Port = defaultRedisPort
	}
	if c.Milvus.Address == "" {
		c.Milvus.Address = defaultMilvusAddress
	}
	if c.Milvus.Database == "" {
		c.Milvus.Database = c.App.Name
	}
	if c.Milvus.Collection == "" {
		c.Milvus.Collection = defaultMilvusCollection
	}
	if c.Embedding.Provider == "" {
		c.Embedding.Provider = defaultEmbeddingProvider
	}
	if c.Embedding.BaseURL == "" {
		c.Embedding.BaseURL = defaultEmbeddingBaseURL
	}
	if c.Embedding.Model == "" {
		c.Embedding.Model = defaultEmbeddingModel
	}
	if c.Embedding.BatchSize == 0 {
		c.Embedding.BatchSize = defaultEmbeddingBatchSize
	}
	if c.Retrieval.TopK == 0 {
		c.Retrieval.TopK = defaultRetrievalTopK
	}
	if c.Retrieval.CandidateTopK == 0 {
		c.Retrieval.CandidateTopK = defaultRetrievalCandidateTopK
	}
	if c.Retrieval.RerankMode == "" {
		c.Retrieval.RerankMode = RerankModeAuto
	}
	if c.Rerank.Provider == "" {
		c.Rerank.Provider = defaultRerankProvider
	}
	if c.Rerank.BaseURL == "" {
		c.Rerank.BaseURL = defaultRerankBaseURL
	}
	if c.MinIO.Endpoint == "" {
		c.MinIO.Endpoint = defaultMinIOEndpoint
	}
	if c.MinIO.Bucket == "" {
		c.MinIO.Bucket = defaultMinIOBucket
	}
	if c.Postgres.MaxOpenConns == 0 {
		c.Postgres.MaxOpenConns = defaultPostgresMaxOpenConns
	}
	if c.Postgres.MaxIdleConns == 0 {
		c.Postgres.MaxIdleConns = defaultPostgresMaxIdleConns
	}
	if c.Postgres.ConnMaxLifetime == 0 {
		c.Postgres.ConnMaxLifetime = Duration(defaultPostgresConnMaxLifetime)
	}
	if c.MinerU.BaseURL == "" {
		c.MinerU.BaseURL = defaultMinerUBaseURL
	}
	if c.MinerU.Tier == "" {
		c.MinerU.Tier = defaultMinerUTier
	}
	if c.MinerU.Timeout == 0 {
		c.MinerU.Timeout = Duration(defaultMinerUTimeout)
	}
}

// applyEnvOverrides 用环境变量覆盖配置，便于容器化部署与密钥注入。
func (c *Config) applyEnvOverrides() {
	if v, ok := envString("EINOFLOW_APP_ENV"); ok {
		c.App.Env = v
	}
	if v, ok := envString("EINOFLOW_SERVER_HOST"); ok {
		c.Server.Host = v
	}
	if v, ok := envInt("EINOFLOW_SERVER_PORT"); ok {
		c.Server.Port = v
	}
	if v, ok := envString("EINOFLOW_LOG_LEVEL"); ok {
		c.Log.Level = v
	}
	if v, ok := envString("EINOFLOW_POSTGRES_HOST"); ok {
		c.Postgres.Host = v
	}
	if v, ok := envInt("EINOFLOW_POSTGRES_PORT"); ok {
		c.Postgres.Port = v
	}
	if v, ok := envString("EINOFLOW_POSTGRES_USER"); ok {
		c.Postgres.User = v
	}
	if v, ok := envString("EINOFLOW_POSTGRES_PASSWORD"); ok {
		c.Postgres.Password = v
	}
	if v, ok := envString("EINOFLOW_POSTGRES_DATABASE"); ok {
		c.Postgres.Database = v
	}
	if v, ok := envString("EINOFLOW_POSTGRES_SSLMODE"); ok {
		c.Postgres.SSLMode = v
	}
	if v, ok := envBool("EINOFLOW_POSTGRES_AUTO_MIGRATE"); ok {
		c.Postgres.AutoMigrate = v
	}
	if v, ok := envString("EINOFLOW_REDIS_HOST"); ok {
		c.Redis.Host = v
	}
	if v, ok := envInt("EINOFLOW_REDIS_PORT"); ok {
		c.Redis.Port = v
	}
	if v, ok := envString("EINOFLOW_REDIS_PASSWORD"); ok {
		c.Redis.Password = v
	}
	if v, ok := envInt("EINOFLOW_REDIS_DB"); ok {
		c.Redis.DB = v
	}
	if v, ok := envString("EINOFLOW_MILVUS_ADDRESS"); ok {
		c.Milvus.Address = v
	}
	if v, ok := envString("EINOFLOW_MILVUS_USERNAME"); ok {
		c.Milvus.Username = v
	}
	if v, ok := envString("EINOFLOW_MILVUS_PASSWORD"); ok {
		c.Milvus.Password = v
	}
	if v, ok := envString("EINOFLOW_MILVUS_DATABASE"); ok {
		c.Milvus.Database = v
	}
	if v, ok := envString("EINOFLOW_MILVUS_COLLECTION"); ok {
		c.Milvus.Collection = v
	}
	if v, ok := envString("EINOFLOW_EMBEDDING_PROVIDER"); ok {
		c.Embedding.Provider = v
	}
	if v, ok := envString("EINOFLOW_EMBEDDING_BASE_URL"); ok {
		c.Embedding.BaseURL = v
	}
	if v, ok := envString("EINOFLOW_EMBEDDING_MODEL"); ok {
		c.Embedding.Model = v
	}
	if v, ok := envInt("EINOFLOW_EMBEDDING_BATCH_SIZE"); ok {
		c.Embedding.BatchSize = v
	}
	if v, ok := envInt("EINOFLOW_RETRIEVAL_TOP_K"); ok {
		c.Retrieval.TopK = v
	}
	if v, ok := envInt("EINOFLOW_RETRIEVAL_CANDIDATE_TOP_K"); ok {
		c.Retrieval.CandidateTopK = v
	}
	if v, ok := envString("EINOFLOW_RETRIEVAL_RERANK"); ok {
		c.Retrieval.RerankMode = v
	}
	if v, ok := envString("EINOFLOW_RERANK_PROVIDER"); ok {
		c.Rerank.Provider = v
	}
	if v, ok := envString("EINOFLOW_RERANK_BASE_URL"); ok {
		c.Rerank.BaseURL = v
	}
	if v, ok := envString("EINOFLOW_RERANK_MODEL"); ok {
		c.Rerank.Model = v
	}
	if v, ok := envString("EINOFLOW_SECRET_KEY"); ok {
		c.Security.SecretKey = v
	}
	if v, ok := envString("EINOFLOW_MINIO_ENDPOINT"); ok {
		c.MinIO.Endpoint = v
	}
	if v, ok := envString("EINOFLOW_MINIO_ACCESS_KEY"); ok {
		c.MinIO.AccessKey = v
	}
	if v, ok := envString("EINOFLOW_MINIO_SECRET_KEY"); ok {
		c.MinIO.SecretKey = v
	}
	if v, ok := envString("EINOFLOW_MINIO_BUCKET"); ok {
		c.MinIO.Bucket = v
	}
	if v, ok := envBool("EINOFLOW_MINIO_USE_SSL"); ok {
		c.MinIO.UseSSL = v
	}
	if v, ok := envString("EINOFLOW_MINERU_BASE_URL"); ok {
		c.MinerU.BaseURL = v
	}
	if v, ok := envString("EINOFLOW_MINERU_TOKEN"); ok {
		c.MinerU.Token = v
	}
	if v, ok := envString("EINOFLOW_MINERU_TIER"); ok {
		c.MinerU.Tier = v
	}
}

// validate 做基础合法性校验，尽早暴露配置错误。
func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 不合法: %d", c.Server.Port)
	}
	if c.Postgres.Host == "" {
		return fmt.Errorf("postgres.host 不能为空")
	}
	if c.Postgres.User == "" {
		return fmt.Errorf("postgres.user 不能为空")
	}
	if c.Postgres.Database == "" {
		return fmt.Errorf("postgres.database 不能为空")
	}
	if c.Milvus.Address == "" {
		return fmt.Errorf("milvus.address 不能为空")
	}
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("minio.endpoint 不能为空")
	}
	if c.MinerU.BaseURL == "" {
		return fmt.Errorf("mineru.base_url 不能为空")
	}
	if c.Milvus.Collection == "" {
		return fmt.Errorf("milvus.collection 不能为空")
	}
	if c.Embedding.Model == "" {
		return fmt.Errorf("embedding.model 不能为空")
	}
	if c.Embedding.BaseURL == "" {
		return fmt.Errorf("embedding.base_url 不能为空")
	}
	if c.Retrieval.TopK <= 0 || c.Retrieval.TopK > model.MaxRetrievalTopK {
		return fmt.Errorf("retrieval.top_k 必须在 1..%d 之间，当前为 %d",
			model.MaxRetrievalTopK, c.Retrieval.TopK)
	}
	if c.Retrieval.CandidateTopK < c.Retrieval.TopK {
		// 召回数小于最终条数是自相矛盾的配置，直接拒绝而不是静默放大：
		// 静默放大意味着使用者以为「只召回 3 条」，实际跑的却是别的值。
		return fmt.Errorf("retrieval.candidate_top_k（%d）不能小于 retrieval.top_k（%d）",
			c.Retrieval.CandidateTopK, c.Retrieval.TopK)
	}
	if c.Retrieval.CandidateTopK > model.MaxCandidateTopK {
		return fmt.Errorf("retrieval.candidate_top_k 不能超过 %d，当前为 %d",
			model.MaxCandidateTopK, c.Retrieval.CandidateTopK)
	}
	switch strings.ToLower(strings.TrimSpace(c.Retrieval.RerankMode)) {
	case RerankModeAuto, RerankModeOff, RerankModeStrict:
	default:
		return fmt.Errorf("retrieval.rerank 只支持 %s / %s / %s，当前为 %q",
			RerankModeAuto, RerankModeOff, RerankModeStrict, c.Retrieval.RerankMode)
	}
	if c.Security.SecretKey == "" {
		return fmt.Errorf(
			"未配置 security.secret_key（模型凭证的加密密钥）；" +
				"请生成一个并写入 .env：openssl rand -base64 32")
	}
	return nil
}

// envString 读取非空环境变量。
func envString(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// envInt 读取整数环境变量，解析失败时忽略该覆盖。
func envInt(key string) (int, bool) {
	value, ok := envString(key)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// envBool 读取布尔环境变量，解析失败时忽略该覆盖。
func envBool(key string) (bool, bool) {
	value, ok := envString(key)
	if !ok {
		return false, false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return parsed, true
}

// Duration 让 YAML 配置可以写 "1h"、"30m" 这类时长，解析后等价于 time.Duration。
type Duration time.Duration

// UnmarshalYAML 实现 yaml.Unmarshaler。
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("解析时长 %q 失败: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std 返回标准库的 time.Duration。
func (d Duration) Std() time.Duration { return time.Duration(d) }
