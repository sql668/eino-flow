"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

// 控制台外壳：左侧导航 + 主内容区。
//
// 视觉方向（见 web/AGENTS.md「视觉方向」）：工程图纸 + 仪表读数。
//   - 左侧是「墨」：深墨底、纸色字，和右侧的纸底形成明确分区；
//     控制台里导航长期占位，压深一点能让内容区的读数更醒目。
//   - 主内容区铺一层极淡的图纸网格，给整页定调（背景材质，不是装饰图案）。
//   - 当前项用一条竖向蓝图蓝标记，而不是整块高亮：标记比填充更克制，
//     也和「读数用色节制」的原则一致。

interface NavItem {
  href: string;
  label: string;
  icon: ReactNode;
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
    <div className="flex h-full overflow-hidden">
      <aside className="flex w-[216px] shrink-0 flex-col overflow-y-auto bg-ink text-white/70">
        <div className="flex h-14 items-center gap-2 px-5">
          <span className="h-2 w-2 rounded-sm bg-pri" aria-hidden="true" />
          <Link href="/" className="text-[15px] font-semibold tracking-tight text-white">
            EinoFlow
          </Link>
        </div>

        <span className="eyebrow px-5 pb-2 !text-white/35">导航</span>

        <nav className="flex flex-1 flex-col gap-0.5 px-2.5">
          {navItems.map((item) => {
            const active =
              item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            const className = [
              "relative flex items-center gap-2.5 rounded px-2.5 py-2 text-[13px] transition-colors",
              active ? "bg-white/8 text-white" : "hover:bg-white/5 hover:text-white/90",
              item.pending ? "cursor-not-allowed opacity-45" : "",
            ].join(" ");

            const marker = active ? (
              <span
                className="absolute -left-2.5 top-1.5 h-[18px] w-[2px] bg-pri"
                aria-hidden="true"
              />
            ) : null;

            if (item.pending) {
              return (
                <span
                  key={item.href}
                  className={className}
                  title="该页面尚未开发（见 docs/PROGRESS.md 的阶段计划）"
                >
                  {item.icon}
                  {item.label}
                  <span className="ml-auto rounded-sm bg-white/10 px-1 py-0.5 text-[10px] text-white/50">
                    待开发
                  </span>
                </span>
              );
            }
            return (
              <Link key={item.href} href={item.href} className={className}>
                {marker}
                {item.icon}
                {item.label}
              </Link>
            );
          })}
        </nav>

        <div className="border-t border-white/10 px-5 py-3">
          <span className="reading text-[11px] text-white/40">V0.1 · 本地开发</span>
        </div>
      </aside>

      <main className="blueprint-grid flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        {children}
      </main>
    </div>
  );
}
