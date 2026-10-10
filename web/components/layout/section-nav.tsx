"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

// 二级导航面板（对齐 Dify 的「集成」面板）。
//
// Dify 的三栏结构是：全局导航 | 二级导航 | 内容。
// 二级导航承担「同一大类下的互斥页面」，抽出来之后每加一个设置页只需在这里加一项。

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
    <nav className="flex w-[200px] shrink-0 flex-col gap-0.5 border-r border-neutral-200 bg-white px-3 py-4">
      {sections.map((item) => {
        const active = pathname.startsWith(item.href);
        const className = [
          "flex items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors",
          active
            ? "bg-neutral-100 font-medium text-neutral-900"
            : "text-neutral-600 hover:bg-neutral-50",
          item.pending ? "cursor-not-allowed opacity-50" : "",
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
