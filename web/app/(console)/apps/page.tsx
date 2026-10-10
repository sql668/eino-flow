"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { PageContainer } from "@/components/layout/page-container";
import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/field";
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
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">应用</h1>
        <p className="text-sm leading-6 text-neutral-600">
          应用分两类：<b>对话</b>只聊天；<b>知识库问答</b>会先检索你选中的知识库，回答里带来源引用。
        </p>
      </header>

      {error ? (
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
      ) : null}

      <section className="flex flex-wrap items-end gap-3 rounded-xl border border-neutral-200 bg-white px-4 py-3">
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
            创建应用
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-medium text-neutral-500">全部应用（{apps.length}）</h2>
          <Button busy={loading} onClick={() => void load()}>
            刷新
          </Button>
        </div>

        {apps.length === 0 ? (
          <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-8 text-center text-xs text-neutral-400">
            {loading ? "加载中…" : "还没有应用，先在上面创建一个"}
          </p>
        ) : (
          <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {apps.map((app) => (
              <li
                key={app.id}
                className="flex flex-col gap-2 rounded-xl border border-neutral-200 bg-white px-4 py-3"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-sm font-semibold text-neutral-900">
                    {app.name}
                  </span>
                  <span
                    className={`shrink-0 rounded px-1.5 py-0.5 text-[11px] ${
                      app.mode === "rag"
                        ? "bg-emerald-50 text-emerald-700"
                        : "bg-violet-50 text-violet-700"
                    }`}
                  >
                    {modeLabels[app.mode] ?? app.mode}
                  </span>
                </div>
                <p className="min-h-10 text-xs leading-5 text-neutral-500">
                  {app.description || "（无描述）"}
                </p>
                <div className="flex items-center gap-2">
                  <Link
                    href={`/apps/${app.id}`}
                    className="rounded-md bg-neutral-900 px-3 py-1.5 text-xs text-white transition-colors hover:bg-neutral-700"
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
      </section>
    </PageContainer>
  );
}
