-- 0009_segment_keywords：分段的自动关键词
--
-- 背景（按 2026-10-11 的方案 C）：
--   * 入库时用 Milvus 的中文分析器对每个分段分词，再做文档内 TF-IDF 抽出最多 5 个关键词；
--   * 关键词既在界面上可见可改，也是向量库里 BM25 稀疏字段的**第二个输入字段**——
--     也就是说人工调整关键词会真实影响全文/混合检索的召回，与 Dify 的关键词表同源思路，
--     但底座是真 BM25（带词频与 IDF）而不是「命中几个词」。
--
-- 存 JSONB 数组：关键词是展示型数据（随片段一起读），不需要按词反查片段。

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS keywords JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN document_segments.keywords IS '分段关键词（自动生成，最多 5 个，可人工修改）；同时作为向量库 BM25 的输入字段之一';
