"use client";

import { useModelConfigStore } from "@/stores/model-config-store";
import type { EffectiveModelInfo, ModelKind } from "@/types/api";

// 「当前生效模型」面板。
//
// Dify 把默认模型设置放在独立页面；这里先做成模型供应商页顶部的一块面板，
// 因为它回答的是「现在到底在用哪个模型」——这正是配置了三级覆盖之后最容易迷失的信息：
// 知识库 / 应用 / 全局层层覆盖叠加起来，光看模型列表推不出结果。

const kindLabels: Record<ModelKind, string> = {
  llm: "对话模型",
  embedding: "向量化模型",
  rerank: "重排模型",
};

const scopeLabels: Record<string, string> = {
  global: "全局默认",
  app: "应用覆盖",
  dataset: "知识库覆盖",
};

export function DefaultModelPanel() {
  const effective = useModelConfigStore((state) => state.effective);
  const effectiveRerank = useModelConfigStore((state) => state.effectiveRerank);
  const effectiveLLM = useModelConfigStore((state) => state.effectiveLLM);

  return (
    <div className="grid gap-3 lg:grid-cols-3">
      <EffectiveCard kind="llm" effective={effectiveLLM} />
      <EffectiveCard kind="embedding" effective={effective} />
      <EffectiveCard kind="rerank" effective={effectiveRerank} />
    </div>
  );
}

function EffectiveCard({
  kind,
  effective,
}: {
  kind: ModelKind;
  effective: EffectiveModelInfo | null;
}) {
  // 对话模型可以正常配置与显示，只是调用逻辑还没接入（随流式对话落地）。
  // 之前这里一律显示「尚未接入」，导致配好了模型却像没生效——配置状态必须如实反映。
  const configured = effective !== null && effective.modelName !== "";
  const dimensionText =
    configured && kind === "embedding" && (effective?.dimension ?? 0) > 0
      ? ` · ${effective.dimension} 维`
      : "";
  const sourceText = !configured
    ? kind === "llm"
      ? "还没有设定对话模型"
      : "还没有绑定模型"
    : effective?.fromConfig
      ? "来自配置文件兜底，尚未在界面上绑定"
      : effective?.providerName || "—";
  const note = kind === "llm" && configured ? "调用逻辑尚未接入（随流式对话落地）" : "";

  return (
    <div className="rounded-xl border border-neutral-200 bg-white px-4 py-3">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-neutral-500">{kindLabels[kind]}</span>
        {configured ? (
          <span className="rounded bg-emerald-50 px-1.5 py-0.5 text-[11px] text-emerald-700">
            {scopeLabels[effective?.scope ?? ""] ?? "已启用"}
          </span>
        ) : (
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-[11px] text-neutral-500">
            未配置
          </span>
        )}
      </div>

      <div className="mt-2 flex flex-col gap-1">
        <span className="truncate text-sm font-semibold text-neutral-900">
          {effective?.modelName || "未配置"}
        </span>
        <span className="truncate text-xs text-neutral-500">
          {sourceText}
          {dimensionText}
        </span>
        {note ? <span className="text-[11px] text-amber-700">{note}</span> : null}
      </div>
    </div>
  );
}
