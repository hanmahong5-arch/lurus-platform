"use client";
import * as React from "react";
import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { MailboxEntity, MailboxSyncEntity } from "@db";
import { PublicConfig } from "@schema";
import {
	FetchIdentityMailboxListResult,
	FetchMailboxThreadsResult,
} from "@/lib/actions/mailbox";
import {
	FetchLabelsResult,
	FetchMailboxThreadLabelsResult,
} from "@/lib/actions/labels";
import MailListHeader from "@/components/mailbox/default/mail-list-header";
import WebmailListItem from "@/components/mailbox/default/webmail-list-item";
import { DynamicContextProvider } from "@/hooks/use-dynamic-context";
import { useMediaQuery } from "@mantine/hooks";
import WebmailListItemMobile from "@/components/mailbox/default/webmail-list-item-mobile";
import { useParams } from "next/navigation";

type WebListProps = {
	mailboxThreads: FetchMailboxThreadsResult;
	publicConfig: PublicConfig;
	activeMailbox: MailboxEntity;
	identityPublicId: string;
	identityMailboxes: FetchIdentityMailboxListResult;
	globalLabels: FetchLabelsResult;
	labelsByThreadId: FetchMailboxThreadLabelsResult;
	mailboxSync?: MailboxSyncEntity;
};

export default function WebmailList({
	mailboxThreads,
	activeMailbox,
	identityPublicId,
	mailboxSync,
	publicConfig,
	identityMailboxes,
	globalLabels,
	labelsByThreadId,
}: WebListProps) {
	const isMobile = useMediaQuery("(max-width: 768px)");
	const params = useParams();
	const parentRef = useRef<HTMLDivElement>(null);

	// Row height: mobile items are taller than desktop items
	const estimatedRowHeight = isMobile ? 72 : 56;

	const virtualizer = useVirtualizer({
		count: mailboxThreads.length,
		getScrollElement: () => parentRef.current,
		estimateSize: () => estimatedRowHeight,
		overscan: 5, // Pre-render 5 items above and below viewport
	});

	const virtualItems = virtualizer.getVirtualItems();

	return (
		<div className={params?.threadId ? "hidden" : ""}>
			<DynamicContextProvider
				initialState={{
					selectedThreadIds: new Set(),
					activeMailbox,
					identityPublicId,
				}}
			>
				{mailboxThreads.length === 0 ? (
					<div className="p-4 text-center text-base text-muted-foreground">
						No messages in{" "}
						<span className={"lowercase"}>{activeMailbox.name}</span>
					</div>
				) : (
					<div className="rounded-xl border bg-background/50 z-[50]">
						<MailListHeader
							mailboxThreads={mailboxThreads}
							mailboxSync={mailboxSync}
							publicConfig={publicConfig}
							identityMailboxes={identityMailboxes}
							activeMailbox={activeMailbox}
						/>

						{/* Virtual scroll container */}
						<div
							ref={parentRef}
							className="max-h-[calc(100vh-180px)] overflow-auto"
						>
							<ul
								role="list"
								className="divide-y rounded-4xl relative"
								style={{
									height: `${virtualizer.getTotalSize()}px`,
								}}
							>
								{virtualItems.map((virtualRow) => {
									const mailboxThreadItem = mailboxThreads[virtualRow.index];
									return (
										<li
											key={
												mailboxThreadItem.threadId +
												mailboxThreadItem.mailboxId
											}
											data-index={virtualRow.index}
											ref={virtualizer.measureElement}
											className="absolute left-0 right-0"
											style={{
												transform: `translateY(${virtualRow.start}px)`,
											}}
										>
											{isMobile ? (
												<WebmailListItemMobile
													mailboxThreadItem={mailboxThreadItem}
													activeMailbox={activeMailbox}
													identityPublicId={identityPublicId}
													mailboxSync={mailboxSync}
													labelsByThreadId={labelsByThreadId}
												/>
											) : (
												<WebmailListItem
													mailboxThreadItem={mailboxThreadItem}
													activeMailbox={activeMailbox}
													identityPublicId={identityPublicId}
													mailboxSync={mailboxSync}
													globalLabels={globalLabels}
													labelsByThreadId={labelsByThreadId}
												/>
											)}
										</li>
									);
								})}
							</ul>
						</div>
					</div>
				)}
			</DynamicContextProvider>
		</div>
	);
}
