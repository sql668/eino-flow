"use client";

import { useEffect, useState } from "react";

// 读取系统的「减少动效」偏好。
//
// 用途：打字机、闪烁光标这类「持续动效」在开启该偏好时必须退化成瞬时呈现——
// 这是可达性的底线（见 web/AGENTS.md 第 4 节的自检项），不是可选装饰。
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) {
      return;
    }
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReduced(query.matches);
    const onChange = (event: MediaQueryListEvent) => setReduced(event.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  return reduced;
}
