import { http, streamSse, uploadFile } from "@/lib/http";
import type {
  AppInfo,
  ChunkConfig,
  ChunkOptions,
  PreviewResult,
  RetrievalConfig,
  AppListResult,
  AppMode,
  ChatCitationsPayload,
  ChatDeltaPayload,
  ChatDonePayload,
  ChatErrorPayload,
  ChatReasoningPayload,
  ChatToolCallPayload,
  ConversationListResult,
  DatasetInfo,
  DatasetListResult,
  DocumentInfo,
  DocumentListResult,
  MessageListResult,
  RetrievalRunInfo,
  RetrievalRunListResult,
  RecallTestPayload,
  SearchResult,
  SegmentInfo,
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

export interface IngestPayload {
  config: ChunkConfig;
  /** 把这份配置写回知识库默认，下次上传自动带出来 */
  saveAsDefault?: boolean;
}

export const chunkApi = {
  /** 分段选项：模式、清洗规则、默认配置、取值边界 */
  options: (datasetId: number) =>
    http.get<ChunkOptions>(`/api/v1/datasets/${datasetId}/chunk-options`),
  getChunkConfig: (datasetId: number) =>
    http.get<ChunkConfig>(`/api/v1/datasets/${datasetId}/chunk-config`),
  saveChunkConfig: (datasetId: number, config: ChunkConfig) =>
    http.put<ChunkConfig>(`/api/v1/datasets/${datasetId}/chunk-config`, { config }),
  getRetrievalConfig: (datasetId: number) =>
    http.get<RetrievalConfig>(`/api/v1/datasets/${datasetId}/retrieval-config`),
  saveRetrievalConfig: (datasetId: number, config: RetrievalConfig) =>
    http.put<RetrievalConfig>(`/api/v1/datasets/${datasetId}/retrieval-config`, config),
  /** 检索历史：按知识库 + 时间范围分页（默认最近记录）。 */
  listRetrievalRuns: (datasetId: number, params?: { from?: string; to?: string; page?: number; pageSize?: number }) =>
    http.get<RetrievalRunListResult>(`/api/v1/datasets/${datasetId}/retrieval-runs`, {
      query: {
        from: params?.from,
        to: params?.to,
        page: params?.page ?? 1,
        pageSize: params?.pageSize ?? 20,
      },
    }),
  getRetrievalRun: (datasetId: number, runId: number) =>
    http.get<RetrievalRunInfo>(`/api/v1/datasets/${datasetId}/retrieval-runs/${runId}`),
  /** 召回测试：参数只在本次请求生效，不改知识库配置。 */
  recallTest: (datasetId: number, payload: RecallTestPayload) =>
    http.post<SearchResult>(`/api/v1/datasets/${datasetId}/recall-tests`, payload),

  /** 预览分段结果：不写库，可以反复调参 */
  preview: (documentId: number, config: ChunkConfig) =>
    http.post<PreviewResult>(`/api/v1/documents/${documentId}/preview`, { config }),
  /** 按配置入库（异步，202 后轮询文档状态） */
  ingest: (documentId: number, payload: IngestPayload) =>
    http.post<null>(`/api/v1/documents/${documentId}/ingest`, payload),
  /** 用知识库默认配置重切 */
  rechunk: (documentId: number) => http.post<null>(`/api/v1/documents/${documentId}/rechunk`),
  /** 重新解析（旧文档没有解析产物，或解析失败时用） */
  reparse: (documentId: number) => http.post<null>(`/api/v1/documents/${documentId}/parse`),
};

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
  /**
   * 覆盖单个分段的关键词。
   *
   * 覆盖式（传什么就是什么）而不是增量：界面上是一个可增删的标签列表，
   * 增量语义（加一个 / 删一个）在这里没有额外价值，反而容易与并发编辑打架。
   */
  updateSegmentKeywords: (documentId: number, segmentId: number, keywords: string[]) =>
    http.put<SegmentInfo>(`/api/v1/documents/${documentId}/segments/${segmentId}/keywords`, {
      keywords,
    }),
};

// ---------- 会话与问答 ----------

export interface ChatStreamHandlers {
  onCitations: (payload: ChatCitationsPayload) => void;
  /** 思考过程增量（推理模型才有；普通模型不会触发）。 */
  onReasoning: (payload: ChatReasoningPayload) => void;
  /** 工具调用开始 / 结果（智能体形态才有；顺序即轮次）。 */
  onToolCall: (payload: ChatToolCallPayload) => void;
  onToolResult: (payload: ChatToolCallPayload) => void;
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
          case "reasoning":
            handlers.onReasoning(event.data as ChatReasoningPayload);
            break;
          case "tool_call":
            handlers.onToolCall(event.data as ChatToolCallPayload);
            break;
          case "tool_result":
            handlers.onToolResult(event.data as ChatToolCallPayload);
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
