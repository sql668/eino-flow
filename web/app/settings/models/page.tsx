"use client";

import { useEffect } from "react";

import { EffectiveBanner } from "@/components/model-config/effective-banner";
import { ModelSection } from "@/components/model-config/model-section";
import { ProviderCard } from "@/components/model-config/provider-card";
import { Button } from "@/components/ui/button";
import { useModelConfigStore } from "@/stores/model-config-store";

// 模型配置页（阶段 2）。
//
// 页面结构对应「像 Dify 一样配置模型」的三步：
//   1. 保存厂商凭证（内置厂商，填地址 + 密钥）；
//   2. 添加模型（声明这个模型名被当作什么能力用）；
//   3. 设为默认 / 覆盖（全局 / 应用 / 知识库三级）。
//
// 页面本身不持有业务状态：数据与操作都在 Zustand store 里，
// 组件只负责渲染与把用户动作转发给 store。

export default function ModelSettingsPage() {
  const providers = useModelConfigStore((state) => state.providers);
  const loading = useModelConfigStore((state) => state.loading);
  const error = useModelConfigStore((state) => state.error);
  const notice = useModelConfigStore((state) => state.notice);
  const effective = useModelConfigStore((state) => state.effective);
  const loadAll = useModelConfigStore((state) => state.loadAll);
  const clearMessages = useModelConfigStore((state) => state.clearMessages);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 px-6 py-10">
      <header className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">模型配置</h1>
          <Button onClick={() => void loadAll()} busy={loading}>
            刷新
          </Button>
        </div>
        <p className="text-sm leading-6 text-neutral-600">
          厂商是内置的（Ollama / OpenAI 兼容接口 / TEI 边车），你需要填的是地址与凭证；
          凭证以 AES-256-GCM 加密存储，接口只返回掩码。
        </p>
      </header>

      {/* 提示区：成功与失败都放在页面顶部，避免每个卡片各写一套提示 */}
      {error || notice ? (
        <div
          className={`flex items-start justify-between gap-3 rounded-md px-3 py-2 text-sm ${
            error ? "bg-red-50 text-red-700" : "bg-emerald-50 text-emerald-700"
          }`}
        >
          <span className="leading-6">{error || notice}</span>
          <button
            type="button"
            className="text-xs text-neutral-500 hover:text-neutral-800"
            onClick={clearMessages}
          >
            关闭
          </button>
        </div>
      ) : null}

      <EffectiveBanner effective={effective} />

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-medium text-neutral-500">厂商凭证</h2>
        {providers.length === 0 ? (
          <p className="rounded-lg border border-neutral-200 bg-white p-4 text-sm text-neutral-500 shadow-sm">
            {loading ? "加载中…" : "没有可用的内置厂商"}
          </p>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {providers.map((provider) => (
              <ProviderCard key={provider.provider} provider={provider} />
            ))}
          </div>
        )}
      </section>

      <ModelSection />

      <p className="text-xs leading-5 text-neutral-500">
        换 embedding 模型会改变向量空间：向量维度不一致时后端会直接报错（不会静默写入），
        此时需要清空向量集合后重新入库文档。
      </p>
    </main>
  );
}
