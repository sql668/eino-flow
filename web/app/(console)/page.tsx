"use client";

import Link from "next/link";
import { useEffect } from "react";

import { HealthCheck } from "@/components/health-check";
import { useModelConfigStore } from "@/stores/model-config-store";

// 主页。
// 结构参考 Dify 首页：问候语 + 分组标题 + 卡片网格；
// 骨架阶段的卡片指向已落地的页面，未开发的入口标注「待开发」而不是给一个死链接。

interface EntryCard {
  href: string;
  title: string;
  meta: string;
  description: string;
  pending?: boolean;
}

const entries: EntryCard[] = [
  {
    href: "/settings/models",
    title: "模型配置",
    meta: "已完成",
    description: "配置厂商凭证、添加模型、设置全局默认与连通性测试。",
  },
  {
    href: "/datasets",
    title: "知识库",
    meta: "接口已完成，页面待开发",
    description: "上传文档 → 解析切分 → 向量化入库 → 检索溯源。",
    pending: true,
  },
  {
    href: "/apps",
    title: "应用与对话调试",
    meta: "待开发",
    description: "创建应用、ReAct / RAG 问答与流式对话（V0.1 收尾）。",
    pending: true,
  },
];

export default function HomePage() {
  const effective = useModelConfigStore((state) => state.effective);
  const loadAll = useModelConfigStore((state) => state.loadAll);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  return (
    <div className="flex flex-col gap-6 px-8 py-7">
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">欢迎回来 👋</h1>
        <p className="text-sm text-neutral-600">
          基于 CloudWeGo Eino 内核的低代码 AI 工作流 / 智能体平台。
        </p>
      </header>

      <section className="flex flex-wrap items-center gap-3 rounded-xl border border-neutral-200 bg-white px-4 py-3">
        <span className="text-xs font-medium text-neutral-500">当前向量模型</span>
        <span className="text-sm font-semibold text-neutral-900">
          {effective?.modelName || "未配置"}
        </span>
        <span className="text-xs text-neutral-500">
          {effective?.fromConfig ? "（来自配置文件兜底）" : effective?.providerName || ""}
        </span>
        <div className="ml-auto">
          <HealthCheck />
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-medium text-neutral-500">功能入口</h2>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {entries.map((entry) => {
            const body = (
              <>
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-semibold text-neutral-900">{entry.title}</span>
                  <span
                    className={`rounded px-1.5 py-0.5 text-[11px] ${
                      entry.pending
                        ? "bg-neutral-100 text-neutral-500"
                        : "bg-emerald-50 text-emerald-700"
                    }`}
                  >
                    {entry.meta}
                  </span>
                </div>
                <p className="mt-2 text-xs leading-5 text-neutral-600">{entry.description}</p>
              </>
            );

            if (entry.pending) {
              return (
                <div
                  key={entry.href}
                  className="cursor-not-allowed rounded-xl border border-neutral-200 bg-white px-4 py-4 opacity-60"
                  title="该页面尚未开发，可先用后端接口验证"
                >
                  {body}
                </div>
              );
            }
            return (
              <Link
                key={entry.href}
                href={entry.href}
                className="rounded-xl border border-neutral-200 bg-white px-4 py-4 transition-colors hover:border-neutral-400"
              >
                {body}
              </Link>
            );
          })}
        </div>
      </section>

      <section className="flex flex-col gap-2 rounded-xl border border-neutral-200 bg-white px-4 py-4">
        <h2 className="text-sm font-medium text-neutral-900">当前阶段</h2>
        <p className="text-xs leading-6 text-neutral-600">
          V0.1 极速 Demo：模型配置与 RAG 检索链路（含重排）已跑通；
          接下来是 Eino 内核接入与流式对话。模块完成情况见{" "}
          <span className="font-medium text-neutral-800">docs/PROGRESS.md</span>。
        </p>
      </section>
    </div>
  );
}
