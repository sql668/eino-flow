import { http, streamSse, uploadFile } from "@/lib/http";
import type {
  AppInfo,
  AppListResult,
  AppMode,
  ChatCitationsPayload,
  ChatDeltaPayload,
  ChatDonePayload,
  ChatErrorPayload,
  ConversationListResult,
  DatasetInfo,
  DatasetListResult,
  DocumentInfo,
  DocumentListResult,
  MessageListResult,
  SegmentListResult,
} from "@/types/api";

// 应用 / 知识库 / 文档 / 对话的接口层。
//
// 与 lib/api/models.ts 同样的定位：路径与出入参类型收敛在这里，
// 组件与 store 不拼路径、不写裸 fetch（见 web/AGENTS.md）。

// ---------- 应用 ----------

export interface CreateAppPayload {
  name: string;
  description?: string;
  mode: AppMode;
}

export const appsApi = {
  list: (page = 1, pageSize = 20, keyword?: string) =>
    http.get<AppListResult>("/api/v1/apps", { query: { page, pageSize, keyword } }),
  get: (id: number) => http.get<AppInfo>(`/api/v1/apps/${id}`),
  create: (payload: CreateAppPayload) => http.post<AppInfo>("/api/v1/apps", payload),
  remove: (id: number) => http.delete<null>(`/api/v1/apps/${id}`),
};

// ---------- 知识库 ----------

export interface CreateDatasetPayload {
  name: string;
  description?: string;
}

export const datasetsApi = {
  list: (page = 1, pageSize = 20, keyword?: string) =>
    http.get<DatasetListResult>("/api/v1/datasets", { query: { page, pageSize, keyword } }),
  get: (id: number) => http.get<DatasetInfo>(`/api/v1/datasets/${id}`),
  create: (payload: CreateDatasetPayload) => http.post<DatasetInfo>("/api/v1/datasets", payload),
  remove: (id: number) => http.delete<null>(`/api/v1/datasets/${id}`),
};

// ---------- 文档 ----------

export const documentsApi = {
  list: (datasetId: number, page = 1, pageSize = 50) =>
    http.get<DocumentListResult>(`/api/v1/datasets/${datasetId}/documents`, {
      query: { page, pageSize },
    }),
  // 上传走 multipart：后端字段名固定为 file（见 handler/document.go 的 uploadFormField）。
  upload: (datasetId: number, file: File) =>
    uploadFile<DocumentInfo>(`/api/v1/datasets/${datasetId}/documents`, file),
  get: (id: number) => http.get<DocumentInfo>(`/api/v1/documents/${id}`),
  remove: (id: number) => http.delete<null>(`/api/v1/documents/${id}`),
  segments: (documentId: number, page = 1, pageSize = 20) =>
    http.get<SegmentListResult>(`/api/v1/documents/${documentId}/segments`, {
      query: { page, pageSize },
    }),
};

// ---------- 会话与问答 ----------

export interface ChatStreamHandlers {
  onCitations: (payload: ChatCitationsPayload) => void;
  onDelta: (payload: ChatDeltaPayload) => void;
  onDone: (payload: ChatDonePayload) => void;
  onError: (payload: ChatErrorPayload) => void;
}

export interface ChatStreamInput {
  appId: number;
  question: string;
  conversationId?: number;
  datasetId?: number;
  topK?: number;
}

export const chatApi = {
  listConversations: (appId: number) =>
    http.get<ConversationListResult>("/api/v1/conversations", {
      query: { appId, page: 1, pageSize: 50 },
    }),
  listMessages: (conversationId: number) =>
    http.get<MessageListResult>(`/api/v1/conversations/${conversationId}/messages`, {
      query: { page: 1, pageSize: 100 },
    }),
  /**
   * 发起流式问答。
   *
   * 事件与后端 handler/chat.go 一一对应；这里只做分发，不持有状态——
   * 状态由调用方（store）决定怎么合并，接口层保持无状态便于测试与复用。
   */
  stream: async (input: ChatStreamInput, handlers: ChatStreamHandlers, signal?: AbortSignal) => {
    await streamSse(`/api/v1/apps/${input.appId}/chat`, {
      signal,
      body: {
        question: input.question,
        conversationId: input.conversationId ?? 0,
        datasetId: input.datasetId ?? 0,
        topK: input.topK ?? 0,
      },
      onEvent: (event) => {
        switch (event.event) {
          case "citations":
            handlers.onCitations(event.data as ChatCitationsPayload);
            break;
          case "delta":
            handlers.onDelta(event.data as ChatDeltaPayload);
            break;
          case "done":
            handlers.onDone(event.data as ChatDonePayload);
            break;
          case "error":
            handlers.onError(event.data as ChatErrorPayload);
            break;
          default:
            // 未知事件忽略而不是抛错：服务端将来加事件类型时，旧前端不该整条流崩掉。
            break;
        }
      },
    });
  },
};
