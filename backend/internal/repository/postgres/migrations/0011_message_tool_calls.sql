-- 0011_message_tool_calls：助手消息承载工具调用记录
--
-- 智能体问答的回答依据不只在引用里，还在「调用过什么工具、传入什么、拿到什么」。
-- 只记日志不够：用户刷新会话后看不到当时的调用过程，排障也无从复现。
-- 这里用 jsonb 数组存摘要，旧消息默认 []，读取不需要按消息类型分支。

ALTER TABLE messages ADD COLUMN IF NOT EXISTS tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN messages.tool_calls IS '助手消息的工具调用摘要（工具名、入参、结果、失败/跳过标记）';
