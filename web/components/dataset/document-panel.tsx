"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { documentsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { DocumentInfo, SegmentInfo } from "@/types/api";

// 知识库详情里的文档面板：上传、看状态、看分段。
//
// 状态需要轮询：上传是异步的（后端排队解析），返回时文档还在 pending/indexing。
// 轮询只在有「未完成」的文档时才跑，处理完就停——否则页面会一直空转打后端。

const statusMeta: Record<
  DocumentInfo["status"],
  { label: string; className: string }
> = {
  pending: { label: "排队中", className: "bg-neutral-100 text-neutral-600" },
  indexing: { label: "解析中", className: "bg-amber-50 text-amber-700" },
  ready: { label: "已就绪", className: "bg-emerald-50 text-emerald-700" },
  failed: { label: "失败", className: "bg-red-50 text-red-700" },
};

export function DocumentPanel({ datasetId }: { datasetId: number }) {
  const [documents, setDocuments] = useState<DocumentInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState("");
  // 展开的文档分段：记录文档 ID 与它的分段列表，同时只展开一个。
  const [detail, setDetail] = useState<{ documentId: number; segments: SegmentInfo[] } | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const load = useCallback(async () => {
    try {
      const result = await documentsApi.list(datasetId);
      setDocuments(result.items);
      return result.items;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载文档失败");
      return [];
    } finally {
      setLoading(false);
    }
  }, [datasetId]);

  useEffect(() => {
    void load();
  }, [load]);

  // 轮询：只要还有 pending / indexing 的文档，就每 3 秒刷新一次。
  useEffect(() => {
    const hasPending = documents.some(
      (item) => item.status === "pending" || item.status === "indexing",
    );
    if (!hasPending) {
      return;
    }
    const timer = window.setInterval(() => void load(), 3000);
    return () => window.clearInterval(timer);
  }, [documents, load]);

  const handleUpload = async (file: File) => {
    setUploading(true);
    setError("");
    try {
      await documentsApi.upload(datasetId, file);
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "上传失败");
    } finally {
      setUploading(false);
      // 清空 input：否则选同一个文件不会触发 change，看起来像没反应。
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  };

  const toggleSegments = async (document: DocumentInfo) => {
    if (detail?.documentId === document.id) {
      setDetail(null);
      return;
    }
    try {
      const result = await documentsApi.segments(document.id, 1, 20);
      setDetail({ documentId: document.id, segments: result.items });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载分段失败");
    }
  };

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-sm font-medium text-neutral-500">文档（{documents.length}）</h2>
        <input
          ref={fileInputRef}
          type="file"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) {
              void handleUpload(file);
            }
          }}
        />
        <Button
          variant="primary"
          busy={uploading}
          onClick={() => fileInputRef.current?.click()}
        >
          上传文档
        </Button>
        <Button busy={loading} onClick={() => void load()}>
          刷新
        </Button>
        <span className="text-xs text-neutral-500">
          支持 PDF / 图片 / Office / txt / md；PDF 与扫描件由 MinerU 解析，需要等待。
        </span>
      </div>

      {error ? (
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
      ) : null}

      {documents.length === 0 ? (
        <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-8 text-center text-xs text-neutral-400">
          {loading ? "加载中…" : "还没有文档，点「上传文档」开始"}
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {documents.map((document) => {
            const meta = statusMeta[document.status];
            const expanded = detail?.documentId === document.id;
            return (
              <li
                key={document.id}
                className="rounded-xl border border-neutral-200 bg-white px-4 py-3"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-sm font-medium text-neutral-900">
                    {document.name}
                  </span>
                  <span className={`rounded px-1.5 py-0.5 text-[11px] ${meta.className}`}>
                    {meta.label}
                  </span>
                  <span className="text-xs text-neutral-500">
                    {document.segmentCount} 段 · {formatSize(document.fileSize)}
                  </span>
                  <Button disabled={document.status !== "ready"} onClick={() => void toggleSegments(document)}>
                    {expanded ? "收起分段" : "查看分段"}
                  </Button>
                  <Button
                    variant="danger"
                    onClick={() => {
                      if (window.confirm(`确定删除「${document.name}」吗？`)) {
                        void documentsApi.remove(document.id).then(load);
                      }
                    }}
                  >
                    删除
                  </Button>
                </div>

                {document.errorMessage ? (
                  <p className="mt-2 rounded-md bg-red-50 px-2 py-1 text-xs leading-5 text-red-700">
                    处理失败：{document.errorMessage}
                  </p>
                ) : null}

                {expanded ? (
                  <div className="mt-3 flex flex-col gap-2 border-t border-neutral-100 pt-3">
                    {detail.segments.map((segment) => (
                      <div key={segment.id} className="rounded-md bg-neutral-50 px-3 py-2">
                        <div className="flex flex-wrap items-center gap-2 text-[11px] text-neutral-500">
                          <span>#{segment.chunkIndex}</span>
                          {segment.page > 0 ? <span>第 {segment.page} 页</span> : null}
                          {segment.titlePath.length > 0 ? (
                            <span>{segment.titlePath.join(" / ")}</span>
                          ) : null}
                          <span>{segment.kind === "table" ? "表格" : "文本"}</span>
                          <span>{segment.tokenCount} 字</span>
                        </div>
                        <p className="mt-1 whitespace-pre-wrap text-xs leading-5 text-neutral-700">
                          {segment.content.length > 300
                            ? `${segment.content.slice(0, 300)}…`
                            : segment.content}
                        </p>
                      </div>
                    ))}
                    <span className="text-[11px] text-neutral-400">
                      仅显示前 {detail.segments.length} 段（按文档顺序）
                    </span>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** formatSize 把字节数转成人类可读的大小。 */
function formatSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
