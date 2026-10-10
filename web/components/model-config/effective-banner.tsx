"use client";

import type { EffectiveModelInfo } from "@/types/api";

// 「当前生效模型」横幅。
//
// 配置页最有价值的信息不是「我配了哪些模型」，而是「现在实际在跑哪个」：
// 三级覆盖（知识库 / 应用 / 全局）+ 配置兜底叠加起来，光看列表很难推断结果。

const scopeLabels: Record<string, string> = {
  global: "全局默认",
  app: "应用覆盖",
  dataset: "知识库覆盖",
};

export function EffectiveBanner({ effective }: { effective: EffectiveModelInfo | null }) {
  if (!effective) {
    return (
      <div className="rounded-lg border border-neutral-200 bg-white p-4 text-sm text-neutral-500 shadow-sm">
        尚未读取到生效模型。
      </div>
    );
  }

  const configured = effective.modelName !== "";
  const source = effective.fromConfig
    ? "配置文件兜底"
    : scopeLabels[effective.scope] ?? "未知来源";

  return (
    <div className="rounded-lg border border-neutral-200 bg-white p-4 shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium text-neutral-500">当前生效的向量模型</span>
          <span className="text-base font-semibold text-neutral-900">
            {configured ? effective.modelName : "未配置"}
          </span>
          <span className="text-xs text-neutral-500">
            {effective.fromConfig
              ? "来自配置文件（数据库里还没有绑定，绑定后即可在界面上管理）"
              : `来源：${source}`}
          </span>
        </div>
        <dl className="flex gap-6 text-xs text-neutral-600">
          <div className="flex flex-col gap-1">
            <dt className="text-neutral-400">厂商</dt>
            <dd>{effective.providerName || effective.provider || "—"}</dd>
          </div>
          <div className="flex flex-col gap-1">
            <dt className="text-neutral-400">向量维度</dt>
            <dd>{effective.dimension > 0 ? effective.dimension : "—"}</dd>
          </div>
          <div className="flex flex-col gap-1">
            <dt className="text-neutral-400">来源</dt>
            <dd>{source}</dd>
          </div>
        </dl>
      </div>
    </div>
  );
}
