"use client";
import React, { useState, useMemo } from "react";
import { Divider, Button } from "@mantine/core";
import { ChevronDown, ChevronUp, Users } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

// Helper functions to extract address info from the 'from' field
// (avoiding dependency on MessageEntity type)
const extractAddress = (from: any): string | null => {
	if (!from) return null;
	if (typeof from === "string") return from;
	return from.value?.[0]?.address ?? null;
};

const extractName = (from: any): string | null => {
	if (!from) return null;
	if (typeof from === "string") return from.split("@")[0] ?? null;
	return from.value?.[0]?.name ?? null;
};

type ThreadPageClientProps = {
	messages: Array<{
		id: string;
		seen: boolean;
		from: any;
		createdAt: Date;
		text?: string | null;
		html?: string | null;
	}>;
	children: React.ReactNode[];
};

export default function ThreadPageClient({
	messages,
	children,
}: ThreadPageClientProps) {
	// Initialize expanded state: last message + unread messages
	const [expandedIds, setExpandedIds] = useState<Set<string>>(() => {
		const ids = new Set<string>();
		if (messages.length > 0) {
			ids.add(messages[messages.length - 1].id);
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

	const expandAll = () => {
		setExpandedIds(new Set(messages.map((m) => m.id)));
	};

	const collapseAll = () => {
		if (messages.length > 0) {
			setExpandedIds(new Set([messages[messages.length - 1].id]));
		}
	};

	// Get unique participants
	const participants = useMemo(() => {
		const seen = new Set<string>();
		const result: { name: string; email: string }[] = [];

		messages.forEach((m) => {
			const email = extractAddress(m.from);
			if (email && !seen.has(email.toLowerCase())) {
				seen.add(email.toLowerCase());
				result.push({
					name: extractName(m.from) || email,
					email,
				});
			}
		});

		return result;
	}, [messages]);

	// Date range
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

	const allExpanded = expandedIds.size === messages.length;
	const collapsedCount = messages.length - expandedIds.size;

	// For 1-2 messages, just show everything
	if (messages.length <= 2) {
		return <>{children}</>;
	}

	return (
		<>
			{/* Summary Bar */}
			<div className="sticky top-0 z-10 bg-background/95 backdrop-blur border-b px-4 py-2 flex items-center justify-between">
				<div className="flex items-center gap-4">
					{/* Participants */}
					<div className="flex items-center gap-2">
						<Users className="h-4 w-4 text-muted-foreground" />
						<div className="flex -space-x-2">
							{participants.slice(0, 3).map((p) => (
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
					<div className="text-sm text-muted-foreground hidden sm:block">{dateRange}</div>
				</div>

				{/* Expand/Collapse controls */}
				<div className="flex items-center gap-2">
					{collapsedCount > 0 && (
						<span className="text-xs text-muted-foreground">
							{collapsedCount} collapsed
						</span>
					)}
					<Button
						variant="subtle"
						size="xs"
						onClick={allExpanded ? collapseAll : expandAll}
						leftSection={
							allExpanded ? (
								<ChevronUp className="h-3 w-3" />
							) : (
								<ChevronDown className="h-3 w-3" />
							)
						}
					>
						{allExpanded ? "Collapse" : "Expand all"}
					</Button>
				</div>
			</div>

			{/* Messages */}
			{messages.map((message, index) => {
				const isExpanded = expandedIds.has(message.id);
				const senderName =
					extractName(message.from) ||
					extractAddress(message.from) ||
					"Unknown";
				const date = new Date(message.createdAt).toLocaleDateString(undefined, {
					month: "short",
					day: "numeric",
					hour: "numeric",
					minute: "2-digit",
				});
				const preview =
					message.text?.slice(0, 100) ||
					message.html?.replace(/<[^>]+>/g, "").slice(0, 100) ||
					"";

				return (
					<motion.div
						key={message.id}
						layout
						initial={{ opacity: 0 }}
						animate={{ opacity: 1 }}
						transition={{ duration: 0.2 }}
					>
						<AnimatePresence mode="wait">
							{isExpanded ? (
								<motion.div
									key="expanded"
									initial={{ opacity: 0, height: 0 }}
									animate={{ opacity: 1, height: "auto" }}
									exit={{ opacity: 0, height: 0 }}
									transition={{ duration: 0.2 }}
								>
									{children[index]}
								</motion.div>
							) : (
								/* Collapsed state */
								<motion.div
									key="collapsed"
									initial={{ opacity: 0 }}
									animate={{ opacity: 1 }}
									exit={{ opacity: 0 }}
									transition={{ duration: 0.15 }}
									onClick={() => toggleExpand(message.id)}
									className="px-4 py-3 cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-4 border-b"
								>
									{/* Sender avatar */}
									<motion.div
										className="h-8 w-8 rounded-full bg-primary/10 flex items-center justify-center text-sm font-medium shrink-0"
										whileHover={{ scale: 1.05 }}
									>
										{senderName.charAt(0).toUpperCase()}
									</motion.div>

									{/* Content */}
									<div className="flex-1 min-w-0">
										<div className="flex items-center gap-2">
											<span
												className={`font-medium ${!message.seen ? "font-semibold" : ""}`}
											>
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
										<div className="text-sm text-muted-foreground truncate">
											{preview}...
										</div>
									</div>

									{/* Expand indicator */}
									<motion.div whileHover={{ scale: 1.2 }}>
										<ChevronDown className="h-4 w-4 text-muted-foreground shrink-0" />
									</motion.div>
								</motion.div>
							)}
						</AnimatePresence>
						{isExpanded && (
							<Divider className="opacity-50 mb-6" ml="xl" mr="xl" />
						)}
					</motion.div>
				);
			})}
		</>
	);
}
