"use client";

import { create } from "zustand";

import { DEFAULT_SEPARATORS } from "@/components/chunk/separator-field";
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
// 关键交互（2026-10-11 调整）：
//   * 进入工作台会自动预览一次（不然右侧是空的，不知道从哪开始）；
//   * 之后**改配置不自动重算**，要点「预览」——大文件上自动预览纯属浪费，
//     而且并发请求不会取消，会一起排队；
//   * 配置改过而预览没跟上时，界面必须显示「结果已过期」，
//     否则使用者会照着旧结果判断新参数的效果。

interface ChunkWorkbenchState {
  datasetId: number;
  documentId: number;
  options: ChunkOptions | null;
  config: ChunkConfig;
  preview: PreviewResult | null;
  loading: boolean;
  /** 预览请求进行中 */
  previewing: boolean;
  /**
   * 配置改过、但预览还没跟上。
   *
   * 预览是**手动触发**的（大文件上自动预览太浪费），因此必须明确告诉使用者
   * 「右边看到的结果不是当前配置的」——否则调参时会照着过期结果下判断。
   */
  previewDirty: boolean;
  /**
   * 预览被跳过的原因（当前配置还不完整，例如分隔符模式没填分隔符）。
   *
   * 与 error 分开：这不是失败，是「还没轮到发请求」，
   * 混用会让界面用红色报错去说一件本来很正常的事。
   */
  blockedReason: string;
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
  /** 按当前配置重算预览（手动触发）。 */
  refreshPreview: () => Promise<void>;
  ingest: () => Promise<boolean>;
  clearMessages: () => void;
}

/** emptyConfig 是加载完成前的占位配置。 */
const emptyConfig: ChunkConfig = {
  mode: "title",
  separators: [],
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
  // markDirty 在配置变化后把预览标记为「已过期」。
  //
  // 刻意**不**自动重算：大文档上「还没调完就触发一次预览」纯属浪费
  // （实测 200 页文档每次预览 20~50ms 的解析与切分，改五个参数就是五次），
  // 而且并发的预览请求不会被取消，会一起排队。改为使用者点「预览」时才算，
  // 代价是要把「结果已过期」明确显示出来（见 previewDirty）。
  const markDirty = () => set({ previewDirty: true });

  return {
    datasetId: 0,
    documentId: 0,
    options: null,
    config: emptyConfig,
    preview: null,
    loading: false,
    previewing: false,
    previewDirty: false,
    blockedReason: "",
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
      set((state) => {
        const config = { ...state.config, ...patch };
        // 只在**切到**分隔符模式且还没填过分隔符时补默认值：否则这次改动会立刻
        // 触发一次注定失败的预览（后端拒绝空分隔符），用户看到的是红色报错。
        //
        // 判断依据是 patch 本身带没带 mode，而不是「结果里 mode 是不是 separator」：
        // 后者会让「用户手动清空分隔符输入框」被立刻填回去，输入框和配置对不上。
        if (patch.mode === "separator" && config.separators.length === 0) {
          config.separators = [...DEFAULT_SEPARATORS];
        }
        return { config };
      });
      markDirty();
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
      markDirty();
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
      markDirty();
    },

    resetToDatasetDefault: async () => {
      const { datasetId } = get();
      try {
        const config = await chunkApi.getChunkConfig(datasetId);
        // 同样只标记「已过期」而不自动重算：规则统一成「只有进页面时自动预览一次」，
        // 使用者才不会在「有的操作自动、有的不自动」之间猜。
        set({ config, notice: "已恢复为知识库默认配置，点「预览」查看结果", previewDirty: true });
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
          // 预设给两个分隔符：空行是最常见的「一条记录」边界，
          // 分隔线（----）则是纯文本导出里另一种常见写法。
          separators: ["\\n\\n", "----"],
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
      set({ notice: "已应用预设（清洗规则未改动），点「预览」查看结果" });
      markDirty();
    },

    setSaveAsDefault: (value) => set({ saveAsDefault: value }),

    refreshPreview: async () => {
      const { documentId, config } = get();
      if (!documentId) {
        return;
      }
      // 配置本身还不完整时不要发请求：分隔符模式下没填分隔符，
      // 后端会（正确但没意义地）返回 400，而用户此刻只是还没填完表单。
      // 这里给出「等待填写」的状态，比一条红色报错有用得多。
      if (config.mode === "separator" && config.separators.length === 0) {
        set({
          previewing: false,
          preview: null,
          error: "",
          blockedReason: "填写分隔符后会自动预览（多个用英文逗号隔开）",
        });
        return;
      }
      set({ previewing: true, error: "", blockedReason: "" });
      try {
        const preview = await chunkApi.preview(documentId, config);
        // 成功才清掉「过期」标记：失败时右侧仍是旧结果，标记必须留着。
        set({ preview, previewing: false, previewDirty: false });
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

    clearMessages: () => set({ error: "", notice: "", blockedReason: "" }),
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
