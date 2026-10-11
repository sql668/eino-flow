-- 0008_legacy_documents_draft：把「工作台之前入库」的文档改回待确认
--
-- 背景：改用分段工作台时，向量集合从 einoflow_segments 换成了 einoflow_segments_v2
-- （主键语义变了，无法原地改）。旧文档的向量留在旧集合里，检索已经看不到它们；
-- 同时它们没有解析产物（parsed_key 为空），也无法在工作台里重切。
--
-- 如果继续显示「已就绪」，界面会告诉使用者「这份文档能搜」，实际搜不到——
-- 这属于最误导人的那种状态。改成「待确认」后界面上会给出「解析」入口，
-- 重新解析 + 确认分段即可恢复可用。

UPDATE documents
SET status = 'draft',
    segment_count = 0,
    chunk_mode = '',
    error_message = ''
WHERE parsed_key = ''
  AND status = 'ready'
  AND deleted_at IS NULL;

-- 旧分段（无 chunk_id）已经不再有任何引用方：检索按 chunk_id 回查，
-- 它们的存在只会让「文档详情里有多少段」这类统计虚高。清掉。
DELETE FROM document_segments WHERE chunk_id = '';
