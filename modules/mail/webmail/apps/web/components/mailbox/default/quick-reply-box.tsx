"use client";
import React, { useState, useRef, useCallback } from "react";
import { Button } from "@mantine/core";
import { Send, Maximize2, Paperclip } from "lucide-react";
import { MessageEntity } from "@db";
import { getMessageAddress, getMessageName } from "@common/mail-client";

type Props = {
	message: MessageEntity;
	onExpandToFullEditor: () => void;
	onSend: (content: string) => Promise<void>;
	recipientEmail: string;
	recipientName?: string;
};

export default function QuickReplyBox({
	message,
	onExpandToFullEditor,
	onSend,
	recipientEmail,
	recipientName,
}: Props) {
	const [content, setContent] = useState("");
	const [isSending, setIsSending] = useState(false);
	const [isFocused, setIsFocused] = useState(false);
	const textareaRef = useRef<HTMLTextAreaElement>(null);

	const handleSend = useCallback(async () => {
		if (!content.trim() || isSending) return;

		setIsSending(true);
		try {
			await onSend(content);
			setContent("");
			setIsFocused(false);
		} catch (error) {
			console.error("Failed to send reply:", error);
		} finally {
			setIsSending(false);
		}
	}, [content, isSending, onSend]);

	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent<HTMLTextAreaElement>) => {
			// Cmd/Ctrl + Enter to send
			if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
				e.preventDefault();
				handleSend();
			}
			// Escape to blur
			if (e.key === "Escape") {
				e.preventDefault();
				textareaRef.current?.blur();
				setIsFocused(false);
			}
		},
		[handleSend]
	);

	// Auto-resize textarea
	const handleInput = useCallback(
		(e: React.ChangeEvent<HTMLTextAreaElement>) => {
			const textarea = e.target;
			setContent(textarea.value);

			// Reset height to auto to get the correct scrollHeight
			textarea.style.height = "auto";
			// Set height to scrollHeight, max 200px
			textarea.style.height = `${Math.min(textarea.scrollHeight, 200)}px`;
		},
		[]
	);

	const displayName = recipientName || recipientEmail;

	return (
		<div className="mt-4 border rounded-lg bg-background shadow-sm">
			{/* Collapsed state - just shows input placeholder */}
			{!isFocused && !content ? (
				<div
					className="px-4 py-3 cursor-text text-muted-foreground hover:bg-muted/30 transition-colors rounded-lg"
					onClick={() => {
						setIsFocused(true);
						setTimeout(() => textareaRef.current?.focus(), 0);
					}}
				>
					<span className="text-sm">
						Reply to {displayName}...
					</span>
				</div>
			) : (
				/* Expanded state - shows full textarea and actions */
				<div className="p-3">
					{/* Recipient indicator */}
					<div className="text-xs text-muted-foreground mb-2">
						To: <span className="font-medium text-foreground">{displayName}</span>
						{recipientName && (
							<span className="text-muted-foreground ml-1">
								&lt;{recipientEmail}&gt;
							</span>
						)}
					</div>

					{/* Textarea */}
					<textarea
						ref={textareaRef}
						value={content}
						onChange={handleInput}
						onKeyDown={handleKeyDown}
						onFocus={() => setIsFocused(true)}
						onBlur={() => {
							if (!content.trim()) {
								setIsFocused(false);
							}
						}}
						placeholder="Write your reply..."
						className="w-full min-h-[60px] resize-none border-none bg-transparent text-sm focus:outline-none placeholder:text-muted-foreground"
						rows={3}
						autoFocus
					/>

					{/* Actions */}
					<div className="flex items-center justify-between mt-2 pt-2 border-t">
						<div className="flex items-center gap-2">
							<Button
								onClick={handleSend}
								disabled={!content.trim() || isSending}
								loading={isSending}
								size="xs"
								radius="md"
								leftSection={<Send className="h-3 w-3" />}
							>
								Send
							</Button>

							<span className="text-xs text-muted-foreground">
								⌘ + Enter
							</span>
						</div>

						<div className="flex items-center gap-1">
							{/* Attachment button - triggers full editor */}
							<button
								type="button"
								onClick={onExpandToFullEditor}
								className="p-2 hover:bg-muted rounded-md text-muted-foreground hover:text-foreground transition-colors"
								title="Add attachment"
							>
								<Paperclip className="h-4 w-4" />
							</button>

							{/* Expand to full editor */}
							<button
								type="button"
								onClick={onExpandToFullEditor}
								className="p-2 hover:bg-muted rounded-md text-muted-foreground hover:text-foreground transition-colors"
								title="Expand to full editor"
							>
								<Maximize2 className="h-4 w-4" />
							</button>
						</div>
					</div>
				</div>
			)}
		</div>
	);
}
