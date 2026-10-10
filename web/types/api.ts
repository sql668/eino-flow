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
