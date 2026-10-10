"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

// 控制台外壳：左侧导航 + 主内容区。
//
// 结构对齐 Dify（本机 http://localhost 的界面）：顶部品牌 + 导航列表 + 分组标题 +
// 底部用户行，主内容区独立滚动。之所以先搭外壳而不是逐页加样式：
// 后面还有应用列表、知识库、对话调试等页面，导航必须只有一个来源。

interface NavItem {
  href: string;
  label: string;
  icon: ReactNode;
  /** 右侧小标签，例如 Dify 的 BETA */
  badge?: string;
  /** 尚未实现的入口：置灰并给出说明，避免点进去是空页面 */
  pending?: boolean;
}

// 图标用内联 SVG：项目还没引入图标库，为了一个导航再引一个依赖不划算。
const icon = (path: ReactNode) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    strokeLinejoin="round"
    className="h-4 w-4"
    aria-hidden="true"
  >
    {path}
  </svg>
);

const navItems: NavItem[] = [
  {
    href: "/",
    label: "主页",
    icon: icon(<path d="M3 10.5 12 3l9 7.5M5 9.5V21h14V9.5" />),
  },
  {
    href: "/apps",
    label: "应用",
    icon: icon(<rect x="3" y="3" width="7" height="7" rx="1.5" />),
  },
  {
    href: "/datasets",
    label: "知识库",
    icon: icon(<path d="M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3Zm0 0v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6" />),
  },
  {
    href: "/settings/models",
    label: "模型配置",
    icon: icon(<path d="M12 3v4m0 10v4M3 12h4m10 0h4M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Z" />),
  },
];

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  return (
    // h-full + overflow-hidden：外壳自身不滚动，滚动交给内部明确指定的区域。
    // 侧栏只在内容超高时滚动（导航项多了才会），主内容区由页面各自决定。
    <div className="flex h-full overflow-hidden bg-neutral-50 text-neutral-900">
      <aside className="flex w-[228px] shrink-0 flex-col overflow-y-auto border-r border-neutral-200 bg-white">
        <div className="flex h-14 items-center px-5">
          <Link href="/" className="text-lg font-semibold tracking-tight">
            EinoFlow
          </Link>
        </div>

        <nav className="flex flex-1 flex-col gap-0.5 px-3 py-2">
          {navItems.map((item) => {
            const active =
              item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            const className = [
              "flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors",
              active
                ? "bg-neutral-100 font-medium text-neutral-900"
                : "text-neutral-600 hover:bg-neutral-50",
              item.pending ? "cursor-not-allowed opacity-50" : "",
            ].join(" ");

            if (item.pending) {
              return (
                <span
                  key={item.href}
                  className={className}
                  title="该页面尚未开发（见 docs/PROGRESS.md 的阶段计划）"
                >
                  {item.icon}
                  {item.label}
                  <span className="ml-auto rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] text-neutral-500">
                    待开发
                  </span>
                </span>
              );
            }
            return (
              <Link key={item.href} href={item.href} className={className}>
                {item.icon}
                {item.label}
              </Link>
            );
          })}
        </nav>

        <div className="border-t border-neutral-200 px-4 py-3 text-xs text-neutral-500">
          V0.1 · 本地开发
        </div>
      </aside>

      <main className="flex min-h-0 min-w-0 flex-1 overflow-hidden">{children}</main>
    </div>
  );
}
