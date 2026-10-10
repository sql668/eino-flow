"use client";

import { create } from "zustand";

/**
 * 全局 UI 状态。
 *
 * 约定（见 web/AGENTS.md）：跨组件共享的状态统一放 Zustand，
 * 组件内部的局部状态用 useState，派生数据即时计算、不要放进 store。
 */
interface AppState {
  /** 侧边导航是否折叠 */
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
}

export const useAppStore = create<AppState>((set) => ({
  sidebarCollapsed: false,
  toggleSidebar: () =>
    set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
}));
