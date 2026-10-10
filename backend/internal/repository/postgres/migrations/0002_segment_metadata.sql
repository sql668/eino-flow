-- 0002_segment_metadata：给分段补上溯源与上下文信息
--
-- 背景：切片时必须记录「这段来自哪一页、原文哪个块、属于哪个标题路径」，
-- 否则无法实现引用溯源（PRD 要求「溯源引用，降低模型幻觉」）。
-- title_path 用 jsonb 存结构化数组而不是拼接字符串：
-- 将来要按标题路径过滤或统计时可以直接查，不用解析字符串——这正是选 PostgreSQL 的收益之一。

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS page INTEGER NOT NULL DEFAULT 0;

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS locator VARCHAR(128) NOT NULL DEFAULT '';

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS kind VARCHAR(32) NOT NULL DEFAULT 'text';

ALTER TABLE document_segments ADD COLUMN IF NOT EXISTS title_path JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN document_segments.page IS '分段起始页码，从 1 开始';

COMMENT ON COLUMN document_segments.locator IS '分段起始块定位串，形如 page:3/block:12';

COMMENT ON COLUMN document_segments.kind IS '分段主要类型：text / table';

COMMENT ON COLUMN document_segments.title_path IS '所属标题路径，JSON 数组';
