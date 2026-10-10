"use client";

import { useCallback, useState } from "react";

import { http } from "@/lib/http";

type Status = "idle" | "loading" | "ok" | "error";

/**
 * 后端连通性探针。
 * 用途：验证统一请求封装（lib/http）与后端 /healthz 是否打通，
 * 骨架阶段作为前后端联调的快速自检手段。
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

  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        onClick={check}
        disabled={status === "loading"}
        className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm transition-colors hover:bg-neutral-100 disabled:opacity-50"
      >
        检测后端连接
      </button>
      <span className="text-sm text-neutral-600">{message}</span>
    </div>
  );
}
