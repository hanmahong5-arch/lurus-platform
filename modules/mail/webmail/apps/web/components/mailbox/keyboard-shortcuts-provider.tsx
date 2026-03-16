"use client";
import { useCallback, createContext, useContext, useState, ReactNode } from "react";
import { useRouter, usePathname, useParams } from "next/navigation";
import {
	useKeyboardShortcuts,
	useEmailNavigation,
	ShortcutAction,
} from "@/hooks/use-keyboard-shortcuts";
import KeyboardShortcutsHelp from "./keyboard-shortcuts-help";

type KeyboardShortcutsContextType = {
	focusedIndex: number;
	setFocusedIndex: (index: number) => void;
	totalCount: number;
	setTotalCount: (count: number) => void;
	triggerAction: (action: ShortcutAction) => void;
};

const KeyboardShortcutsContext = createContext<KeyboardShortcutsContextType | null>(null);

export function useKeyboardShortcutsContext() {
	const ctx = useContext(KeyboardShortcutsContext);
	if (!ctx) {
		throw new Error(
			"useKeyboardShortcutsContext must be used within KeyboardShortcutsProvider"
		);
	}
	return ctx;
}

type Props = {
	children: ReactNode;
	identityPublicId: string;
	mailboxSlug: string;
};

export function KeyboardShortcutsProvider({
	children,
	identityPublicId,
	mailboxSlug,
}: Props) {
	const router = useRouter();
	const pathname = usePathname();
	const params = useParams();

	const [totalCount, setTotalCount] = useState(0);
	const { focusedIndex, setFocusedIndex, navigateUp, navigateDown } =
		useEmailNavigation(totalCount);

	// Track registered action handlers from child components
	const [actionHandlers, setActionHandlers] = useState<
		Map<ShortcutAction, () => void>
	>(new Map());

	const handleAction = useCallback(
		(action: ShortcutAction) => {
			const isInThread = !!params?.threadId;

			switch (action) {
				case "navigate-up":
					navigateUp();
					break;

				case "navigate-down":
					navigateDown();
					break;

				case "go-back":
					if (isInThread) {
						router.push(`/mail/${identityPublicId}/${mailboxSlug}`);
					}
					break;

				case "compose":
					// Trigger compose dialog - dispatch custom event
					window.dispatchEvent(new CustomEvent("keyboard-shortcut-compose"));
					break;

				case "focus-search":
					// Trigger search focus - dispatch custom event
					window.dispatchEvent(new CustomEvent("keyboard-shortcut-search"));
					break;

				// These actions need to be handled by specific components
				case "open-thread":
				case "archive":
				case "delete":
				case "reply":
				case "reply-all":
				case "forward":
				case "toggle-star":
				case "add-label":
				case "move-to-folder":
				case "mark-unread":
				case "mark-read":
					// Dispatch event with action type for components to handle
					window.dispatchEvent(
						new CustomEvent("keyboard-shortcut-action", {
							detail: { action, focusedIndex },
						})
					);
					break;
			}
		},
		[
			params?.threadId,
			router,
			identityPublicId,
			mailboxSlug,
			navigateUp,
			navigateDown,
			focusedIndex,
		]
	);

	const { helpOpen, setHelpOpen } = useKeyboardShortcuts({
		onAction: handleAction,
		enabled: true,
	});

	const triggerAction = useCallback(
		(action: ShortcutAction) => {
			handleAction(action);
		},
		[handleAction]
	);

	return (
		<KeyboardShortcutsContext.Provider
			value={{
				focusedIndex,
				setFocusedIndex,
				totalCount,
				setTotalCount,
				triggerAction,
			}}
		>
			{children}
			<KeyboardShortcutsHelp open={helpOpen} onOpenChange={setHelpOpen} />
		</KeyboardShortcutsContext.Provider>
	);
}
