"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

// 二级导航面板（设置区）。
// 与全局导航的区别：这里是纸底 + 墨字（全局导航是墨底），形成「导航层级」的视觉分层。

interface SectionItem {
  href: string;
  label: string;
  /** 未实现的入口置灰，避免点进去是空白页 */
  pending?: boolean;
}

const sections: SectionItem[] = [
  { href: "/settings/models", label: "模型供应商" },
  { href: "/settings/tools", label: "工具", pending: true },
  { href: "/settings/defaults", label: "默认模型设置", pending: true },
  { href: "/settings/eino", label: "编排内核（Eino）", pending: true },
];

export function SectionNav() {
  const pathname = usePathname();

  return (
    <nav className="flex w-[188px] shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-grid bg-white px-2.5 py-4">
      <span className="eyebrow px-2.5 pb-2">设置</span>
      {sections.map((item) => {
        const active = pathname.startsWith(item.href);
        const className = [
          "relative flex items-center justify-between rounded px-2.5 py-2 text-[13px] transition-colors",
          active ? "bg-pri-soft font-medium text-pri-ink" : "text-ink-muted hover:bg-paper hover:text-ink",
          item.pending ? "cursor-not-allowed opacity-45" : "",
        ].join(" ");

        if (item.pending) {
          return (
            <span key={item.href} className={className} title="该页面尚未开发">
              {item.label}
            </span>
          );
        }
        return (
          <Link key={item.href} href={item.href} className={className}>
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
