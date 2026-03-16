"use client";
import React, { createContext, useContext, useState, useCallback } from "react";
import {
	DndContext,
	DragOverlay,
	useSensor,
	useSensors,
	PointerSensor,
	DragStartEvent,
	DragEndEvent,
	DragOverEvent,
	closestCenter,
} from "@dnd-kit/core";
import { Mail, Mails } from "lucide-react";
import { moveToTrash } from "@/lib/actions/mailbox";
import { toast } from "sonner";

// Types
export type DraggedItem = {
	threadId: string;
	subject: string;
	selectedCount: number; // For batch drag
	mailboxId: string;
	hasSync: boolean;
};

type DndContextValue = {
	isDragging: boolean;
	draggedItem: DraggedItem | null;
	activeDropTarget: string | null;
};

const MailDndContext = createContext<DndContextValue>({
	isDragging: false,
	draggedItem: null,
	activeDropTarget: null,
});

export function useMailDnd() {
	return useContext(MailDndContext);
}

type MailDndProviderProps = {
	children: React.ReactNode;
};

export function MailDndProvider({ children }: MailDndProviderProps) {
	const [draggedItem, setDraggedItem] = useState<DraggedItem | null>(null);
	const [activeDropTarget, setActiveDropTarget] = useState<string | null>(null);

	// Configure sensors with activation delay to avoid accidental drags
	const sensors = useSensors(
		useSensor(PointerSensor, {
			activationConstraint: {
				distance: 8, // Minimum drag distance before activating
			},
		})
	);

	const handleDragStart = useCallback((event: DragStartEvent) => {
		const data = event.active.data.current as DraggedItem | undefined;
		if (data) {
			setDraggedItem(data);
		}
	}, []);

	const handleDragOver = useCallback((event: DragOverEvent) => {
		const { over } = event;
		setActiveDropTarget(over?.id as string | null);
	}, []);

	const handleDragEnd = useCallback(async (event: DragEndEvent) => {
		const { active, over } = event;

		const currentDraggedItem = draggedItem;
		setDraggedItem(null);
		setActiveDropTarget(null);

		if (!over || !active.data.current || !currentDraggedItem) {
			return;
		}

		const dropTarget = over.id as string;
		const { threadId, mailboxId, hasSync } = currentDraggedItem;

		// Determine action based on drop target
		// Note: Only trash is currently supported via drag-and-drop
		// Archive/spam/inbox require proper mailbox UUID resolution
		try {
			if (dropTarget === "trash") {
				await moveToTrash(threadId, mailboxId, hasSync, true);
				toast.success("Message moved to Trash", { position: "bottom-left" });
			}
			// TODO: Implement archive/spam/inbox with proper mailbox UUID lookup
		} catch (error) {
			console.error("Failed to move message:", error);
			toast.error("Failed to move message", { position: "bottom-left" });
		}
	}, [draggedItem]);

	return (
		<DndContext
			sensors={sensors}
			collisionDetection={closestCenter}
			onDragStart={handleDragStart}
			onDragOver={handleDragOver}
			onDragEnd={handleDragEnd}
		>
			<MailDndContext.Provider
				value={{
					isDragging: !!draggedItem,
					draggedItem,
					activeDropTarget,
				}}
			>
				{children}
			</MailDndContext.Provider>

			{/* Drag Overlay - follows cursor while dragging */}
			<DragOverlay>
				{draggedItem && (
					<div className="flex items-center gap-2 bg-background border rounded-lg shadow-lg px-3 py-2 opacity-90">
						{draggedItem.selectedCount > 1 ? (
							<Mails className="h-4 w-4 text-primary" />
						) : (
							<Mail className="h-4 w-4 text-primary" />
						)}
						<span className="text-sm font-medium truncate max-w-[200px]">
							{draggedItem.selectedCount > 1
								? `${draggedItem.selectedCount} messages`
								: draggedItem.subject || "(No Subject)"}
						</span>
					</div>
				)}
			</DragOverlay>
		</DndContext>
	);
}
