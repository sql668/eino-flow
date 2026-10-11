-- 0007_chunk_workbench：分段工作台（知识库级配置 + 解析产物 + 片段元数据）
--
-- 这次改动把「怎么切、怎么洗」从写死的全局配置变成知识库级配置，并让预览可反复调参。
-- 三块内容：
--   1) datasets 存分段/清洗配置与检索配置（JSONB）；
--   2) documents 保留解析产物引用（改动前解析结果不落库，导致改参数必须重新解析，
--      一份 PDF 要几十秒到几分钟，工作台根本没法用）；
--   3) document_segments 用业务生成的 chunk_id 与向量库对应，并补齐元数据。

ALTER TABLE datasets ADD COLUMN IF NOT EXISTS chunk_config JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE datasets ADD COLUMN IF NOT EXISTS retrieval_config JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN datasets.chunk_config IS '分段与清洗配置（模式/长度/重叠/清洗规则）';

COMMENT ON COLUMN datasets.retrieval_config IS '检索配置（TopK/召回数/阈值/重排开关/权重）';

-- 解析产物：IR 放在对象存储，这里只存 key；char_count 供文档列表展示体量。
ALTER TABLE documents ADD COLUMN IF NOT EXISTS parsed_key VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE documents ADD COLUMN IF NOT EXISTS char_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE documents ADD COLUMN IF NOT EXISTS chunk_mode VARCHAR(32) NOT NULL DEFAULT '';

COMMENT ON COLUMN documents.parsed_key IS '解析产物（IR JSON）在对象存储中的 key，供分段工作台反复预览';

COMMENT ON COLUMN documents.chunk_mode IS '最终采用的分段模式，用于文档列表展示';

-- 上传人：随分段写入向量库元数据，为「按人可见」的权限过滤预留。
ALTER TABLE documents ADD COLUMN IF NOT EXISTS user_id VARCHAR(64) NOT NULL DEFAULT '';

COMMENT ON COLUMN documents.user_id IS '上传人标识（V0.1 无用户体系，先记占位值）';

-- 片段：chunk_id 是与向量库对应的业务主键
ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS chunk_id VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS char_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS overlap_runes INTEGER NOT NULL DEFAULT 0;

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS content_hash VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS chunk_config_version VARCHAR(32) NOT NULL DEFAULT '';

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS user_id VARCHAR(64) NOT NULL DEFAULT '';

COMMENT ON COLUMN document_segments.chunk_id IS '业务生成的片段标识（UUID），同时是向量库主键；检索命中后按它回查本表';

COMMENT ON COLUMN document_segments.overlap_runes IS '本片段开头与前一片段重叠的字符数（0 表示无重叠）';

COMMENT ON COLUMN document_segments.content_hash IS '正文哈希，用于将来做增量重切';

COMMENT ON COLUMN document_segments.chunk_config_version IS '切分时使用的配置指纹';

COMMENT ON COLUMN document_segments.user_id IS '上传人标识，作为向量库元数据用于按人过滤';

-- 检索回查改走 chunk_id，必须有唯一索引（否则回查会退化成全表扫描）。
--
-- 用**部分唯一索引**（WHERE chunk_id <> ''）而不是全表唯一索引：
-- 历史分段是在引入 chunk_id 之前入库的，值是空串；全表唯一索引会因为多行空串直接建不起来，
-- 迁移就会在已有数据的库上失败。空值本来也不参与回查（回查是按具体 chunk_id 取的），
-- 所以只约束非空值的语义是对的，同时让迁移对老数据保持幂等。
CREATE UNIQUE INDEX IF NOT EXISTS uk_document_segments_chunk_id
  ON document_segments (chunk_id) WHERE chunk_id <> '';

-- vector_id 的语义已被 chunk_id 取代，留着只会误导后来者：它存的是旧方案下
-- 「分段自增 ID 的文本形式」，没有独立信息，且新流程不再写入。
ALTER TABLE document_segments DROP COLUMN IF EXISTS vector_id;
