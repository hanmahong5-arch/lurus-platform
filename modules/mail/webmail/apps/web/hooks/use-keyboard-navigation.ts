import { useCallback, useEffect } from "react";

export interface KeyboardNavigationOptions {
	/** Total number of items in the list */
	itemCount: number;
	/** Currently focused index */
	activeIndex: number;
	/** Callback when active index changes */
	onActiveIndexChange: (index: number) => void;
	/** Callback when Enter is pressed on the active item */
	onSelect?: (index: number) => void;
	/** Callback when Escape is pressed */
	onEscape?: () => void;
	/** Callback when Delete/Backspace is pressed */
	onDelete?: (index: number) => void;
	/** Whether navigation is enabled */
	enabled?: boolean;
}

/**
 * Hook for keyboard navigation in mail list.
 * Supports j/k (vim-style), Arrow keys, Enter, Escape, Delete.
 */
export function useKeyboardNavigation({
	itemCount,
	activeIndex,
	onActiveIndexChange,
	onSelect,
	onEscape,
	onDelete,
	enabled = true,
}: KeyboardNavigationOptions) {
	const handleKeyDown = useCallback(
		(event: KeyboardEvent) => {
			if (!enabled || itemCount === 0) return;

			// Skip if user is typing in an input/textarea
			const target = event.target as HTMLElement;
			if (
				target.tagName === "INPUT" ||
				target.tagName === "TEXTAREA" ||
				target.isContentEditable
			) {
				return;
			}

			switch (event.key) {
				case "j":
				case "ArrowDown": {
					event.preventDefault();
					const next = Math.min(activeIndex + 1, itemCount - 1);
					onActiveIndexChange(next);
					break;
				}
				case "k":
				case "ArrowUp": {
					event.preventDefault();
					const prev = Math.max(activeIndex - 1, 0);
					onActiveIndexChange(prev);
					break;
				}
				case "Enter": {
					event.preventDefault();
					onSelect?.(activeIndex);
					break;
				}
				case "Escape": {
					event.preventDefault();
					onEscape?.();
					break;
				}
				case "Delete":
				case "Backspace": {
					if (!event.metaKey && !event.ctrlKey) return;
					event.preventDefault();
					onDelete?.(activeIndex);
					break;
				}
				case "Home": {
					event.preventDefault();
					onActiveIndexChange(0);
					break;
				}
				case "End": {
					event.preventDefault();
					onActiveIndexChange(itemCount - 1);
					break;
				}
			}
		},
		[enabled, itemCount, activeIndex, onActiveIndexChange, onSelect, onEscape, onDelete],
	);

	useEffect(() => {
		if (!enabled) return;
		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [enabled, handleKeyDown]);
}
