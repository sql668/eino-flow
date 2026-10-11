"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input, Panel, Select } from "@/components/ui/field";
import { appsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { AppInfo, AppMode } from "@/types/api";

// 应用列表页。
//
// 应用是平台的入口：形态决定对话时是否检索知识库（chat 纯对话 / rag 走知识库），
// 所以创建时就要选，而不是等进对话页再问。

const modeLabels: Record<AppMode, string> = {
  chat: "对话（不检索）",
  rag: "知识库问答（RAG）",
  agent: "智能体（尚未支持）",
  workflow: "工作流（尚未支持）",
};

// 只开放已实现的两类形态：摆出不能用的选项比不摆更糟。
const availableModes: AppMode[] = ["chat", "rag"];

export default function AppsPage() {
  const [apps, setApps] = useState<AppInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<{ name: string; description: string; mode: AppMode }>({
    name: "",
    description: "",
    mode: "rag",
  });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await appsApi.list();
      setApps(result.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载应用失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreate = async () => {
    if (form.name.trim() === "") {
      setError("应用名不能为空");
      return;
    }
    setCreating(true);
    setError("");
    try {
      await appsApi.create({
        name: form.name.trim(),
        description: form.description.trim(),
        mode: form.mode,
      });
      setForm({ name: "", description: "", mode: form.mode });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建应用失败");
    } finally {
      setCreating(false);
    }
  };

  return (
    <PageContainer className="flex flex-col gap-5 px-8 py-7">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="eyebrow">应用</span>
          <h1 className="text-[22px] font-semibold tracking-tight text-ink">对话入口</h1>
          <p className="max-w-2xl text-sm leading-6 text-ink-muted">
            应用分两类：<span className="text-ink">对话</span>只聊天；
            <span className="text-ink">知识库问答</span>会先检索你选中的知识库，回答带来源引用。
          </p>
        </div>
        <span className="reading text-xs text-ink-soft">{apps.length} 个应用</span>
      </header>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      <Panel eyebrow="新建" title="创建一个应用">
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-48 flex-1">
          <Field label="应用名" required>
            <Input
              value={form.name}
              onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
              placeholder="万用表问答助手"
            />
          </Field>
        </div>
        <div className="w-52">
          <Field label="形态" hint="对话=纯聊天；知识库问答=先检索再回答。">
            <Select
              value={form.mode}
              onChange={(event) =>
                setForm((prev) => ({ ...prev, mode: event.target.value as AppMode }))
              }
            >
              {availableModes.map((mode) => (
                <option key={mode} value={mode}>
                  {modeLabels[mode]}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <div className="min-w-48 flex-[2]">
          <Field label="描述">
            <Input
              value={form.description}
              onChange={(event) =>
                setForm((prev) => ({ ...prev, description: event.target.value }))
              }
              placeholder="可选"
            />
          </Field>
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="h-4" aria-hidden="true" />
          <Button variant="primary" busy={creating} onClick={handleCreate}>
            创建
          </Button>
        </div>
      </div>
      </Panel>

      <Panel
        eyebrow="全部"
        title="应用清单"
        reading={`${apps.length} 项`}
        actions={
          <Button busy={loading} onClick={() => void load()}>
            刷新
          </Button>
        }
      >
        {apps.length === 0 ? (
          <p className="py-8 text-center text-xs text-ink-soft">
            {loading ? "加载中…" : "还没有应用，先在上面创建一个"}
          </p>
        ) : (
          <ul className="divide-y divide-grid-soft">
            {apps.map((app) => (
              <li key={app.id} className="flex flex-wrap items-center gap-3 py-3">
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex items-center gap-2">
                    <Link
                      href={`/apps/${app.id}`}
                      className="truncate text-sm font-medium text-ink hover:text-pri"
                    >
                      {app.name}
                    </Link>
                    <span
                      className={`shrink-0 rounded-sm px-1.5 py-0.5 text-[11px] ${
                        app.mode === "rag" ? "bg-ok-soft text-ok" : "bg-pri-soft text-pri-ink"
                      }`}
                    >
                      {modeLabels[app.mode] ?? app.mode}
                    </span>
                  </div>
                  <span className="truncate text-xs text-ink-muted">
                    {app.description || "（无描述）"}
                  </span>
                </div>
                <span className="reading hidden text-xs text-ink-soft md:block">
                  {app.updatedAt.slice(0, 10)}
                </span>
                <div className="flex items-center gap-2">
                  <Link
                    href={`/apps/${app.id}`}
                    className="rounded bg-pri px-3 py-1.5 text-xs text-white transition-colors hover:bg-pri-ink"
                  >
                    对话调试
                  </Link>
                  <Button
                    variant="danger"
                    onClick={() => {
                      if (window.confirm(`确定删除应用「${app.name}」吗？`)) {
                        void appsApi.remove(app.id).then(load);
                      }
                    }}
                  >
                    删除
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </PageContainer>
  );
}
