import type { ReactNode } from "react";

import { SectionNav } from "@/components/layout/section-nav";

// 设置区的二级导航布局。
// 与全局导航叠加成 Dify 那样的三栏结构：全局导航 | 设置导航 | 内容。
export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen">
      <SectionNav />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
