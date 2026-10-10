import type { ReactNode } from "react";

import { SectionNav } from "@/components/layout/section-nav";

// 设置区的二级导航布局。
// 与全局导航叠加成 Dify 那样的三栏结构：全局导航 | 设置导航 | 内容。
export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    // 二级导航与全局侧栏一样固定不动，只有右侧内容区滚动（见 web/AGENTS.md「滚动」）。
    <div className="flex h-full min-h-0 w-full">
      <SectionNav />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
    </div>
  );
}
