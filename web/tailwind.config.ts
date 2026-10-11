import type { Config } from "tailwindcss";

/**
 * 设计 token（见 web/AGENTS.md「视觉方向」）。
 *
 * 方向：工程图纸 + 仪表读数。
 *   - 冷纸底 + 网格线，而不是默认的白卡片 + 灰底；
 *   - 唯一饱和色（蓝图蓝）只给交互与主操作，其他颜色都是「读数色」；
 *   - 圆角收到 2~3px：这是控制台，不是消费级应用；
 *   - 数字一律用等宽字体 + tabular-nums（像仪表读数那样对齐）。
 *
 * 改色前先想清楚：这里的每个色都是**角色**（结构线 / 纸 / 墨 / 读数），
 * 不是「好看的颜色表」。要加新色，先说明它是哪个角色。
 */
const config: Config = {
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./features/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        // 结构与文字
        ink: {
          DEFAULT: "#101828",
          muted: "#5A6478",
          soft: "#8A93A5",
        },
        // 纸与网格（图纸感的来源）
        paper: "#F2F4F8",
        grid: {
          DEFAULT: "#D8DEE9",
          soft: "#E8EDF4",
        },
        // 唯一饱和色：交互与主操作
        pri: {
          DEFAULT: "#2A4FD7",
          ink: "#1B37A0",
          soft: "#EBF0FE",
        },
        // 读数色（状态），刻意不用 Tailwind 默认的 emerald/amber/red
        ok: { DEFAULT: "#17835A", soft: "#E7F4EE" },
        warn: { DEFAULT: "#A9660A", soft: "#FBF1E3" },
        fault: { DEFAULT: "#B3261E", soft: "#FCECEA" },
      },
      borderRadius: {
        // 控制台的锐度：默认 3px，大面板 4px，胶囊只给状态标签
        none: "0",
        DEFAULT: "3px",
        md: "3px",
        lg: "4px",
        xl: "4px",
        sm: "2px",
      },
      fontFamily: {
        sans: ["var(--font-geist-sans)", "system-ui", "sans-serif"],
        mono: ["var(--font-geist-mono)", "ui-monospace", "monospace"],
      },
      fontSize: {
        // 标注字（区块眉标）：大写 + 字距，靠这条规则统一
        label: ["11px", { lineHeight: "16px", letterSpacing: "0.14em" }],
      },
      backgroundImage: {
        // 图纸网格：24px 一格，极淡，只在主内容区铺一层
        blueprint:
          "linear-gradient(to right, rgba(16,24,40,0.045) 1px, transparent 1px), linear-gradient(to bottom, rgba(16,24,40,0.045) 1px, transparent 1px)",
      },
      backgroundSize: {
        blueprint: "24px 24px",
      },
    },
  },
  plugins: [],
};
export default config;
