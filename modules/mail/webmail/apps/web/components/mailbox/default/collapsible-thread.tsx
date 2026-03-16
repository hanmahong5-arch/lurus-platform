"use client";
import React, { useState, useMemo } from "react";
import { MessageEntity } from "@db";
import { ChevronDown, ChevronUp, Users } from "lucide-react";
import { Button } from "@mantine/core";
import { getMessageAddress, getMessageName } from "@common/mail-client";
import { motion, AnimatePresence } from "framer-motion";

type CollapsibleThreadProps = {
	messages: MessageEntity[];
	children: (props: {
		expandedIds: Set<string>;
		toggleExpand: (id: string) => void;
		isExpanded: (id: string) => boolean;
		expandAll: () => void;
		collapseAll: () => void;
	}) => React.ReactNode;
};

export function CollapsibleThread({ messages, children }: CollapsibleThreadProps) {
	// Initialize with: last message + all unread messages expanded
	const [expandedIds, setExpandedIds] = useState<Set<string>>(() => {
		const ids = new Set<string>();
		if (messages.length > 0) {
			// Always expand the last message
			ids.add(messages[messages.length - 1].id);
			// Expand all unread messages
			messages.forEach((m) => {
				if (!m.seen) {
					ids.add(m.id);
				}
			});
		}
		return ids;
	});

	const toggleExpand = (id: string) => {
		setExpandedIds((prev) => {
			const next = new Set(prev);
			if (next.has(id)) {
				next.delete(id);
			} else {
				next.add(id);
			}
			return next;
		});
	};

	const isExpanded = (id: string) => expandedIds.has(id);

	const expandAll = () => {
		setExpandedIds(new Set(messages.map((m) => m.id)));
	};

	const collapseAll = () => {
		// Keep only the last message expanded
		if (messages.length > 0) {
			setExpandedIds(new Set([messages[messages.length - 1].id]));
		} else {
			setExpandedIds(new Set());
		}
	};

	return <>{children({ expandedIds, toggleExpand, isExpanded, expandAll, collapseAll })}</>;
}

// Thread Summary Bar component
type ThreadSummaryBarProps = {
	messages: MessageEntity[];
	expandedCount: number;
	onExpandAll: () => void;
	onCollapseAll: () => void;
};

export function ThreadSummaryBar({
	messages,
	expandedCount,
	onExpandAll,
	onCollapseAll,
}: ThreadSummaryBarProps) {
	// Get unique participants
	const participants = useMemo(() => {
		const seen = new Set<string>();
		const result: { name: string; email: string }[] = [];

		messages.forEach((m) => {
			const email = getMessageAddress(m, "from");
			if (email && !seen.has(email.toLowerCase())) {
				seen.add(email.toLowerCase());
				result.push({
					name: getMessageName(m, "from") || email,
					email,
				});
			}
		});

		return result;
	}, [messages]);

	// Calculate date range
	const dateRange = useMemo(() => {
		if (messages.length === 0) return "";
		const first = new Date(messages[0].createdAt);
		const last = new Date(messages[messages.length - 1].createdAt);

		const formatDate = (d: Date) =>
			d.toLocaleDateString(undefined, { month: "short", day: "numeric" });

		if (
			first.getFullYear() === last.getFullYear() &&
			first.getMonth() === last.getMonth() &&
			first.getDate() === last.getDate()
		) {
			return formatDate(first);
		}

		return `${formatDate(first)} - ${formatDate(last)}`;
	}, [messages]);

	const collapsedCount = messages.length - expandedCount;
	const allExpanded = expandedCount === messages.length;

	return (
		<div className="sticky top-0 z-10 bg-background/95 backdrop-blur border-b px-4 py-2 flex items-center justify-between">
			<div className="flex items-center gap-4">
				{/* Participants */}
				<div className="flex items-center gap-2">
					<Users className="h-4 w-4 text-muted-foreground" />
					<div className="flex -space-x-2">
						{participants.slice(0, 3).map((p, i) => (
							<div
								key={p.email}
								className="h-6 w-6 rounded-full bg-primary/10 border-2 border-background flex items-center justify-center text-xs font-medium"
								title={p.name}
							>
								{p.name.charAt(0).toUpperCase()}
							</div>
						))}
						{participants.length > 3 && (
							<div className="h-6 w-6 rounded-full bg-muted border-2 border-background flex items-center justify-center text-xs text-muted-foreground">
								+{participants.length - 3}
							</div>
						)}
					</div>
					<span className="text-sm text-muted-foreground">
						{participants.length} participant{participants.length !== 1 ? "s" : ""}
					</span>
				</div>

				{/* Message count */}
				<div className="text-sm text-muted-foreground">
					{messages.length} message{messages.length !== 1 ? "s" : ""}
				</div>

				{/* Date range */}
				<div className="text-sm text-muted-foreground">{dateRange}</div>
			</div>

			{/* Expand/Collapse controls */}
			<div className="flex items-center gap-2">
				<AnimatePresence mode="wait">
					{collapsedCount > 0 && (
						<motion.span
							key="collapsed-count"
							initial={{ opacity: 0, x: 10 }}
							animate={{ opacity: 1, x: 0 }}
							exit={{ opacity: 0, x: -10 }}
							className="text-xs text-muted-foreground"
						>
							{collapsedCount} collapsed
						</motion.span>
					)}
				</AnimatePresence>
				<motion.div whileHover={{ scale: 1.02 }} whileTap={{ scale: 0.98 }}>
					<Button
						variant="subtle"
						size="xs"
						onClick={allExpanded ? onCollapseAll : onExpandAll}
						leftSection={
							<motion.div
								animate={{ rotate: allExpanded ? 180 : 0 }}
								transition={{ duration: 0.2 }}
							>
								<ChevronDown className="h-3 w-3" />
							</motion.div>
						}
					>
						{allExpanded ? "Collapse" : "Expand all"}
					</Button>
				</motion.div>
			</div>
		</div>
	);
}

// Collapsible message header for collapsed state
type CollapsedMessageHeaderProps = {
	message: MessageEntity;
	onClick: () => void;
};

export function CollapsedMessageHeader({ message, onClick }: CollapsedMessageHeaderProps) {
	const senderName = getMessageName(message, "from") || getMessageAddress(message, "from") || "Unknown";
	const date = new Date(message.createdAt).toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
		hour: "numeric",
		minute: "2-digit",
	});

	// Get preview text (first 100 chars of text content)
	const preview = message.text?.slice(0, 100) || message.html?.replace(/<[^>]+>/g, "").slice(0, 100) || "";

	return (
		<motion.div
			initial={{ opacity: 0, height: 0 }}
			animate={{ opacity: 1, height: "auto" }}
			exit={{ opacity: 0, height: 0 }}
			transition={{ duration: 0.2, ease: "easeInOut" }}
			onClick={onClick}
			className="px-4 py-3 cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-4 border-b overflow-hidden"
		>
			{/* Sender avatar */}
			<motion.div
				className="h-8 w-8 rounded-full bg-primary/10 flex items-center justify-center text-sm font-medium shrink-0"
				whileHover={{ scale: 1.05 }}
				transition={{ type: "spring", stiffness: 400 }}
			>
				{senderName.charAt(0).toUpperCase()}
			</motion.div>

			{/* Content */}
			<div className="flex-1 min-w-0">
				<div className="flex items-center gap-2">
					<span className={`font-medium ${!message.seen ? "font-semibold" : ""}`}>
						{senderName}
					</span>
					<span className="text-xs text-muted-foreground">{date}</span>
					{!message.seen && (
						<motion.span
							className="h-2 w-2 rounded-full bg-primary"
							initial={{ scale: 0 }}
							animate={{ scale: 1 }}
							transition={{ type: "spring", stiffness: 500 }}
						/>
					)}
				</div>
				<div className="text-sm text-muted-foreground truncate">{preview}...</div>
			</div>

			{/* Expand indicator */}
			<motion.div
				whileHover={{ scale: 1.2 }}
				transition={{ type: "spring", stiffness: 400 }}
			>
				<ChevronDown className="h-4 w-4 text-muted-foreground shrink-0" />
			</motion.div>
		</motion.div>
	);
}
