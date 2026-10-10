import { http } from "@/lib/http";
import type {
  EffectiveModelInfo,
  ModelBindingInfo,
  ModelInstanceInfo,
  ModelKind,
  ModelProviderInfo,
  ModelScope,
  ProbeResultInfo,
} from "@/types/api";

// 模型配置的接口层。
//
// 存在的意义：把「路径 + 出入参类型」收敛到一处，组件与 store 不直接拼路径，
// 后端改路由时只改这个文件。所有请求都必须走 lib/http 的统一封装（见 web/AGENTS.md）。

const base = "/api/v1/model";

export interface SaveProviderPayload {
  displayName?: string;
  baseUrl?: string;
  /** 传 undefined 表示不改动已存凭证；传 {} 表示清空 */
  credentials?: Record<string, string>;
  enabled?: boolean;
}

export interface TestProviderPayload {
  baseUrl?: string;
  modelName?: string;
  /** 支持带未保存的凭证先测试 */
  credentials?: Record<string, string>;
}

export interface CreateModelPayload {
  provider: string;
  modelName: string;
  modelType: ModelKind;
  displayName?: string;
  enabled?: boolean;
}

export interface UpdateModelPayload {
  displayName?: string;
  enabled?: boolean;
}

export interface BindModelPayload {
  scope: ModelScope;
  appId?: number;
  datasetId?: number;
  modelType: ModelKind;
  modelInstanceId: number;
}

export const modelApi = {
  listProviders: () => http.get<ModelProviderInfo[]>(`${base}/providers`),

  saveProvider: (provider: string, payload: SaveProviderPayload) =>
    http.put<ModelProviderInfo>(`${base}/providers/${provider}`, payload),

  deleteProvider: (provider: string) => http.delete<null>(`${base}/providers/${provider}`),

  testProvider: (provider: string, payload: TestProviderPayload) =>
    http.post<ProbeResultInfo>(`${base}/providers/${provider}/test`, payload),

  listModels: (kind?: ModelKind) =>
    http.get<ModelInstanceInfo[]>(`${base}/instances`, {
      query: kind ? { type: kind } : undefined,
    }),

  createModel: (payload: CreateModelPayload) =>
    http.post<ModelInstanceInfo>(`${base}/instances`, payload),

  updateModel: (id: number, payload: UpdateModelPayload) =>
    http.put<ModelInstanceInfo>(`${base}/instances/${id}`, payload),

  deleteModel: (id: number) => http.delete<null>(`${base}/instances/${id}`),

  testModel: (id: number) => http.post<ProbeResultInfo>(`${base}/instances/${id}/test`),

  listBindings: (scope: ModelScope, appId?: number, datasetId?: number) =>
    http.get<ModelBindingInfo[]>(`${base}/bindings`, {
      query: { scope, appId, datasetId },
    }),

  bind: (payload: BindModelPayload) => http.put<null>(`${base}/bindings`, payload),

  unbind: (scope: ModelScope, modelType: ModelKind, appId?: number, datasetId?: number) =>
    http.delete<null>(`${base}/bindings`, {
      query: { scope, modelType, appId, datasetId },
    }),

  effective: (appId?: number, datasetId?: number) =>
    http.get<EffectiveModelInfo>(`${base}/effective`, {
      query: { appId, datasetId },
    }),
};
