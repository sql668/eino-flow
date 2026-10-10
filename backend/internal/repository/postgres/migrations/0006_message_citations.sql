-- 0006_message_citations：助手消息记录「引用了哪些片段」与对应的检索记录
--
-- 背景（PRD：溯源引用，降低模型幻觉）：
--   * citations —— 回答里 [1][2] 这些编号对应哪些片段、来自哪份文档哪一页。
--     不回填的话，历史会话里的答案就变成无源之说，用户无法核对；
--   * retrieval_run_id —— 指向 retrieval_runs，可以回看当时的召回与重排过程
--     （为什么给模型看了这几段，而不是别的）。
--
-- citations 用 jsonb 而不是另建关联表：它的用途是「随消息一起展示」，
-- 不需要按片段反查消息；为展示型数据建表只会增加一次 JOIN。

ALTER TABLE messages ADD COLUMN IF NOT EXISTS citations JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE messages ADD COLUMN IF NOT EXISTS retrieval_run_id BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN messages.citations IS '助手消息引用的片段（jsonb 数组），与回答里的 [n] 编号对应';

COMMENT ON COLUMN messages.retrieval_run_id IS '对应的检索记录 ID（retrieval_runs），0 表示本轮未检索';
