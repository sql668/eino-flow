// 前端接口契约类型。
// 与后端 backend/internal/handler/response.go 中的 Response 保持一一对应，
// 改动必须两边同步（见根 AGENTS.md 第 7 节「DSL 契约」）。

/** 后端统一响应结构。 */
export interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

/** 业务响应码，与后端 handler 包中的常量保持一致。 */
export const ApiCode = {
  OK: 0,
  InvalidParam: 40000,
  Unauthorized: 40100,
  NotFound: 40400,
  Internal: 50000,
} as const;

export type ApiCodeValue = (typeof ApiCode)[keyof typeof ApiCode];

/*
 * 知识库检索契约。
 * 对应后端 handler/search.go，字段名与后端 JSON 标签一一对应，改动必须两边同步。
 * 应用 / 知识库 / 文档等其余实体的类型等对应页面落地时再补，避免现在写一堆没人用的定义。
 */

/** 检索请求。 */
export interface SearchRequest {
  /** 检索内容 */
  query: string;
  /** 召回条数；不传时由后端取配置默认值 */
  topK?: number;
}

/** 单个命中片段，带溯源信息。 */
export interface SearchHit {
  segmentId: number;
  documentId: number;
  /** 命中所属文档名，用于展示「出自哪份文档」 */
  documentName: string;
  /** 本次检索内的排名，从 1 开始 */
  rank: number;
  /** 向量相似度（余弦，越大越相似） */
  score: number;
  content: string;
  chunkIndex: number;
  /** 分段起始页码，从 1 开始 */
  page: number;
  /** 分段起始块定位串，形如 page:3/block:12 */
  locator: string;
  /** 分段主要类型：text / table */
  kind: string;
  /** 所属标题路径，用于展示引用来源 */
  titlePath: string[];
}

/** 检索结果。 */
export interface SearchResult {
  /** 本次检索的留痕记录 ID，便于事后复查 */
  runId: number;
  query: string;
  topK: number;
  /** 本次使用的向量模型 */
  embeddingModel: string;
  latencyMs: number;
  hits: SearchHit[];
}

/*
 * 模型配置契约。
 * 对应后端 handler/modelconfig.go，字段名与后端 JSON 标签一一对应。
 * 安全约定：凭证只会以掩码形式返回，前端拿不到也不会存明文。
 */

/** 模型能力类型。 */
export type ModelKind = "llm" | "embedding" | "rerank";

/** 模型绑定的作用域，优先级：dataset > app > global。 */
export type ModelScope = "global" | "app" | "dataset";

/** 厂商声明的凭证字段，用于动态渲染表单。 */
export interface CredentialField {
  key: string;
  label: string;
  required: boolean;
  /** 为 true 时用密码框渲染 */
  secret: boolean;
  placeholder?: string;
}

/** 内置厂商及其本地配置状态。 */
export interface ModelProviderInfo {
  provider: string;
  displayName: string;
  kinds: ModelKind[];
  defaultBaseUrl: string;
  /** 用户配置的地址；为空表示用默认值 */
  baseUrl: string;
  credentialFields: CredentialField[];
  /** 已存凭证的掩码；为空表示未配置 */
  credentialMask: string;
  configured: boolean;
  enabled: boolean;
  updatedAt?: string;
}

/** 一个可用模型。 */
export interface ModelInstanceInfo {
  id: number;
  provider: string;
  providerName: string;
  modelName: string;
  modelType: ModelKind;
  displayName: string;
  enabled: boolean;
  /** 指向该模型的绑定数量，删除前的风险提示 */
  bindingCount: number;
  createdAt: string;
  updatedAt: string;
}

/** 一条模型绑定。 */
export interface ModelBindingInfo {
  scope: ModelScope;
  appId: number;
  datasetId: number;
  modelType: ModelKind;
  instanceId: number;
  modelName: string;
  provider: string;
  providerName: string;
  enabled: boolean;
}

/** 连通性 / 可用性测试结果。 */
export interface ProbeResultInfo {
  ok: boolean;
  message: string;
  models: string[];
  modelAvailable: boolean;
  latencyMs: number;
}

/** 当前生效的模型（配置页最该看到的信息）。 */
export interface EffectiveModelInfo {
  /** 为 0 表示来自配置文件兜底（数据库里没有任何绑定） */
  instanceId: number;
  provider: string;
  providerName: string;
  modelName: string;
  modelType: ModelKind | "";
  baseUrl: string;
  dimension: number;
  scope: ModelScope | "";
  fromConfig: boolean;
}
