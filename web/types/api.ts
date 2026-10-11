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
/** 检索方式：向量 / 全文 / 混合（与后端 model.SearchMode 一致）。 */
export type SearchMode = "vector" | "fulltext" | "hybrid";

/** 混合检索两路结果的融合方式（与后端 model.HybridRank 一致）。 */
export type HybridRank = "weighted" | "rrf";

export interface SearchHit {
  segmentId: number;
  documentId: number;
  /** 命中所属文档名，用于展示「出自哪份文档」 */
  documentName: string;
  /** 本次检索内的排名，从 1 开始 */
  rank: number;
  /** 决定本次排名的分数（单路检索是该路分数，混合检索是融合分） */
  score: number;
  /** 向量路原始分；该路未参与本次检索时为空 */
  vectorScore?: number;
  /** 全文（BM25）路原始分；该路未参与本次检索时为空 */
  fullTextScore?: number;
  /** 重排分数；未做重排时为空 */
  rerankScore?: number;
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
  /** 本次实际使用的检索方式 */
  mode: SearchMode;
  /** 重排模型；为空表示未做重排 */
  rerankModel: string;
  /** 未做重排的原因（未配置 / 已被关闭 / 服务不可用） */
  rerankSkipped?: string;
  /** 重排前的召回条数 */
  recalled: number;
  /** 被相似度阈值筛掉的条数（仅向量检索模式会筛） */
  filtered: number;
  /** 本次使用的相似度阈值 */
  threshold: number;
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
  /** 一句话说明该厂商的用途 */
  description: string;
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

/*
 * 应用 / 知识库 / 文档 / 会话契约。
 * 对应后端 handler/app.go、dataset.go、document.go、conversation.go、chat.go，
 * 字段名与后端 JSON 标签一一对应。
 */

/** 应用形态。 */
export type AppMode = "chat" | "rag" | "agent" | "workflow";

export interface AppInfo {
  id: number;
  name: string;
  description: string;
  mode: AppMode;
  status: string;
  icon: string;
  createdAt: string;
  updatedAt: string;
}

export interface AppListResult {
  items: AppInfo[];
  total: number;
  page: number;
  pageSize: number;
}

export interface DatasetInfo {
  id: number;
  name: string;
  description: string;
  embeddingModel: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface DatasetListResult {
  items: DatasetInfo[];
  total: number;
  page: number;
  pageSize: number;
}

/**
 * 文档处理状态。
 * pending/indexing 期间前端需要轮询，ready 才可检索，failed 要看 errorMessage。
 */
export type DocumentStatus = "pending" | "parsing" | "draft" | "indexing" | "ready" | "failed";

export interface DocumentInfo {
  id: number;
  datasetId: number;
  name: string;
  fileSize: number;
  status: DocumentStatus;
  /** 解析出的正文字符数（清洗前） */
  charCount: number;
  /** 最终采用的分段模式；未入库时为空 */
  chunkMode: string;
  segmentCount: number;
  /** 解析产物是否就绪（决定能否进分段工作台预览） */
  parsed: boolean;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DocumentListResult {
  items: DocumentInfo[];
  total: number;
  page: number;
  pageSize: number;
}

export interface SegmentInfo {
  id: number;
  /** 片段主键（与向量库对应），引用留痕与按片段复查都用它 */
  chunkId: string;
  chunkIndex: number;
  /** 片段字符数（不含标题面包屑） */
  charCount: number;
  /** 开头与前一片段重叠的字符数 */
  overlapRunes: number;
  content: string;
  tokenCount: number;
  page: number;
  locator: string;
  kind: string;
  titlePath: string[];
  /** 分段关键词（自动生成、可人工修改；同时是全文检索的输入之一） */
  keywords: string[];
  createdAt: string;
}

export interface SegmentListResult {
  items: SegmentInfo[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ConversationInfo {
  id: number;
  appId: number;
  title: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConversationListResult {
  items: ConversationInfo[];
  total: number;
  page: number;
  pageSize: number;
}

/** 助手消息引用的片段（与后端 model.MessageCitation 对应）。 */
export interface CitationInfo {
  /** 回答里的引用编号，从 1 开始 */
  index: number;
  segmentId: number;
  documentId: number;
  documentName: string;
  page: number;
  titlePath: string[];
  score: number;
  rerankScore?: number;
}

export interface MessageInfo {
  id: number;
  conversationId: number;
  role: "system" | "user" | "assistant" | "tool";
  content: string;
  citations?: CitationInfo[];
  tokenCount: number;
  latencyMs: number;
  createdAt: string;
}

export interface MessageListResult {
  items: MessageInfo[];
  total: number;
  page: number;
  pageSize: number;
}

/** SSE 事件负载：citations → delta（多次）→ done。 */
export interface ChatCitationsPayload {
  citations: CitationInfo[];
}

export interface ChatDeltaPayload {
  content: string;
}

/** 推理模型的思考过程增量（qwen3 等会先产出一段 thinking）。 */
export interface ChatReasoningPayload {
  content: string;
}

export interface ChatDonePayload {
  conversationId: number;
  messageId: number;
  model: string;
  latencyMs: number;
}

export interface ChatErrorPayload {
  message: string;
}

/*
 * 分段工作台契约。
 * 对应后端 handler/chunkworkbench.go，字段名与 JSON 标签一一对应。
 */

/** 分段模式。parent_child 目前是占位（后端会拒绝提交）。 */
export type ChunkMode = "title" | "length" | "separator" | "parent_child";

export interface ChunkModeOption {
  mode: ChunkMode;
  label: string;
  description: string;
  implemented: boolean;
}

export interface CleaningRuleInfo {
  key: string;
  label: string;
  description: string;
  /** 需要在界面上填正则 */
  needsPattern?: boolean;
  /** high 表示容易误删，界面会额外提醒 */
  risk?: string;
}

export interface CleaningRuleConfig {
  key: string;
  pattern?: string;
}

/** 关键词抽取配置（入库时自动生成，可人工修改）。 */
export interface KeywordConfig {
  enabled: boolean;
  /** 单段关键词数量上限 */
  maxCount: number;
}

/** 分段与清洗配置。 */
export interface ChunkConfig {
  mode: ChunkMode;
  /**
   * 自定义分隔符列表（仅 separator 模式使用）。
   * 界面上按「英文逗号分隔」输入，字面逗号写作 `\,`（见 components/chunk/separator-field.tsx）。
   */
  separators: string[];
  maxRunes: number;
  overlapRunes: number;
  minRunes: number;
  keepNoise: boolean;
  cleaning: CleaningRuleConfig[];
  /** 关键词抽取配置；不传时后端按「开启、最多 5 个」处理 */
  keywords?: KeywordConfig;
  /** 配置指纹（后端计算），用于识别「这批片段是哪套参数切的」 */
  version?: string;
}

export interface ChunkLimits {
  minMaxRunes: number;
  maxMaxRunes: number;
  maxMinRunes: number;
  defaultMaxRunes: number;
  defaultMinRunes: number;
  maxOverlapPercent: number;
}

export interface ChunkOptions {
  modes: ChunkModeOption[];
  cleaningRules: CleaningRuleInfo[];
  defaults: ChunkConfig;
  limits: ChunkLimits;
}

/** 单条清洗规则的执行结果。 */
export interface CleaningRuleResult {
  key: string;
  label: string;
  hits: number;
  removedRunes: number;
  note?: string;
  /** 被删除内容的样本，界面据此判断规则是否误伤 */
  samples?: string[];
}

export interface PreviewChunk {
  index: number;
  kind: string;
  titlePath: string[];
  page: number;
  locator: string;
  charCount: number;
  overlapRunes: number;
  /** 最终内容（含标题面包屑） */
  content: string;
  /** 清洗前的内容，用于对比 */
  originalText: string;
}

export interface ChunkStats {
  total: number;
  totalRunes: number;
  avgRunes: number;
  maxChunkRunes: number;
  atLimit: number;
  tooShort: number;
  tables: number;
  overlapRunes: number;
  droppedNoise: number;
}

export interface PreviewResult {
  chunks: PreviewChunk[];
  total: number;
  returned: number;
  cleaning: CleaningRuleResult[];
  cleaningStats: { removedRunes: number; beforeRunes: number; afterRunes: number };
  stats: ChunkStats;
  /** 可直接执行的建议（例如「8 个片段贴着上限被切开」） */
  hints: string[];
  configVersion: string;
  meta: { parser: string; parseMode: string; pageCount: number; sourceName: string };
}

/** 知识库级检索配置。 */
export interface RetrievalConfig {
  /** 检索方式：向量 / 全文 / 混合 */
  searchMode: SearchMode;
  /** 混合检索的融合方式；仅 searchMode=hybrid 时有意义 */
  hybridRank: HybridRank;
  topK: number;
  candidateTopK: number;
  scoreThreshold: number;
  rerank: boolean;
  vectorWeight: number;
  keywordWeight: number;
}
