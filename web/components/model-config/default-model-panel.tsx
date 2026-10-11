"use client";

import { Reading, ReadingRow } from "@/components/ui/reading";
import { useModelConfigStore } from "@/stores/model-config-store";
import type { EffectiveModelInfo } from "@/types/api";

// 「当前生效的模型」读数条。
//
// 这一块回答的是「现在实际在跑哪个模型」——三级覆盖（知识库 / 应用 / 全局）叠加之后，
// 光看下面的模型清单是推不出来的。所以把它做成页面级读数条，而不是三张卡片：
// 读数条能在同一行里横向比较三类能力，卡片会把它们拆散。

export function DefaultModelPanel() {
  const effective = useModelConfigStore((state) => state.effective);
  const effectiveRerank = useModelConfigStore((state) => state.effectiveRerank);
  const effectiveLLM = useModelConfigStore((state) => state.effectiveLLM);

  return (
    <ReadingRow columns={3}>
      <EffectiveReading kind="llm" effective={effectiveLLM} />
      <EffectiveReading kind="embedding" effective={effective} />
      <EffectiveReading kind="rerank" effective={effectiveRerank} />
    </ReadingRow>
  );
}

function EffectiveReading({
  kind,
  effective,
}: {
  kind: "llm" | "embedding" | "rerank";
  effective: EffectiveModelInfo | null;
}) {
  const configured = effective !== null && effective.modelName !== "";
  const labels = { llm: "对话模型", embedding: "向量模型", rerank: "重排模型" } as const;

  // 未配置时给「怎么办」，不只是显示「未配置」：空状态要能指路。
  const note = !configured
    ? kind === "llm"
      ? "在下方厂商卡片里添加对话模型并设为默认"
      : "在下方厂商卡片里添加模型并设为默认"
    : effective?.fromConfig
      ? "来自配置文件兜底，尚未在界面上绑定"
      : `${effective?.providerName || "—"}${
          kind === "embedding" && (effective?.dimension ?? 0) > 0
            ? ` · ${effective.dimension} 维`
            : ""
        }${kind === "llm" ? " · 调用随流式对话落地" : ""}`;

  return (
    <Reading
      label={labels[kind]}
      value={configured ? effective.modelName : "未配置"}
      tone={configured ? "default" : "muted"}
      note={note}
    />
  );
}
