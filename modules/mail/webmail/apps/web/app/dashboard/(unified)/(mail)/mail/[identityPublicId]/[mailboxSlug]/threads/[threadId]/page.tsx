import React from "react";
import {fetchMailbox, fetchThreadMailSubscriptions, fetchWebMailThreadDetail} from "@/lib/actions/mailbox";
import ThreadItem from "@/components/mailbox/default/thread-item";
import ThreadPageClient from "@/components/mailbox/default/thread-page-client";
import {MessageEntity} from "@db";

async function Page({
	params,
}: {
	params: Promise<{
		identityPublicId: string;
		mailboxSlug: string;
		threadId: string;
	}>;
}) {
	const { threadId, identityPublicId, mailboxSlug } = await params;
	const { activeMailbox, mailboxSync } = await fetchMailbox(
		identityPublicId,
		mailboxSlug,
	);
	const activeThread = await fetchWebMailThreadDetail(threadId);

    const { byMessageId } = await fetchThreadMailSubscriptions({
        ownerId: activeMailbox.ownerId,
        messages:
            activeThread?.messages.map((m: MessageEntity) => ({
                id: m.id,
                headersJson: m.headersJson,
            })) ?? [],
    });

	// Prepare message data for client component
	const messagesForClient = activeThread?.messages.map((m) => ({
		id: m.id,
		seen: m.seen,
		from: m.from,
		createdAt: m.createdAt,
		text: m.text,
		html: m.html,
	})) ?? [];

	return (
		<ThreadPageClient messages={messagesForClient}>
			{activeThread?.messages.map((message, threadIndex) => (
				<ThreadItem
					key={message.id}
					message={message}
					threadIndex={threadIndex}
					numberOfMessages={activeThread.messages.length}
					threadId={threadId}
					activeMailboxId={activeMailbox.id}
					markSmtp={!!mailboxSync}
					identityPublicId={identityPublicId}
					mailSubscription={byMessageId.get(message.id) ?? null}
				/>
			))}
		</ThreadPageClient>
	);
}

export default Page;
