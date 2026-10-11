"use client";

import { create } from "zustand";

import { chunkApi } from "@/lib/api/console";
import type {
  ChunkConfig,
  ChunkOptions,
  CleaningRuleConfig,
  PreviewResult,
} from "@/types/api";

// 分段工作台的状态。
//
// 放 Zustand 而不是页面内 useState：左侧配置面板与右侧预览面板是并列组件，
// 共享同一份配置与预览结果，用 props 传递会变成层层透传。
//
// 关键交互：**改配置自动重算预览**（300ms 防抖）。
// Dify 那套要点按钮才出预览的做法，调参时等于闭眼调；这里把预览当成配置的函数。

// previewDebounceMS 是配置变更到发起预览之间的等待时间。
// 太短会在拖动滑块时打出一串请求，太长会让人觉得「没反应」。
const previewDebounceMS = 300;

// previewTimer 是模块级定时器：放在 store 外避免它进入状态树（它不需要被订阅）。
let previewTimer: ReturnType<typeof setTimeout> | null = null;

interface ChunkWorkbenchState {
  datasetId: number;
  documentId: number;
  options: ChunkOptions | null;
  config: ChunkConfig;
  preview: PreviewResult | null;
  loading: boolean;
  /** 预览请求进行中 */
  previewing: boolean;
  /** 入库请求进行中 */
  ingesting: boolean;
  /** 是否把当前配置存为知识库默认 */
  saveAsDefault: boolean;
  error: string;
  notice: string;

  load: (datasetId: number, documentId: number) => Promise<void>;
  setConfig: (patch: Partial<ChunkConfig>) => void;
  toggleRule: (key: string, enabled: boolean) => void;
  setRulePattern: (key: string, pattern: string) => void;
  resetToDatasetDefault: () => Promise<void>;
  applyPreset: (preset: string) => void;
  setSaveAsDefault: (value: boolean) => void;
  refreshPreview: () => Promise<void>;
  ingest: () => Promise<boolean>;
  clearMessages: () => void;
}

/** emptyConfig 是加载完成前的占位配置。 */
const emptyConfig: ChunkConfig = {
  mode: "title",
  separator: "",
  maxRunes: 800,
  overlapRunes: 80,
  minRunes: 40,
  keepNoise: false,
  cleaning: [],
  // 关键词抽取默认开启：配置里没有这一项时，后端也按「开启、最多 5 个」处理。
  keywords: { enabled: true, maxCount: 5 },
};

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : "未知错误";
}

export const useChunkWorkbenchStore = create<ChunkWorkbenchState>((set, get) => {
  // schedulePreview 在配置变化后延迟触发一次预览。
  const schedulePreview = () => {
    if (previewTimer) {
      clearTimeout(previewTimer);
    }
    previewTimer = setTimeout(() => {
      previewTimer = null;
      void get().refreshPreview();
    }, previewDebounceMS);
  };

  return {
    datasetId: 0,
    documentId: 0,
    options: null,
    config: emptyConfig,
    preview: null,
    loading: false,
    previewing: false,
    ingesting: false,
    saveAsDefault: false,
    error: "",
    notice: "",

    load: async (datasetId, documentId) => {
      set({ loading: true, error: "", datasetId, documentId });
      try {
        const options = await chunkApi.options(datasetId);
        set({ options, config: options.defaults, loading: false });
        await get().refreshPreview();
      } catch (error) {
        set({ loading: false, error: describeError(error) });
      }
    },

    setConfig: (patch) => {
      set((state) => ({ config: { ...state.config, ...patch } }));
      schedulePreview();
    },

    toggleRule: (key, enabled) => {
      set((state) => {
        const exists = state.config.cleaning.some((rule) => rule.key === key);
        let cleaning = state.config.cleaning;
        if (enabled && !exists) {
          cleaning = [...cleaning, { key, pattern: "" }];
        } else if (!enabled && exists) {
          cleaning = cleaning.filter((rule) => rule.key !== key);
        }
        return { config: { ...state.config, cleaning } };
      });
      schedulePreview();
    },

    setRulePattern: (key, pattern) => {
      set((state) => ({
        config: {
          ...state.config,
          cleaning: state.config.cleaning.map((rule) =>
            rule.key === key ? { ...rule, pattern } : rule,
          ),
        },
      }));
      schedulePreview();
    },

    resetToDatasetDefault: async () => {
      const { datasetId } = get();
      try {
        const config = await chunkApi.getChunkConfig(datasetId);
        set({ config, notice: "已恢复为知识库默认配置" });
        await get().refreshPreview();
      } catch (error) {
        set({ error: describeError(error) });
      }
    },

    applyPreset: (preset) => {
      // 预设只改与「怎么切」直接相关的参数，不动清洗规则：
      // 清洗是不可逆的，不该被一键预设顺手打开。
      const presets: Record<string, Partial<ChunkConfig>> = {
        // 手册 / 说明书：有清晰章节，切口落在标题上，段落偏短
        manual: { mode: "title", maxRunes: 800, overlapRunes: 80, minRunes: 40 },
        // 论文 / 报告：段落长，需要更大的片段才能容纳完整论证
        paper: { mode: "title", maxRunes: 1200, overlapRunes: 120, minRunes: 60 },
        // 聊天记录 / 日志：没有可靠层级，按长度切
        log: { mode: "length", maxRunes: 500, overlapRunes: 50, minRunes: 20 },
        // 逐条记录（FAQ、条款）：按空行分隔
        record: {
          mode: "separator",
          separator: "\n\n",
          maxRunes: 600,
          overlapRunes: 0,
          minRunes: 10,
        },
      };
      const patch = presets[preset];
      if (!patch) {
        return;
      }
      set((state) => ({ config: { ...state.config, ...patch } }));
      set({ notice: "已应用预设（清洗规则未改动）" });
      schedulePreview();
    },

    setSaveAsDefault: (value) => set({ saveAsDefault: value }),

    refreshPreview: async () => {
      const { documentId, config } = get();
      if (!documentId) {
        return;
      }
      set({ previewing: true, error: "" });
      try {
        const preview = await chunkApi.preview(documentId, config);
        set({ preview, previewing: false });
      } catch (error) {
        // 预览失败不清空上一次结果：配置改错时保留上一次可见的结果更有用。
        set({ previewing: false, error: describeError(error) });
      }
    },

    ingest: async () => {
      const { documentId, config, saveAsDefault } = get();
      set({ ingesting: true, error: "", notice: "" });
      try {
        await chunkApi.ingest(documentId, { config, saveAsDefault });
        set({ ingesting: false, notice: "已提交入库，可在文档列表看处理进度" });
        return true;
      } catch (error) {
        set({ ingesting: false, error: describeError(error) });
        return false;
      }
    },

    clearMessages: () => set({ error: "", notice: "" }),
  };
});

/** ruleEnabled 判断某条清洗规则是否已启用（供配置面板渲染勾选框）。 */
export function ruleEnabled(cleaning: CleaningRuleConfig[], key: string): boolean {
  return cleaning.some((rule) => rule.key === key);
}

/** rulePattern 读取某条规则的正则（未启用时为空）。 */
export function rulePattern(cleaning: CleaningRuleConfig[], key: string): string {
  return cleaning.find((rule) => rule.key === key)?.pattern ?? "";
}
