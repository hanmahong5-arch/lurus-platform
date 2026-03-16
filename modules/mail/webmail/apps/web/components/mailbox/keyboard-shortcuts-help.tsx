"use client";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SHORTCUTS } from "@/hooks/use-keyboard-shortcuts";
import { motion } from "framer-motion";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export default function KeyboardShortcutsHelp({ open, onOpenChange }: Props) {
	// Group shortcuts by category
	const navigationShortcuts = SHORTCUTS.filter((s) =>
		["navigate-up", "navigate-down", "open-thread", "go-back"].includes(s.action)
	);

	const actionShortcuts = SHORTCUTS.filter((s) =>
		["archive", "delete", "reply", "reply-all", "forward", "toggle-star"].includes(s.action)
	);

	const organizationShortcuts = SHORTCUTS.filter((s) =>
		["add-label", "move-to-folder", "mark-unread", "mark-read"].includes(s.action)
	);

	const otherShortcuts = SHORTCUTS.filter((s) =>
		["compose", "focus-search", "show-help"].includes(s.action)
	);

	const renderShortcut = (shortcut: (typeof SHORTCUTS)[number], index: number) => (
		<motion.div
			key={shortcut.key + shortcut.action}
			className="flex items-center justify-between py-1.5"
			initial={{ opacity: 0, x: -10 }}
			animate={{ opacity: 1, x: 0 }}
			transition={{ delay: index * 0.03, duration: 0.2 }}
		>
			<span className="text-sm text-muted-foreground">{shortcut.label}</span>
			<motion.kbd
				className="px-2 py-1 text-xs font-mono bg-muted rounded border border-border"
				whileHover={{ scale: 1.05, backgroundColor: "var(--primary)" }}
				transition={{ duration: 0.1 }}
			>
				{"shift" in shortcut && shortcut.shift ? "Shift + " : ""}
				{shortcut.key}
			</motion.kbd>
		</motion.div>
	);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>Keyboard Shortcuts</DialogTitle>
				</DialogHeader>

				<div className="grid grid-cols-2 gap-6 mt-4">
					<motion.div
						initial={{ opacity: 0, y: 10 }}
						animate={{ opacity: 1, y: 0 }}
						transition={{ delay: 0.1 }}
					>
						<h3 className="text-sm font-semibold mb-2">Navigation</h3>
						<div className="space-y-1">
							{navigationShortcuts.map((s, i) => renderShortcut(s, i))}
						</div>
					</motion.div>

					<motion.div
						initial={{ opacity: 0, y: 10 }}
						animate={{ opacity: 1, y: 0 }}
						transition={{ delay: 0.15 }}
					>
						<h3 className="text-sm font-semibold mb-2">Actions</h3>
						<div className="space-y-1">
							{actionShortcuts.map((s, i) => renderShortcut(s, i))}
						</div>
					</motion.div>

					<motion.div
						initial={{ opacity: 0, y: 10 }}
						animate={{ opacity: 1, y: 0 }}
						transition={{ delay: 0.2 }}
					>
						<h3 className="text-sm font-semibold mb-2">Organization</h3>
						<div className="space-y-1">
							{organizationShortcuts.map((s, i) => renderShortcut(s, i))}
						</div>
					</motion.div>

					<motion.div
						initial={{ opacity: 0, y: 10 }}
						animate={{ opacity: 1, y: 0 }}
						transition={{ delay: 0.25 }}
					>
						<h3 className="text-sm font-semibold mb-2">Other</h3>
						<div className="space-y-1">
							{otherShortcuts.map((s, i) => renderShortcut(s, i))}
						</div>
					</motion.div>
				</div>

				<div className="mt-4 pt-4 border-t border-border">
					<p className="text-xs text-muted-foreground text-center">
						Press <kbd className="px-1 py-0.5 text-xs font-mono bg-muted rounded">?</kbd> to toggle this dialog
					</p>
				</div>
			</DialogContent>
		</Dialog>
	);
}
