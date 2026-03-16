"use client";
import React, { useEffect, useState } from "react";
import { Loader, Avatar, Badge, ActionIcon, Tooltip } from "@mantine/core";
import {
	Reply,
	Forward,
	Trash2,
	Paperclip,
	FileText,
	Image as ImageIcon,
	File,
	Star,
} from "lucide-react";
import { IconStarFilled } from "@tabler/icons-react";
import { FetchMailboxThreadsResult } from "@/lib/actions/mailbox";

type EmailPreviewPopoverProps = {
	threadItem: FetchMailboxThreadsResult[number];
	onReply?: () => void;
	onForward?: () => void;
	onDelete?: () => void;
	onToggleStar?: () => void;
};

function getAttachmentIcon(filename: string) {
	const ext = filename.split(".").pop()?.toLowerCase() || "";

	if (["jpg", "jpeg", "png", "gif", "webp", "svg"].includes(ext)) {
		return <ImageIcon className="h-4 w-4" />;
	}
	if (["pdf"].includes(ext)) {
		return <FileText className="h-4 w-4 text-red-500" />;
	}
	if (["doc", "docx"].includes(ext)) {
		return <FileText className="h-4 w-4 text-blue-500" />;
	}
	if (["xls", "xlsx"].includes(ext)) {
		return <FileText className="h-4 w-4 text-green-500" />;
	}
	return <File className="h-4 w-4" />;
}

function getInitials(name: string): string {
	const parts = name.trim().split(/\s+/);
	if (parts.length >= 2) {
		return (parts[0][0] + parts[1][0]).toUpperCase();
	}
	return name.slice(0, 2).toUpperCase();
}

function getSenderInfo(participants: FetchMailboxThreadsResult[number]["participants"]) {
	const from = participants?.from?.[0];
	if (!from) return { name: "Unknown", email: "" };

	return {
		name: from.n?.trim() || from.e?.split("@")[0] || "Unknown",
		email: from.e || "",
	};
}

export default function EmailPreviewPopover({
	threadItem,
	onReply,
	onForward,
	onDelete,
	onToggleStar,
}: EmailPreviewPopoverProps) {
	const sender = getSenderInfo(threadItem.participants);
	const previewText = threadItem.previewText || "";
	const truncatedPreview =
		previewText.length > 200 ? previewText.slice(0, 200) + "..." : previewText;

	// Format date
	const formattedDate = new Date(
		threadItem.lastActivityAt || Date.now()
	).toLocaleDateString(undefined, {
		weekday: "short",
		month: "short",
		day: "numeric",
		hour: "numeric",
		minute: "2-digit",
	});

	return (
		<div className="w-[400px] p-3">
			{/* Header with sender info */}
			<div className="flex items-start gap-3 mb-3">
				<Avatar
					size="md"
					radius="xl"
					color="blue"
					className="shrink-0"
				>
					{getInitials(sender.name)}
				</Avatar>
				<div className="flex-1 min-w-0">
					<div className="flex items-center justify-between gap-2">
						<div className="font-semibold truncate">{sender.name}</div>
						<div className="text-xs text-muted-foreground shrink-0">
							{formattedDate}
						</div>
					</div>
					<div className="text-xs text-muted-foreground truncate">
						{sender.email}
					</div>
				</div>
			</div>

			{/* Subject */}
			<div className="font-medium mb-2 line-clamp-2">
				{threadItem.subject || "(No Subject)"}
			</div>

			{/* Preview text */}
			<div className="text-sm text-muted-foreground mb-3 line-clamp-4">
				{truncatedPreview || "(No preview available)"}
			</div>

			{/* Attachments indicator */}
			{threadItem.hasAttachments && (
				<div className="flex items-center gap-2 mb-3 p-2 bg-muted/50 rounded-md">
					<Paperclip className="h-4 w-4 text-muted-foreground" />
					<span className="text-xs text-muted-foreground">
						Has attachments
					</span>
				</div>
			)}

			{/* Message count badge */}
			{threadItem.messageCount > 1 && (
				<div className="mb-3">
					<Badge variant="light" size="sm">
						{threadItem.messageCount} messages in thread
					</Badge>
				</div>
			)}

			{/* Quick actions */}
			<div className="flex items-center justify-between pt-2 border-t">
				<div className="flex items-center gap-1">
					<Tooltip label="Reply" position="bottom">
						<ActionIcon
							variant="subtle"
							size="sm"
							onClick={(e) => {
								e.stopPropagation();
								onReply?.();
							}}
						>
							<Reply className="h-4 w-4" />
						</ActionIcon>
					</Tooltip>
					<Tooltip label="Forward" position="bottom">
						<ActionIcon
							variant="subtle"
							size="sm"
							onClick={(e) => {
								e.stopPropagation();
								onForward?.();
							}}
						>
							<Forward className="h-4 w-4" />
						</ActionIcon>
					</Tooltip>
					<Tooltip label="Delete" position="bottom">
						<ActionIcon
							variant="subtle"
							size="sm"
							color="red"
							onClick={(e) => {
								e.stopPropagation();
								onDelete?.();
							}}
						>
							<Trash2 className="h-4 w-4" />
						</ActionIcon>
					</Tooltip>
				</div>

				<Tooltip
					label={threadItem.starred ? "Unstar" : "Star"}
					position="bottom"
				>
					<ActionIcon
						variant="subtle"
						size="sm"
						onClick={(e) => {
							e.stopPropagation();
							onToggleStar?.();
						}}
					>
						{threadItem.starred ? (
							<IconStarFilled className="h-4 w-4 text-yellow-400" />
						) : (
							<Star className="h-4 w-4" />
						)}
					</ActionIcon>
				</Tooltip>
			</div>

			{/* Unread indicator */}
			{threadItem.unreadCount > 0 && (
				<div className="absolute top-2 right-2">
					<Badge color="blue" size="xs" variant="filled">
						{threadItem.unreadCount} unread
					</Badge>
				</div>
			)}
		</div>
	);
}
