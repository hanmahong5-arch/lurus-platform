import { create } from "zustand";
import { persist } from "zustand/middleware";

export interface MailSelectionState {
	// Selected thread/message IDs
	selectedThreadIds: Set<string>;
	activeThreadId: string | null;
	activeMessageId: string | null;

	// Bulk selection mode
	bulkMode: boolean;

	// Actions
	selectThread: (id: string) => void;
	deselectThread: (id: string) => void;
	toggleThread: (id: string) => void;
	selectAll: (ids: string[]) => void;
	clearSelection: () => void;
	setActiveThread: (id: string | null) => void;
	setActiveMessage: (id: string | null) => void;
	setBulkMode: (enabled: boolean) => void;
}

export const useMailStore = create<MailSelectionState>((set) => ({
	selectedThreadIds: new Set(),
	activeThreadId: null,
	activeMessageId: null,
	bulkMode: false,

	selectThread: (id) =>
		set((state) => {
			const next = new Set(state.selectedThreadIds);
			next.add(id);
			return { selectedThreadIds: next };
		}),

	deselectThread: (id) =>
		set((state) => {
			const next = new Set(state.selectedThreadIds);
			next.delete(id);
			return { selectedThreadIds: next };
		}),

	toggleThread: (id) =>
		set((state) => {
			const next = new Set(state.selectedThreadIds);
			if (next.has(id)) {
				next.delete(id);
			} else {
				next.add(id);
			}
			return { selectedThreadIds: next };
		}),

	selectAll: (ids) =>
		set(() => ({ selectedThreadIds: new Set(ids) })),

	clearSelection: () =>
		set(() => ({ selectedThreadIds: new Set(), bulkMode: false })),

	setActiveThread: (id) =>
		set(() => ({ activeThreadId: id })),

	setActiveMessage: (id) =>
		set(() => ({ activeMessageId: id })),

	setBulkMode: (enabled) =>
		set(() => ({ bulkMode: enabled })),
}));
