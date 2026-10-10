"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { ChatPanel } from "@/components/chat/chat-panel";
import { appsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { AppInfo } from "@/types/api";

// 应用详情 = 对话调试页。
//
// 这一页故意不套 PageContainer：对话需要「消息区自己滚动、输入框固定在底部」，
// 而 PageContainer 会整块滚动，输入框会被推到屏幕外。
// 这里的布局是「头部 + 可滚动消息区 + 底部输入」，滚动边界在 ChatPanel 内部。
export default function AppChatPage() {
  const params = useParams<{ id: string }>();
  const appId = Number(params.id);

  const [app, setApp] = useState<AppInfo | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!Number.isFinite(appId) || appId <= 0) {
      setError("应用 ID 不合法");
      return;
    }
    appsApi
      .get(appId)
      .then(setApp)
      .catch((err) => setError(err instanceof ApiError ? err.message : "加载应用失败"));
  }, [appId]);

  if (error) {
    return (
      <div className="flex flex-col gap-3 px-8 py-7">
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
        <Link href="/apps" className="text-sm text-neutral-600 hover:text-neutral-900">
          ← 返回应用列表
        </Link>
      </div>
    );
  }

  if (!app) {
    return <p className="px-8 py-7 text-sm text-neutral-500">加载中…</p>;
  }

  return <ChatPanel app={app} />;
}
