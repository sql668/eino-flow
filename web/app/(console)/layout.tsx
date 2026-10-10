import type { ReactNode } from "react";

import { AppShell } from "@/components/layout/app-shell";

// 控制台路由组。
//
// layout.tsx 只负责挂外壳，不持有任何业务状态：
// 状态在各自的 Zustand store 里，页面按需读取（见 web/AGENTS.md）。
export default function ConsoleLayout({ children }: { children: ReactNode }) {
  return <AppShell>{children}</AppShell>;
}
