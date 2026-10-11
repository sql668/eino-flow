"use client";

import Link from "next/link";
import { useEffect } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { HealthCheck } from "@/components/health-check";
import { Panel } from "@/components/ui/field";
import { useModelConfigStore } from "@/stores/model-config-store";

// 主页 = 控制台总览。
//
// 设计取向：这一页不摆入口卡片墙，而是当成**仪表盘**——先给三条关键读数
// （当前向量模型 / 重排服务 / 系统就绪），再列可进入的功能。
// 对工程师来说「现在系统在用什么模型、服务是否就绪」比「有哪些功能」更重要。

interface Entry {
  href: string;
  title: string;
  description: string;
  pending?: boolean;
}

const entries: Entry[] = [
  {
    href: "/datasets",
    title: "知识库",
    description: "上传 → 解析 → 确认分段与清洗 → 入库 → 可检索（支持两阶段重排与引用溯源）",
  },
  {
    href: "/apps",
    title: "应用与对话调试",
    description: "创建应用，用知识库问答验证检索效果，回答带来源引用",
  },
  {
    href: "/settings/models",
    title: "模型配置",
    description: "厂商凭证、模型清单、全局默认与连通性测试",
  },
];

export default function HomePage() {
  const effective = useModelConfigStore((state) => state.effective);
  const effectiveRerank = useModelConfigStore((state) => state.effectiveRerank);
  const loadAll = useModelConfigStore((state) => state.loadAll);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <header className="flex flex-col gap-1">
        <span className="eyebrow">控制台</span>
        <h1 className="text-[22px] font-semibold tracking-tight text-ink">
          EinoFlow · AI 工作流控制台
        </h1>
        <p className="max-w-2xl text-sm leading-6 text-ink-muted">
          基于 CloudWeGo Eino 的编排内核，Go 后端 + Next.js 前端。下面是当前实际生效的读数。
        </p>
      </header>

      <Panel eyebrow="运行读数" title="当前生效的模型与服务">
        <dl className="grid grid-cols-2 divide-grid sm:grid-cols-3 sm:divide-x">
          <Reading
            label="向量模型"
            value={effective?.modelName || "未配置"}
            note={effective?.fromConfig ? "来自配置文件兜底" : effective?.providerName || "—"}
          />
          <Reading
            label="重排模型"
            value={effectiveRerank?.modelName || "未启用"}
            note={effectiveRerank ? effectiveRerank.providerName : "检索将按向量顺序返回"}
          />
          <Reading
            label="对话模型"
            value={
              useModelConfigStore.getState().effectiveLLM?.modelName || "未配置"
            }
            note="知识库问答用"
          />
        </dl>
        <div className="mt-3 flex items-center gap-3 border-t border-grid-soft pt-3">
          <span className="eyebrow !text-ink-soft">后端连通性</span>
          <HealthCheck />
        </div>
      </Panel>

      <Panel eyebrow="入口" title="可以做什么" reading={`${entries.length} 项`}>
        <ul className="divide-y divide-grid-soft">
          {entries.map((entry) => {
            const body = (
              <>
                <span className="w-6 shrink-0 text-ink-soft" aria-hidden="true">
                  →
                </span>
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex items-center gap-2 text-sm font-medium text-ink">
                    {entry.title}
                    {entry.pending ? (
                      <span className="rounded-sm bg-grid-soft px-1 py-0.5 text-[10px] text-ink-muted">
                        待开发
                      </span>
                    ) : null}
                  </span>
                  <span className="text-xs leading-5 text-ink-muted">{entry.description}</span>
                </span>
              </>
            );

            if (entry.pending) {
              return (
                <li key={entry.href} className="flex cursor-not-allowed gap-3 py-3 opacity-55">
                  {body}
                </li>
              );
            }
            return (
              <li key={entry.href}>
                <Link
                  href={entry.href}
                  className="flex items-start gap-3 py-3 transition-colors hover:bg-paper"
                >
                  {body}
                </Link>
              </li>
            );
          })}
        </ul>
      </Panel>

      <p className="text-xs leading-5 text-ink-soft">
        模块完成情况与未完成项见 docs/PROGRESS.md；规则见 AGENTS.md（含「开工前先对齐」与视觉方向）。
      </p>
    </PageContainer>
  );
}

/** Reading 是仪表读数：大号等宽数字 + 说明，全站读数统一用这个形态。 */
function Reading({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex flex-col gap-0.5 px-3 py-2 first:pl-0">
      <dt className="eyebrow !text-ink-soft">{label}</dt>
      <dd className="reading truncate text-[15px] font-semibold text-ink" title={value}>
        {value}
      </dd>
      {note ? <span className="text-[10px] text-ink-soft">{note}</span> : null}
    </div>
  );
}
