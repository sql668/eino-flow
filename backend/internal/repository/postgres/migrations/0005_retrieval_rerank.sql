-- 0005_retrieval_rerank：检索留痕补上「重排」这一阶段所需的字段
--
-- 0003 建表时已经预留了 retrieval_hits.stage 与 rerank_score，
-- 但 retrieval_runs 还缺两项信息，无法解释「同一次检索为什么两次结果不同」：
--   * candidate_count —— 重排前召回了几条（没有它就无法还原两阶段过程）；
--   * rerank_model    —— 用了哪个重排模型（换模型后结果必然变化，不记录就查不出原因）。

ALTER TABLE retrieval_runs ADD COLUMN IF NOT EXISTS candidate_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE retrieval_runs ADD COLUMN IF NOT EXISTS rerank_model VARCHAR(128) NOT NULL DEFAULT '';

COMMENT ON COLUMN retrieval_runs.candidate_count IS '重排前的向量召回条数；未启用重排时等于命中数';

COMMENT ON COLUMN retrieval_runs.rerank_model IS '本次重排使用的模型；为空表示未做重排';
