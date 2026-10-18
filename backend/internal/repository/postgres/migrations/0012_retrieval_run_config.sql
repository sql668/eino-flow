-- 0012_retrieval_run_config：检索留痕记录完整参数快照
--
-- 现有列只有检索方式、TopK、候选数与重排模型，缺阈值、融合方式与两路权重。
-- 检索历史页要回答"当时用了什么参数"，这些字段必须一起留痕。
-- 用 JSONB 而不是继续加散列：检索参数还会增加，快照结构可以独立演进。

ALTER TABLE retrieval_runs ADD COLUMN IF NOT EXISTS config_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN retrieval_runs.config_snapshot IS '本次检索的完整参数快照；旧记录为空对象表示当时未记录';
