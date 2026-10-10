"use client";

import { useEffect, useMemo, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { DefaultModelPanel } from "@/components/model-config/default-model-panel";
import { ProviderCard } from "@/components/model-config/provider-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/field";
import { useModelConfigStore } from "@/stores/model-config-store";

// 模型供应商页。
//
// 结构对齐 Dify 的「集成 → 模型供应商」（本机 http://localhost/integrations/model-provider）：
//   标题 + 说明 → 工具条（搜索）→ 已配置分组 → 待配置分组 → 安装更多（本项目的内置厂商是代码，
//   没有市场，因此这一块换成「待配置」提示）。
// EinoFlow 额外多一块「当前生效模型」：三级覆盖之下，需要有人直接回答「现在用的是什么」。

export default function ModelSettingsPage() {
  const providers = useModelConfigStore((state) => state.providers);
  const models = useModelConfigStore((state) => state.models);
  const loading = useModelConfigStore((state) => state.loading);
  const error = useModelConfigStore((state) => state.error);
  const notice = useModelConfigStore((state) => state.notice);
  const loadAll = useModelConfigStore((state) => state.loadAll);
  const clearMessages = useModelConfigStore((state) => state.clearMessages);

  // 搜索是纯展示层过滤，放组件内 useState 即可（不需要跨组件共享）。
  const [keyword, setKeyword] = useState("");

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  const { configured, unconfigured, matched } = useMemo(() => {
    const text = keyword.trim().toLowerCase();
    const hit = (provider: (typeof providers)[number]) =>
      text === "" ||
      provider.displayName.toLowerCase().includes(text) ||
      provider.provider.toLowerCase().includes(text) ||
      models.some(
        (model) =>
          model.provider === provider.provider && model.modelName.toLowerCase().includes(text),
      );
    const list = providers.filter(hit);
    return {
      matched: models,
      configured: list.filter((provider) => provider.configured),
      unconfigured: list.filter((provider) => !provider.configured),
    };
  }, [providers, models, keyword]);

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">模型供应商</h1>
        <p className="text-sm leading-6 text-neutral-600">
          厂商是内置的（Ollama / OpenAI 兼容接口 / TEI 重排服务），你只需要填服务地址与凭证。
          凭证以 AES-256-GCM 加密存储，接口与页面只会看到掩码。
        </p>
      </header>

      {error || notice ? (
        <div
          className={`flex items-start justify-between gap-3 rounded-lg px-3 py-2 text-sm ${
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

      <DefaultModelPanel />

      <div className="flex flex-wrap items-center gap-3">
        <div className="w-72">
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索厂商或模型…"
          />
        </div>
        <span className="text-xs text-neutral-500">
          共 {providers.length} 个内置厂商，{matched.length} 个模型
        </span>
        <div className="ml-auto">
          <Button busy={loading} onClick={() => void loadAll()}>
            刷新
          </Button>
        </div>
      </div>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-medium text-neutral-500">已配置</h2>
        {configured.length === 0 ? (
          <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-6 text-center text-xs text-neutral-400">
            {loading ? "加载中…" : "还没有配置任何厂商"}
          </p>
        ) : (
          configured.map((provider) => (
            <ProviderCard
              key={provider.provider}
              provider={provider}
              models={models.filter((model) => model.provider === provider.provider)}
            />
          ))
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-medium text-neutral-500">待配置</h2>
        {unconfigured.length === 0 ? (
          <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-6 text-center text-xs text-neutral-400">
            内置厂商都已完成配置
          </p>
        ) : (
          unconfigured.map((provider) => (
            <ProviderCard
              key={provider.provider}
              provider={provider}
              models={models.filter((model) => model.provider === provider.provider)}
            />
          ))
        )}
      </section>

      <p className="text-xs leading-5 text-neutral-500">
        说明：换掉向量化模型会改变向量空间。维度不一致时后端会直接报错（不会静默写入），
        此时需要清空向量集合后重新入库文档；重排模型可以随时替换，只会影响排序结果。
      </p>
    </PageContainer>
  );
}
