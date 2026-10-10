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
