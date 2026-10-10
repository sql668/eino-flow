import Link from "next/link";

import { HealthCheck } from "@/components/health-check";

/**
 * 首页。
 * V0.1 阶段用于确认前后端骨架已连通，并提供已落地页面的入口；
 * 后续替换为应用列表入口。
 */
export default function HomePage() {
  return (
    <main className="mx-auto flex min-h-screen max-w-3xl flex-col gap-10 px-6 py-16">
      <header className="flex flex-col gap-3">
        <h1 className="text-3xl font-semibold tracking-tight">EinoFlow</h1>
        <p className="text-base text-neutral-600">
          基于 CloudWeGo Eino 内核的低代码 AI 工作流 / 智能体平台。
        </p>
      </header>

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium text-neutral-500">当前阶段</h2>
        <p className="text-sm leading-6 text-neutral-600">
          V0.1 极速 Demo：前后端骨架已搭建完成，接下来接入 Eino 基础内核、RAG
          入库检索与流式对话接口。
        </p>
      </section>

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium text-neutral-500">后端连通性</h2>
        <HealthCheck />
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-medium text-neutral-500">功能入口</h2>
        <div className="flex flex-col gap-2">
          <Link
            href="/settings/models"
            className="rounded-md border border-neutral-200 bg-white px-4 py-3 text-sm shadow-sm transition-colors hover:border-neutral-400"
          >
            <span className="font-medium text-neutral-900">模型配置</span>
            <span className="ml-2 text-neutral-500">
              配置厂商凭证、添加模型、设置默认模型
            </span>
          </Link>
        </div>
      </section>
    </main>
  );
}
