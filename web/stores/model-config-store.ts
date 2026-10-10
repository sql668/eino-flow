"use client";

import { create } from "zustand";

import {
  modelApi,
  type BindModelPayload,
  type CreateModelPayload,
  type SaveProviderPayload,
  type TestProviderPayload,
} from "@/lib/api/models";
import type {
  EffectiveModelInfo,
  ModelBindingInfo,
  ModelInstanceInfo,
  ModelKind,
  ModelProviderInfo,
  ProbeResultInfo,
} from "@/types/api";

// 模型配置页的状态。
//
// 按项目约定（web/AGENTS.md），跨组件共享的状态统一放 Zustand：
// 厂商卡片、模型列表、生效模型横幅都要读同一份数据，放组件内部会立刻需要层层传参。
//
// 约定：
//   * 所有网络调用都收敛在这里，组件只调 action、不直接发请求；
//   * 错误统一进 error 字段，由页面顶部展示，避免每个组件各写一套提示；
//   * 写入类操作成功后重新拉取列表（配置项很少，简单可靠优先）。

interface ModelConfigState {
  providers: ModelProviderInfo[];
  models: ModelInstanceInfo[];
  bindings: ModelBindingInfo[];
  /** 当前生效的向量化模型 */
  effective: EffectiveModelInfo | null;
  /** 当前生效的重排模型（未配置时为 null） */
  effectiveRerank: EffectiveModelInfo | null;
  /** 当前生效的对话模型（未配置时为 null）；调用逻辑尚未接入，但配置状态要如实显示 */
  effectiveLLM: EffectiveModelInfo | null;
  /** 首次加载中 */
  loading: boolean;
  /** 正在进行的操作标识（如 "provider:ollama"），用于按钮禁用 */
  pending: string;
  error: string;
  /** 最近一次操作的成功提示 */
  notice: string;

  loadAll: () => Promise<void>;
  saveProvider: (provider: string, payload: SaveProviderPayload) => Promise<boolean>;
  deleteProvider: (provider: string) => Promise<boolean>;
  testProvider: (provider: string, payload: TestProviderPayload) => Promise<ProbeResultInfo | null>;
  createModel: (payload: CreateModelPayload) => Promise<boolean>;
  setModelEnabled: (id: number, enabled: boolean) => Promise<boolean>;
  deleteModel: (id: number) => Promise<boolean>;
  testModel: (id: number) => Promise<ProbeResultInfo | null>;
  /** 设为全局默认；embedding 与 rerank 共用同一条绑定接口 */
  bindGlobal: (modelType: ModelKind, instanceId: number) => Promise<boolean>;
  unbind: (modelType: ModelKind) => Promise<boolean>;
  clearMessages: () => void;
}

/** describeError 把异常转成可读文案。 */
function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "未知错误";
}

export const useModelConfigStore = create<ModelConfigState>((set, get) => ({
  providers: [],
  models: [],
  bindings: [],
  effective: null,
  effectiveRerank: null,
  effectiveLLM: null,
  loading: false,
  pending: "",
  error: "",
  notice: "",

  loadAll: async () => {
    set({ loading: true, error: "" });
    try {
      // 查询互相独立，并发发出，减少首屏等待。
      // effective 分开取 embedding / rerank：两者可以由不同厂商提供，来源也可能不同。
      const [providers, models, bindings, effective, effectiveRerank, effectiveLLM] = await Promise.all([
        modelApi.listProviders(),
        modelApi.listModels(),
        modelApi.listBindings("global"),
        modelApi.effective("embedding"),
        // 未配置时接口返回 ErrNotConfigured，这属于正常状态（不是故障），捕获后按未配置展示。
        modelApi.effective("rerank").catch(() => null),
        modelApi.effective("llm").catch(() => null),
      ]);
      set({ providers, models, bindings, effective, effectiveRerank, effectiveLLM, loading: false });
    } catch (error) {
      set({ loading: false, error: describeError(error) });
    }
  },

  saveProvider: async (provider, payload) => {
    set({ pending: `provider:${provider}`, error: "", notice: "" });
    try {
      await modelApi.saveProvider(provider, payload);
      set({ notice: `厂商 ${provider} 配置已保存` });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  deleteProvider: async (provider) => {
    set({ pending: `provider:${provider}`, error: "", notice: "" });
    try {
      await modelApi.deleteProvider(provider);
      set({ notice: `厂商 ${provider} 配置已删除` });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  testProvider: async (provider, payload) => {
    set({ pending: `provider:${provider}`, error: "" });
    try {
      return await modelApi.testProvider(provider, payload);
    } catch (error) {
      // 探测失败是预期内的（地址写错、密钥无效），用 error 展示而不是抛出去。
      set({ error: describeError(error) });
      return null;
    } finally {
      set({ pending: "" });
    }
  },

  createModel: async (payload) => {
    set({ pending: "model:create", error: "", notice: "" });
    try {
      await modelApi.createModel(payload);
      set({ notice: `模型 ${payload.modelName} 已添加` });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  setModelEnabled: async (id, enabled) => {
    set({ pending: `model:${id}`, error: "", notice: "" });
    try {
      await modelApi.updateModel(id, { enabled });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  deleteModel: async (id) => {
    set({ pending: `model:${id}`, error: "", notice: "" });
    try {
      await modelApi.deleteModel(id);
      set({ notice: "模型已删除" });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  testModel: async (id) => {
    set({ pending: `model:${id}`, error: "" });
    try {
      return await modelApi.testModel(id);
    } catch (error) {
      set({ error: describeError(error) });
      return null;
    } finally {
      set({ pending: "" });
    }
  },

  bindGlobal: async (modelType, instanceId) => {
    const payload: BindModelPayload = { scope: "global", modelType, modelInstanceId: instanceId };
    set({ pending: `bind:${modelType}`, error: "", notice: "" });
    try {
      await modelApi.bind(payload);
      set({ notice: "已设为全局默认模型" });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  unbind: async (modelType) => {
    set({ pending: `bind:${modelType}`, error: "", notice: "" });
    try {
      await modelApi.unbind("global", modelType);
      set({ notice: "已取消全局默认，回落到配置兜底" });
      await get().loadAll();
      return true;
    } catch (error) {
      set({ error: describeError(error) });
      return false;
    } finally {
      set({ pending: "" });
    }
  },

  clearMessages: () => set({ error: "", notice: "" }),
}));
