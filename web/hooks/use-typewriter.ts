"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import { usePrefersReducedMotion } from "@/hooks/use-prefers-reduced-motion";

// 打字机：把「已经收到的完整文本」按稳定的节奏逐字揭示出来。
//
// 为什么流式之外还要打字机：模型的增量是**成块**到的（实测每块 1~4 个字，
// 但长回答或快模型会一次给几十个字），逐块渲染会一顿一顿地跳；
// 打字机把「块」抹平成连续的字流，观感才是「在写」而不是「在闪」。
//
// 两个关键约束：
//   - **不能落后于模型**：积压越多揭示越快（上限是积压的 1/3），
//     否则长回答会出现「模型早写完了，界面还在慢慢打」；
//   - **尊重减少动效**：系统开启后直接呈现，不做逐字动画。

export interface TypewriterOptions {
  /** 基础揭示速度（字/秒）。默认 60，接近人类快速阅读的节奏。 */
  charsPerSecond?: number;
  /** 是否启用逐字动画（false 时直接显示全文，用于历史消息）。 */
  enabled?: boolean;
}

export interface TypewriterResult {
  /** 当前应当显示的文本（是 target 的前缀）。 */
  visible: string;
  /** 是否还在逐字揭示（调用方据此决定是否显示光标）。 */
  typing: boolean;
}

export function useTypewriter(target: string, options: TypewriterOptions = {}): TypewriterResult {
  const { charsPerSecond = 60, enabled = true } = options;
  const reducedMotion = usePrefersReducedMotion();

  // 按「码点」切分而不是按 UTF-16 下标：否则会把 emoji 之类的代理对切成半个字符。
  const chars = useMemo(() => Array.from(target), [target]);
  const total = chars.length;
  const instant = !enabled || reducedMotion;

  const [count, setCount] = useState(() => (instant ? total : 0));
  // 用 ref 读取最新长度：动画循环不因为 target 变化而重建（重建会抖）。
  const totalRef = useRef(total);
  totalRef.current = total;
  const speedRef = useRef(charsPerSecond);
  speedRef.current = charsPerSecond;

  useEffect(() => {
    if (instant) {
      setCount(totalRef.current);
      return;
    }
    let frame = 0;
    let last = performance.now();
    const tick = (now: number) => {
      // 标签页切回来时 dt 会很大：夹住上限，否则会一次吐掉半篇。
      const dt = Math.min((now - last) / 1000, 0.2);
      last = now;
      setCount((current) => {
        const target = totalRef.current;
        if (current >= target) {
          return target;
        }
        const backlog = target - current;
        const base = Math.max(1, Math.round(speedRef.current * dt));
        // 追赶项：积压 30 字时每帧额外多揭示约 3 字，几十帧内追平。
        const catchUp = Math.ceil(backlog * 0.1);
        return Math.min(target, current + Math.max(base, catchUp));
      });
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [instant]);

  // target 变短（换了一条消息）时把进度拉回合法范围。
  useEffect(() => {
    setCount((current) => Math.min(current, total));
  }, [total]);

  const visible = instant ? target : chars.slice(0, Math.min(count, total)).join("");
  return { visible, typing: !instant && count < total };
}
