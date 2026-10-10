// Package config 负责加载并校验应用配置。
//
// 约定（见根 AGENTS.md / backend/AGENTS.md）：
//   - 所有可变项统一从配置文件 + 环境变量读取，业务代码禁止硬编码；
//   - 密钥类字段只通过环境变量注入，不写入仓库。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// envConfigPath 是指定配置文件路径的环境变量名。
	envConfigPath = "EINOFLOW_CONFIG"
	// defaultConfigPath 是默认配置文件路径（相对 backend/ 目录）。
	defaultConfigPath = "configs/config.yaml"
)

// Config 是应用顶层配置，结构对应 configs/config.yaml。
type Config struct {
	App    AppConfig    `yaml:"app"`
	Server ServerConfig `yaml:"server"`
	Log    LogConfig    `yaml:"log"`
	MySQL  MySQLConfig  `yaml:"mysql"`
	Redis  RedisConfig  `yaml:"redis"`
	Milvus MilvusConfig `yaml:"milvus"`
	MinIO  MinIOConfig  `yaml:"minio"`
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

// MySQLConfig 描述业务数据库连接配置。
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	Charset  string `yaml:"charset"`
}

// DSN 返回 database/sql 所需的数据源名称。
func (m MySQLConfig) DSN() string {
	charset := m.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.Database, charset,
	)
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
}

// MinIOConfig 描述对象存储配置。
type MinIOConfig struct {
	Endpoint  string `yaml:"endpoint"` // 形如 127.0.0.1:9000
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
}

const (
	defaultServerPort    = 8888
	defaultMySQLPort     = 3306
	defaultRedisPort     = 6379
	defaultMilvusAddress = "127.0.0.1:19530"
	defaultMinIOEndpoint = "127.0.0.1:9000"
	defaultMinIOBucket   = "einoflow"
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
	if c.MySQL.Charset == "" {
		c.MySQL.Charset = "utf8mb4"
	}
	if c.MySQL.Port == 0 {
		c.MySQL.Port = defaultMySQLPort
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
	if c.MinIO.Endpoint == "" {
		c.MinIO.Endpoint = defaultMinIOEndpoint
	}
	if c.MinIO.Bucket == "" {
		c.MinIO.Bucket = defaultMinIOBucket
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
	if v, ok := envString("EINOFLOW_MYSQL_HOST"); ok {
		c.MySQL.Host = v
	}
	if v, ok := envInt("EINOFLOW_MYSQL_PORT"); ok {
		c.MySQL.Port = v
	}
	if v, ok := envString("EINOFLOW_MYSQL_USER"); ok {
		c.MySQL.User = v
	}
	if v, ok := envString("EINOFLOW_MYSQL_PASSWORD"); ok {
		c.MySQL.Password = v
	}
	if v, ok := envString("EINOFLOW_MYSQL_DATABASE"); ok {
		c.MySQL.Database = v
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
}

// validate 做基础合法性校验，尽早暴露配置错误。
func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 不合法: %d", c.Server.Port)
	}
	if c.MySQL.Host == "" {
		return fmt.Errorf("mysql.host 不能为空")
	}
	if c.MySQL.User == "" {
		return fmt.Errorf("mysql.user 不能为空")
	}
	if c.MySQL.Database == "" {
		return fmt.Errorf("mysql.database 不能为空")
	}
	if c.Milvus.Address == "" {
		return fmt.Errorf("milvus.address 不能为空")
	}
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("minio.endpoint 不能为空")
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
