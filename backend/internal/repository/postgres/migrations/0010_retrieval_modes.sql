-- 0010_retrieval_modes：检索留痕记录「用了哪种检索方式」与两路原始分
--
-- 背景（按 2026-10-11 的方案 C）：
--   知识库的检索设置新增了检索方式（向量 / 全文 / 混合）与混合检索的融合方式，
--   同一个问题换个方式结果必然不同。0003/0005 只记了向量模型与重排模型，
--   缺了「方式」这一项，事后就无法解释「同样的问法为什么昨天搜得到今天搜不到」。
--
-- 同时把召回阶段拆开记：混合检索下排序分是两路融合分，
-- 只知道融合分无法判断一条命中是靠语义还是靠关键词进来的，因此补两路原始分。

ALTER TABLE retrieval_runs ADD COLUMN IF NOT EXISTS search_mode VARCHAR(16) NOT NULL DEFAULT 'vector';

COMMENT ON COLUMN retrieval_runs.search_mode IS '本次检索方式：vector 向量 / fulltext 全文 / hybrid 混合';

-- 两路原始分允许为空：向量检索没有全文分，反之亦然（0 是有意义的分数，不能拿它代替「不适用」）。
ALTER TABLE retrieval_hits ADD COLUMN IF NOT EXISTS vector_score REAL;

ALTER TABLE retrieval_hits ADD COLUMN IF NOT EXISTS fulltext_score REAL;

COMMENT ON COLUMN retrieval_hits.score IS '本阶段的排序分：单路检索是该路分数，混合检索是融合分，重排阶段是重排分';

COMMENT ON COLUMN retrieval_hits.vector_score IS '向量路原始分；该路未参与本次检索时为空';

COMMENT ON COLUMN retrieval_hits.fulltext_score IS '全文（BM25）路原始分；该路未参与本次检索时为空';

COMMENT ON COLUMN retrieval_hits.stage IS '命中发生的阶段：vector 向量召回 / fulltext 全文召回 / hybrid 混合召回 / rerank 重排';
