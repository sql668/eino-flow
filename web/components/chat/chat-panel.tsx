"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Field, Select } from "@/components/ui/field";
import { chatApi, datasetsApi } from "@/lib/api/console";
import { ApiError } from "@/lib/http";
import type { AppInfo, CitationInfo, DatasetInfo } from "@/types/api";

// 对话调试面板：流式渲染 + 引用来源。
//
// 状态放在组件内而不是全局 store：对话是「一个页面一份」的状态，
// 放进全局反而要额外处理「切应用时清空」；项目规则要求的是跨组件共享状态用 Zustand。
// 消息区是本页唯一滚动区域（web/AGENTS.md 第 4 节）。

interface ChatMessage {
  /** 本地临时消息没有落库 ID，用负数区分，避免与后端 ID 冲突。 */
  id: number;
  role: "user" | "assistant";
  content: string;
  citations?: CitationInfo[];
  streaming?: boolean;
  latencyMs?: number;
  model?: string;
}

export function ChatPanel({ app }: { app: AppInfo }) {
  const [conversationId, setConversationId] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [datasets, setDatasets] = useState<DatasetInfo[]>([]);
  const [datasetId, setDatasetId] = useState(0);
  const [question, setQuestion] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const localIdRef = useRef(-1);
  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const needDataset = app.mode === "rag";

  // RAG 形态必须先选知识库：后端在没有 datasetId 时会直接拒绝（见 service/chat.go）。
  useEffect(() => {
    if (!needDataset) {
      return;
    }
    datasetsApi
      .list(1, 50)
      .then((result) => {
        setDatasets(result.items);
        if (result.items.length > 0) {
          setDatasetId(result.items[0].id);
        }
      })
      .catch(() => setError("加载知识库列表失败"));
  }, [needDataset]);

  // 有新内容就滚到底部：对话页不滚就看不到最新回答。
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages]);

  useEffect(() => () => abortRef.current?.abort(), []);

  const handleSend = useCallback(async () => {
    const text = question.trim();
    if (text === "") {
      return;
    }
    if (needDataset && datasetId === 0) {
      setError("知识库问答需要先选择一个知识库");
      return;
    }

    setError("");
    setQuestion("");
    setSending(true);

    // 用户消息与占位的助手消息一起入列：助手消息随后续增量更新，
    // 避免「先空白、答完突然出现」的观感。
    const assistantId = localIdRef.current--;
    setMessages((prev) => [
      ...prev,
      { id: localIdRef.current--, role: "user", content: text },
      { id: assistantId, role: "assistant", content: "", streaming: true },
    ]);

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      await chatApi.stream(
        { appId: app.id, question: text, conversationId, datasetId: needDataset ? datasetId : 0 },
        {
          onCitations: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId ? { ...item, citations: payload.citations } : item,
              ),
            ),
          onDelta: (payload) =>
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? { ...item, content: item.content + payload.content }
                  : item,
              ),
            ),
          onDone: (payload) => {
            setConversationId(payload.conversationId);
            setMessages((prev) =>
              prev.map((item) =>
                item.id === assistantId
                  ? {
                      ...item,
                      streaming: false,
                      latencyMs: payload.latencyMs,
                      model: payload.model,
                    }
                  : item,
              ),
            );
          },
          onError: (payload) => {
            setError(payload.message);
            setMessages((prev) =>
              prev.map((item) => (item.id === assistantId ? { ...item, streaming: false } : item)),
            );
          },
        },
        controller.signal,
      );
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "发送失败");
      setMessages((prev) =>
        prev.map((item) => (item.id === assistantId ? { ...item, streaming: false } : item)),
      );
    } finally {
      setSending(false);
      abortRef.current = null;
    }
  }, [app.id, conversationId, datasetId, needDataset, question]);

  const handleNewConversation = () => {
    abortRef.current?.abort();
    setConversationId(0);
    setMessages([]);
    setError("");
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 px-8 py-6">
      <header className="flex flex-wrap items-center gap-3">
        <div className="flex flex-col">
          <h1 className="text-xl font-semibold tracking-tight">{app.name}</h1>
          <span className="text-xs text-neutral-500">
            {needDataset
              ? "知识库问答：先检索再回答，回答带来源引用"
              : "对话：直接回答，不检索"}
            {conversationId > 0 ? ` · 会话 #${conversationId}` : ""}
          </span>
        </div>
        {needDataset ? (
          <div className="w-56">
            <Field label="知识库" hint="检索范围；回答里的 [n] 对应这里的第 n 条命中。">
              <Select
                value={datasetId}
                onChange={(event) => setDatasetId(Number(event.target.value))}
              >
                {datasets.length === 0 ? <option value={0}>（还没有知识库）</option> : null}
                {datasets.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </Select>
            </Field>
          </div>
        ) : null}
        <div className="ml-auto">
          <Button onClick={handleNewConversation}>新会话</Button>
        </div>
      </header>

      {error ? (
        <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto rounded-xl border border-neutral-200 bg-white px-4 py-4">
        {messages.length === 0 ? (
          <p className="m-auto text-xs text-neutral-400">
            输入问题开始对话
            {needDataset ? "（回答会给出引用的文档与页码）" : ""}
          </p>
        ) : null}
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        <div ref={bottomRef} />
      </div>

      <div className="flex items-end gap-3">
        <div className="flex-1">
          <Field label="提问" hint="Enter 发送，Shift+Enter 换行。">
            <textarea
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  void handleSend();
                }
              }}
              rows={2}
              placeholder="例如：用万用表测直流电压时要注意什么？"
              className="w-full rounded-md border border-neutral-300 px-3 py-2 text-sm outline-none transition-colors focus:border-neutral-500"
            />
          </Field>
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="h-4" aria-hidden="true" />
          <Button
            variant="primary"
            busy={sending}
            disabled={question.trim() === ""}
            onClick={() => void handleSend()}
          >
            发送
          </Button>
        </div>
      </div>
    </div>
  );
}

/** MessageBubble 渲染单条消息；助手消息额外展示引用来源与耗时。 */
function MessageBubble({ message }: { message: ChatMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] rounded-xl bg-neutral-900 px-3.5 py-2.5 text-sm leading-6 text-white">
          {message.content}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="max-w-[90%] self-start rounded-xl bg-neutral-50 px-3.5 py-2.5 text-sm leading-6 text-neutral-800">
        {message.content === "" && message.streaming ? (
          <span className="text-neutral-400">正在检索并组织回答…</span>
        ) : (
          <span className="whitespace-pre-wrap">{message.content}</span>
        )}
        {message.streaming && message.content !== "" ? (
          <span className="ml-0.5 inline-block h-4 w-1 animate-pulse bg-neutral-400 align-middle" />
        ) : null}
      </div>

      {message.citations && message.citations.length > 0 ? (
        <div className="flex flex-col gap-1.5">
          <span className="text-[11px] text-neutral-500">
            引用来源（{message.citations.length} 条）
          </span>
          <div className="flex flex-wrap gap-2">
            {message.citations.map((citation) => (
              <span
                key={`${citation.index}-${citation.segmentId}`}
                className="rounded-md border border-neutral-200 bg-white px-2 py-1 text-[11px] text-neutral-600"
                title={citationTooltip(citation)}
              >
                [{citation.index}] {citation.documentName}
                {citation.page > 0 ? ` · 第 ${citation.page} 页` : ""}
                {citation.titlePath.length > 0 ? ` · ${citation.titlePath.join("/")}` : ""}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {message.latencyMs !== undefined ? (
        <span className="text-[11px] text-neutral-400">
          耗时 {message.latencyMs} ms{message.model ? ` · ${message.model}` : ""}
        </span>
      ) : null}
    </div>
  );
}

/** citationTooltip 把两阶段分数放进悬浮提示：需要核对召回质量时才看，平时不占地方。 */
function citationTooltip(citation: CitationInfo): string {
  const parts = [`片段 #${citation.segmentId}`, `向量分 ${citation.score.toFixed(4)}`];
  if (citation.rerankScore !== undefined) {
    parts.push(`重排分 ${citation.rerankScore.toFixed(4)}`);
  }
  return parts.join("｜");
}
