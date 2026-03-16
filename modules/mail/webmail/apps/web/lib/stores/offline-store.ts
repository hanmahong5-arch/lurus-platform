import { create } from "zustand";

export interface OfflineState {
	// Connection status
	isOnline: boolean;
	lastSyncAt: Date | null;
	syncInProgress: boolean;

	// Pending actions queue
	pendingActions: PendingAction[];

	// Actions
	setOnline: (online: boolean) => void;
	setLastSync: (date: Date) => void;
	setSyncInProgress: (inProgress: boolean) => void;
	addPendingAction: (action: PendingAction) => void;
	removePendingAction: (id: string) => void;
	clearPendingActions: () => void;
}

export interface PendingAction {
	id: string;
	type: "mark_read" | "mark_unread" | "star" | "unstar" | "move" | "delete" | "send";
	payload: Record<string, unknown>;
	createdAt: Date;
}

export const useOfflineStore = create<OfflineState>((set) => ({
	isOnline: typeof navigator !== "undefined" ? navigator.onLine : true,
	lastSyncAt: null,
	syncInProgress: false,
	pendingActions: [],

	setOnline: (isOnline) => set({ isOnline }),
	setLastSync: (lastSyncAt) => set({ lastSyncAt }),
	setSyncInProgress: (syncInProgress) => set({ syncInProgress }),

	addPendingAction: (action) =>
		set((state) => ({
			pendingActions: [...state.pendingActions, action],
		})),

	removePendingAction: (id) =>
		set((state) => ({
			pendingActions: state.pendingActions.filter((a) => a.id !== id),
		})),

	clearPendingActions: () => set({ pendingActions: [] }),
}));
