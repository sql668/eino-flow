"use client";

import { useCallback, useState } from "react";

import { http } from "@/lib/http";

type Status = "idle" | "loading" | "ok" | "error";

/**
 * 后端连通性探针。
 *
 * 展示形态按「仪表读数」来：状态用一个小圆点 + 等宽文字，不用大按钮。
 * 它是个诊断读数，不该抢主操作的注意力。
 */
export function HealthCheck() {
  const [status, setStatus] = useState<Status>("idle");
  const [message, setMessage] = useState("尚未检测");

  const check = useCallback(async () => {
    setStatus("loading");
    setMessage("检测中…");
    try {
      await http.get("/healthz");
      setStatus("ok");
      setMessage("后端已连通");
    } catch (error) {
      // 这里只做连通性提示，直接把错误信息展示出来便于本地排查。
      setStatus("error");
      setMessage(error instanceof Error ? error.message : "未知错误");
    }
  }, []);

  const tone =
    status === "ok"
      ? "bg-ok"
      : status === "error"
        ? "bg-fault"
        : status === "loading"
          ? "bg-warn"
          : "bg-ink-soft";

  return (
    <span className="flex items-center gap-2">
      <span className={`h-1.5 w-1.5 rounded-full ${tone}`} aria-hidden="true" />
      <span className="reading text-xs text-ink-muted">{message}</span>
      <button
        type="button"
        onClick={check}
        disabled={status === "loading"}
        className="rounded border border-grid px-2 py-0.5 text-[11px] text-ink-muted transition-colors hover:border-ink-soft hover:bg-paper disabled:opacity-50"
      >
        重新检测
      </button>
    </span>
  );
}
