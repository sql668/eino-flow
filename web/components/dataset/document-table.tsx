"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Panel } from "@/components/ui/field";
import { chunkApi, documentsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DocumentInfo, DocumentStatus } from "@/types/api";

// 文档列表：上传 + 状态 + 进工作台。
//
// 与旧版简易面板的差别：
//   - 用表格展示（名称 / 分段模式 / 字符数 / 段落数 / 状态 / 操作），对齐 Dify 的信息密度；
//   - 新增「待确认」状态：解析完成但还没确认分段配置，此时文档**不参与检索**；
//   - 每行的操作按状态给：待确认 → 去配置分段；失败 → 重新解析；就绪 → 调整分段 / 查看分段。

const statusMeta: Record<DocumentStatus, { label: string; className: string }> = {
  pending: { label: "排队中", className: "bg-grid-soft text-ink-muted" },
  parsing: { label: "解析中", className: "bg-warn-soft text-warn" },
  draft: { label: "待确认分段", className: "bg-pri-soft text-pri-ink" },
  indexing: { label: "入库中", className: "bg-warn-soft text-warn" },
  ready: { label: "已就绪", className: "bg-ok-soft text-ok" },
  failed: { label: "失败", className: "bg-fault-soft text-fault" },
};

const modeLabels: Record<string, string> = {
  title: "按标题",
  length: "按长度",
  separator: "按分隔符",
  parent_child: "父子分段",
};

export function DocumentTable({ datasetId }: { datasetId: number }) {
  const [documents, setDocuments] = useState<DocumentInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const load = useCallback(async () => {
    try {
      const result = await documentsApi.list(datasetId);
      setDocuments(result.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载文档失败");
    } finally {
      setLoading(false);
    }
  }, [datasetId]);

  useEffect(() => {
    void load();
  }, [load]);

  // 只在有「未完成」文档时轮询，处理完立即停：否则页面会一直空转打后端。
  useEffect(() => {
    const hasPending = documents.some(
      (item) => item.status === "pending" || item.status === "parsing" || item.status === "indexing",
    );
    if (!hasPending) {
      return;
    }
    const timer = window.setInterval(() => void load(), 3000);
    return () => window.clearInterval(timer);
  }, [documents, load]);

  const handleUpload = async (files: FileList) => {
    setBusy("upload");
    setError("");
    try {
      // 逐个上传：后端一次只接一个文件，而且解析本身是串行的。
      for (const file of Array.from(files)) {
        await documentsApi.upload(datasetId, file);
      }
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "上传失败");
    } finally {
      setBusy("");
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  };

  const runAction = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key);
    setError("");
    try {
      await action();
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "操作失败");
    } finally {
      setBusy("");
    }
  };

  return (
    <Panel
      eyebrow="文档"
      title="解析与入库"
      reading={`${documents.length} 个文件`}
      actions={
        <Button busy={loading} onClick={() => void load()}>
          刷新
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(event) => {
            if (event.target.files?.length) {
              void handleUpload(event.target.files);
            }
          }}
        />
        <Button variant="primary" busy={busy === "upload"} onClick={() => fileInputRef.current?.click()}>
          添加文件
        </Button>
        <span className="text-xs text-ink-muted">
          上传后先解析，再进「分段与清洗」确认后才入库；支持 PDF / 图片 / Office / txt / md
        </span>
      </div>

      {error ? (
        <p className="rounded border border-fault/30 bg-fault-soft px-3 py-2 text-sm text-fault">
          {error}
        </p>
      ) : null}

      {documents.length === 0 ? (
        <p className="rounded border border-dashed border-grid px-4 py-10 text-center text-xs text-ink-soft">
          {loading ? "加载中…" : "还没有文档，点「添加文件」开始"}
        </p>
      ) : (
        // 这里刻意不设 overflow-hidden：状态列的悬浮提示会超出单元格，被裁掉就看不全了
        <div className="border-t border-grid-soft">
          <table className="w-full text-sm">
            <thead className="text-[11px] text-ink-soft">
              <tr>
                <th className="px-3 py-2 text-left font-medium eyebrow !text-ink-soft">#</th>
                <th className="px-3 py-2 text-left font-medium eyebrow !text-ink-soft">名称</th>
                <th className="px-3 py-2 text-left font-medium eyebrow !text-ink-soft">分段模式</th>
                <th className="px-3 py-2 text-right font-medium eyebrow !text-ink-soft">字符数</th>
                <th className="px-3 py-2 text-right font-medium eyebrow !text-ink-soft">片段数</th>
                <th className="px-3 py-2 text-left font-medium eyebrow !text-ink-soft">状态</th>
                <th className="px-3 py-2 text-right font-medium eyebrow !text-ink-soft">操作</th>
              </tr>
            </thead>
            <tbody>
              {documents.map((doc, index) => {
                const meta = statusMeta[doc.status];
                const pendingChunk = doc.status === "draft";
                return (
                  <tr key={doc.id} className="border-t border-grid-soft align-top">
                    <td className="reading px-3 py-2 text-xs text-ink-soft">
                      {String(index + 1).padStart(2, "0")}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium text-ink">{doc.name}</span>
                        <span className="reading text-[11px] text-ink-soft">
                          {formatSize(doc.fileSize)}
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-2 text-xs text-ink-muted">
                      {doc.chunkMode ? modeLabels[doc.chunkMode] ?? doc.chunkMode : "—"}
                    </td>
                    <td className="reading px-3 py-2 text-right text-xs text-ink-muted">
                      {doc.charCount > 0 ? doc.charCount.toLocaleString("zh-CN") : "—"}
                    </td>
                    <td className="reading px-3 py-2 text-right text-xs text-ink-muted">
                      {doc.segmentCount > 0 ? doc.segmentCount : "—"}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-1.5">
                        <span className={`rounded px-1.5 py-0.5 text-[11px] ${meta.className}`}>
                          {meta.label}
                        </span>
                        {/* 失败原因放在状态旁的悬浮提示里：直接铺在名称列会把行撑得很高，
                            也会抢走名称的注意力。悬浮窗限宽限高并可滚动，长错误也不会溢出屏幕。 */}
                        {doc.errorMessage ? <FailureHint message={doc.errorMessage} /> : null}
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap justify-end gap-1.5">
                        {doc.parsed ? (
                          <Link
                            href={`/datasets/${datasetId}/documents/${doc.id}/chunk`}
                            className={`rounded-md px-2.5 py-1.5 text-xs transition-colors ${
                              pendingChunk
                                ? "bg-pri text-white hover:bg-pri-ink"
                                : "border border-grid text-ink hover:border-ink-soft hover:bg-paper"
                            }`}
                          >
                            {pendingChunk ? "配置分段" : "调整分段"}
                          </Link>
                        ) : (
                          <Button
                            variant={doc.status === "failed" ? "primary" : "secondary"}
                            busy={busy === `parse-${doc.id}`}
                            onClick={() =>
                              void runAction(`parse-${doc.id}`, () => chunkApi.reparse(doc.id))
                            }
                          >
                            {doc.status === "failed" ? "重新解析" : "解析"}
                          </Button>
                        )}
                        {doc.status === "ready" ? (
                          <Button
                            busy={busy === `rechunk-${doc.id}`}
                            onClick={() =>
                              void runAction(`rechunk-${doc.id}`, () => chunkApi.rechunk(doc.id))
                            }
                          >
                            按默认重切
                          </Button>
                        ) : null}
                        <Button
                          variant="danger"
                          busy={busy === `delete-${doc.id}`}
                          onClick={() => {
                            if (window.confirm(`确定删除「${doc.name}」吗？`)) {
                              void runAction(`delete-${doc.id}`, () => documentsApi.remove(doc.id));
                            }
                          }}
                        >
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      </div>
    </Panel>
  );
}

/** FailureHint 是失败原因的悬浮提示：限宽限高、内容过长时可滚动。 */
function FailureHint({ message }: { message: string }) {
  return (
    <span className="group relative inline-flex" tabIndex={0}>
      <span className="cursor-help rounded-sm border border-fault/30 px-1 py-0.5 text-[10px] leading-4 text-fault">
        失败原因
      </span>
      <span
        className="absolute left-0 top-full z-30 mt-1 hidden max-h-40 w-72 overflow-y-auto overscroll-contain rounded bg-ink px-2.5 py-2 text-[11px] font-normal leading-5 text-white shadow-lg group-hover:block group-focus:block"
        role="tooltip"
      >
        {message}
      </span>
    </span>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
