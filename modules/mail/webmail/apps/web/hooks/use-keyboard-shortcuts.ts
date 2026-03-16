"use client";
import { useEffect, useCallback, useState } from "react";
import { useRouter, usePathname } from "next/navigation";

export type ShortcutAction =
	| "navigate-up"
	| "navigate-down"
	| "open-thread"
	| "go-back"
	| "archive"
	| "delete"
	| "reply"
	| "reply-all"
	| "forward"
	| "toggle-star"
	| "add-label"
	| "move-to-folder"
	| "mark-unread"
	| "mark-read"
	| "compose"
	| "focus-search"
	| "show-help";

export type ShortcutHandler = (action: ShortcutAction) => void;

type KeyboardShortcutsOptions = {
	onAction: ShortcutHandler;
	enabled?: boolean;
};

// Keyboard shortcuts configuration matching Gmail
export const SHORTCUTS = [
	{ key: "j", action: "navigate-down" as const, label: "Next email" },
	{ key: "k", action: "navigate-up" as const, label: "Previous email" },
	{ key: "o", action: "open-thread" as const, label: "Open email" },
	{ key: "Enter", action: "open-thread" as const, label: "Open email" },
	{ key: "u", action: "go-back" as const, label: "Back to list" },
	{ key: "e", action: "archive" as const, label: "Archive" },
	{ key: "#", action: "delete" as const, label: "Delete" },
	{ key: "r", action: "reply" as const, label: "Reply" },
	{ key: "a", action: "reply-all" as const, label: "Reply all" },
	{ key: "f", action: "forward" as const, label: "Forward" },
	{ key: "s", action: "toggle-star" as const, label: "Toggle star" },
	{ key: "l", action: "add-label" as const, label: "Add label" },
	{ key: "v", action: "move-to-folder" as const, label: "Move to folder" },
	{ key: "U", action: "mark-unread" as const, label: "Mark unread", shift: true },
	{ key: "I", action: "mark-read" as const, label: "Mark read", shift: true },
	{ key: "c", action: "compose" as const, label: "Compose" },
	{ key: "/", action: "focus-search" as const, label: "Search" },
	{ key: "?", action: "show-help" as const, label: "Show shortcuts", shift: true },
] as const;

export function useKeyboardShortcuts({
	onAction,
	enabled = true,
}: KeyboardShortcutsOptions) {
	const [helpOpen, setHelpOpen] = useState(false);

	const handleKeyDown = useCallback(
		(event: KeyboardEvent) => {
			if (!enabled) return;

			// Ignore shortcuts when typing in input fields
			const target = event.target as HTMLElement;
			if (
				target.tagName === "INPUT" ||
				target.tagName === "TEXTAREA" ||
				target.isContentEditable
			) {
				return;
			}

			// Handle special keys
			const key = event.key;
			const shift = event.shiftKey;
			const ctrl = event.ctrlKey || event.metaKey;

			// Ctrl/Cmd shortcuts are handled elsewhere (e.g., Cmd+K for search)
			if (ctrl) return;

			// Find matching shortcut
			const shortcut = SHORTCUTS.find((s) => {
				const hasShift = "shift" in s && s.shift;
				if (hasShift && !shift) return false;
				if (!hasShift && shift && s.key !== "?") return false;
				return s.key.toLowerCase() === key.toLowerCase();
			});

			if (shortcut) {
				event.preventDefault();

				if (shortcut.action === "show-help") {
					setHelpOpen((prev) => !prev);
				} else {
					onAction(shortcut.action);
				}
			}
		},
		[enabled, onAction]
	);

	useEffect(() => {
		if (!enabled) return;

		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [enabled, handleKeyDown]);

	return {
		helpOpen,
		setHelpOpen,
		shortcuts: SHORTCUTS,
	};
}

// Hook for managing focused email index in list
export function useEmailNavigation(totalCount: number) {
	const [focusedIndex, setFocusedIndex] = useState(-1);

	const navigateUp = useCallback(() => {
		setFocusedIndex((prev) => Math.max(0, prev - 1));
	}, []);

	const navigateDown = useCallback(() => {
		setFocusedIndex((prev) => Math.min(totalCount - 1, prev + 1));
	}, [totalCount]);

	const resetFocus = useCallback(() => {
		setFocusedIndex(-1);
	}, []);

	return {
		focusedIndex,
		setFocusedIndex,
		navigateUp,
		navigateDown,
		resetFocus,
	};
}
