import type { ReactNode } from "react";

// 页面内容容器：**唯一**允许滚动的层级。
//
// 布局约定（见 web/AGENTS.md「滚动」）：
//   html/body 不滚动 → AppShell 不滚动 → 侧栏与二级导航固定 → 只有本容器滚动。
// 横向一律不滚动（overflow-x-hidden）：横向滚动条在控制台里几乎总是布局问题
// （比如某个 flex 子项没有 min-w-0），应当修布局而不是允许横向滚动把问题藏起来。
export function PageContainer({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`h-full min-h-0 w-full overflow-y-auto overflow-x-hidden ${className}`}>
      {children}
    </div>
  );
}
