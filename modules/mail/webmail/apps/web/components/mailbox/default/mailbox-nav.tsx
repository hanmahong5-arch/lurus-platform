"use client";

import Link from "next/link";
import { useParams, usePathname } from "next/navigation";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import {
	Inbox,
	Send,
	FileText,
	Archive,
	Ban,
	Trash2,
	Folder,
	Plus,
} from "lucide-react";
import { MailboxEntity } from "@db";
import { useDroppable } from "@dnd-kit/core";
import { useMailDnd } from "@/components/mailbox/dnd-provider";

type Mailbox = {
	slug: string | null;
	kind:
		| "inbox"
		| "sent"
		| "drafts"
		| "archive"
		| "spam"
		| "trash"
		| "outbox"
		| "custom";
	name?: string | null;
	unreadCount?: number | null;
};

export function MailboxNav({
	mailboxes,
	identityPublicId,
	onCreateLabel,
}: {
	mailboxes: MailboxEntity[];
	identityPublicId: string;
	onCreateLabel?: () => void;
}) {
	const pathname = usePathname();
	const params = useParams() as { mailboxSlug?: string };

	const systemOrder: Mailbox["kind"][] = [
		"inbox",
		"starred" as any, // if you add later
		"drafts",
		"sent",
		"archive",
		"spam",
		"trash",
	].filter(Boolean) as Mailbox["kind"][];

	const iconFor: Record<Mailbox["kind"], React.ElementType> = {
		inbox: Inbox,
		sent: Send,
		drafts: FileText,
		archive: Archive,
		spam: Ban,
		trash: Trash2,
		outbox: Send,
		custom: Folder,
	};

	const system = mailboxes
		.filter((m) => m.kind !== "custom")
		.filter((m) => m.kind !== "drafts")
		.sort((a, b) => systemOrder.indexOf(a.kind) - systemOrder.indexOf(b.kind));

	const custom = mailboxes.filter((m) => m.kind === "custom");

	const { isDragging, activeDropTarget } = useMailDnd();

	const DroppableItem = ({ m }: { m: Mailbox }) => {
		const Icon = iconFor[m.kind] ?? Folder;
		const slug = m.slug ?? "inbox";
		const href = `/mail/${identityPublicId}/${slug}`;
		const dropId = m.kind; // Use kind as drop target ID

		const { setNodeRef, isOver } = useDroppable({
			id: dropId,
		});

		const isActive =
			pathname === href || (params.mailboxSlug == null && slug === "inbox");

		// Only highlight droppable folders when dragging
		const canDrop = isDragging && ["inbox", "archive", "spam", "trash"].includes(m.kind);
		const isDropTarget = isOver && canDrop;

		return (
			<div ref={setNodeRef}>
				<Link
					href={href}
					className={cn(
						"group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-all",
						"hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
						isActive && "bg-sidebar-accent text-sidebar-accent-foreground",
						// Drop target styles
						canDrop && "ring-2 ring-primary/20 ring-inset",
						isDropTarget && "bg-primary/10 ring-primary scale-105",
					)}
				>
					<Icon className={cn(
						"h-4 w-4 shrink-0 transition-colors",
						isDropTarget && "text-primary"
					)} />
					<span className="min-w-0 truncate">
						{m.kind === "custom" ? (m.name ?? "Label") : titleFor(m.kind)}
					</span>
					{m.unreadCount ? (
						<Badge
							variant={isActive ? "secondary" : "outline"}
							className="ml-auto"
						>
							{m.unreadCount}
						</Badge>
					) : null}
				</Link>
			</div>
		);
	};

	return (
		<div className="space-y-4 px-2">
			<div className="space-y-1">
				{system.map((m) => (
					<DroppableItem key={m.slug ?? m.kind} m={m} />
				))}
			</div>
		</div>
	);
}

function titleFor(kind: Mailbox["kind"]) {
	switch (kind) {
		case "inbox":
			return "Inbox";
		case "sent":
			return "Sent";
		case "drafts":
			return "Drafts";
		case "archive":
			return "Archive";
		case "spam":
			return "Spam";
		case "trash":
			return "Trash";
		case "outbox":
			return "Outbox";
		default:
			return "Mailbox";
	}
}
