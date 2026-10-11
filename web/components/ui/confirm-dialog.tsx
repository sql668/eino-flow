"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

import { Button } from "@/components/ui/button";

// 二次确认弹窗。
//
// 为什么不用 window.confirm：它是浏览器原生弹窗，样式无法控制（在这个「工程图纸」
// 视觉体系里格外突兀），文案只能一行纯文本，也不能标明「删除什么」。
//
// 用法（把「弹窗状态」和「确认后要做的事」分开，调用处只关心一件事）：
//
//	const { confirm, dialog } = useConfirm();
//	...
//	if (await confirm({ title: "删除文档「X」？", tone: "danger" })) { await remove(); }
//	...
//	{dialog}
//
// 走 portal 渲染到 body：页面里存在 `overflow: hidden` 的滚动容器，
// 就地渲染的浮层会被裁掉。

export interface ConfirmOptions {
	/** 标题：写清「要对什么做什么」，不要在标题里只写「提示」 */
	title: string;
	/** 说明后果（不可恢复 / 会影响什么），可给 JSX 以突出关键词 */
	description?: ReactNode;
	/** 确认按钮文案：与标题里的动作同名（删除 → 删除），不要用「确定」 */
	confirmLabel?: string;
	cancelLabel?: string;
	/** danger 用读数红，用于删除这类破坏性操作 */
	tone?: "default" | "danger";
}

export interface ConfirmDialogProps extends ConfirmOptions {
	open: boolean;
	onConfirm: () => void;
	onCancel: () => void;
}

export function ConfirmDialog({
	open,
	title,
	description,
	confirmLabel = "确认",
	cancelLabel = "取消",
	tone = "default",
	onConfirm,
	onCancel,
}: ConfirmDialogProps) {
	const panelRef = useRef<HTMLDivElement | null>(null);

	useEffect(() => {
		if (!open) {
			return;
		}
		// Esc 取消 + Tab 焦点锁在弹窗内：弹窗打开后键盘操作不该跑到下面的页面上。
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape") {
				event.preventDefault();
				onCancel();
				return;
			}
			if (event.key !== "Tab" || !panelRef.current) {
				return;
			}
			const focusable = panelRef.current.querySelectorAll<HTMLElement>(
				'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
			);
			if (focusable.length === 0) {
				return;
			}
			const first = focusable[0];
			const last = focusable[focusable.length - 1];
			if (event.shiftKey && document.activeElement === first) {
				event.preventDefault();
				last.focus();
				return;
			}
			if (!event.shiftKey && document.activeElement === last) {
				event.preventDefault();
				first.focus();
			}
		};
		document.addEventListener("keydown", onKeyDown);
		return () => document.removeEventListener("keydown", onKeyDown);
	}, [open, onCancel]);

	if (!open || typeof document === "undefined") {
		return null;
	}

	return createPortal(
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-ink/40 px-4"
			// 点遮罩取消：这是「放弃操作」，与显式取消同义；不会误触确认。
			onMouseDown={(event) => {
				if (event.target === event.currentTarget) {
					onCancel();
				}
			}}
			role="presentation"
		>
			<div
				ref={panelRef}
				role="alertdialog"
				aria-modal="true"
				aria-label={title}
				className="w-full max-w-[420px] rounded border border-grid bg-white"
			>
				<header className="flex flex-col gap-1 border-b border-grid-soft px-4 py-3">
					<span className="eyebrow">{tone === "danger" ? "不可恢复" : "请确认"}</span>
					<h2 className="text-sm font-semibold text-ink">{title}</h2>
				</header>
				{description ? (
					<p className="px-4 py-3 text-xs leading-6 text-ink-muted">{description}</p>
				) : null}
				<footer className="flex justify-end gap-2 border-t border-grid-soft px-4 py-3">
					{/* autoFocus 落在「取消」上：破坏性操作不该让一次回车直接生效。 */}
					<Button autoFocus onClick={onCancel}>
						{cancelLabel}
					</Button>
					<Button variant={tone === "danger" ? "danger" : "primary"} onClick={onConfirm}>
						{confirmLabel}
					</Button>
				</footer>
			</div>
		</div>,
		document.body,
	);
}

/** useConfirm 把「弹一次确认并拿到结果」收敛成一个 promise，调用处不需要维护状态。 */
export function useConfirm() {
	const [state, setState] = useState<(ConfirmOptions & { resolve: (ok: boolean) => void }) | null>(
		null,
	);

	const confirm = useCallback(
		(options: ConfirmOptions) =>
			new Promise<boolean>((resolve) => {
				setState({ ...options, resolve });
			}),
		[],
	);

	const close = useCallback((ok: boolean) => {
		setState((current) => {
			current?.resolve(ok);
			return null;
		});
	}, []);

	const dialog = state ? (
		<ConfirmDialog
			open
			title={state.title}
			description={state.description}
			confirmLabel={state.confirmLabel}
			cancelLabel={state.cancelLabel}
			tone={state.tone}
			onConfirm={() => close(true)}
			onCancel={() => close(false)}
		/>
	) : null;

	return { confirm, dialog };
}
