"use client";

import { memo, useEffect } from "react";

import { useTypewriter } from "@/hooks/use-typewriter";

// 打字机文本：把流式收到的回答逐字揭示出来，并在揭示期间显示光标。
//
// 单独抽成组件（而不是在页面上直接调 hook）有两个原因：
//   - 动画状态只让这一个子树重渲染，消息列表与输入框不受每帧刷新影响；
//   - 「光标何时该消失」这类细节只需要写一次。

export interface TypewriterTextProps {
  /** 当前已收到的完整文本（流式过程中会不断变长）。 */
  text: string;
  /**
   * 流是否已结束。结束不等于「可以停」：缓冲区里可能还有没揭示完的文字，
   * 停早了会表现为「最后一段突然蹦出来」。
   */
  streamFinished: boolean;
  /** 每帧揭示后回调一次，供页面把视图钉在底部。 */
  onReveal?: () => void;
  /**
   * 是否启用逐字揭示。
   *
   * 本次会话新产生的回答才逐字揭示；历史消息（重新挂载时已存在的文本）
   * 直接呈现——否则每次进页面都要「重打」一遍旧回答，那是噪声不是效果。
   */
  animate?: boolean;
}

function TypewriterTextInner({ text, streamFinished, onReveal, animate = true }: TypewriterTextProps) {
  const { visible, typing } = useTypewriter(text, { enabled: animate });

  useEffect(() => {
    if (typing) {
      onReveal?.();
    }
  }, [visible, typing, onReveal]);

  return (
    <>
      <span className="whitespace-pre-wrap">{visible}</span>
      {typing ? (
        // 光标用方块而不是竖线：与「工程图纸」的标注风格一致，也更显眼。
        <span
          aria-hidden="true"
          className="ml-0.5 inline-block h-4 w-[6px] animate-pulse bg-pri align-text-bottom"
        />
      ) : null}
      {/* 逐字揭示期间对读屏软件静默（否则每帧都会被念一遍），
          结束后整体朗读一次。 */}
      <span className="sr-only" aria-live={streamFinished && !typing ? "polite" : "off"}>
        {streamFinished && !typing ? text : ""}
      </span>
    </>
  );
}

export const TypewriterText = memo(TypewriterTextInner);
