import React, { ReactNode } from "react";
import MailboxSearchHeader from "@/components/mailbox/mailbox-search-header";
import { KeyboardShortcutsProvider } from "@/components/mailbox/keyboard-shortcuts-provider";

type LayoutProps = {
	children: ReactNode;
	thread: ReactNode;
	params: Promise<{
		identityPublicId: string;
		mailboxSlug: string;
	}>;
};

export default async function DashboardLayout({
	children,
	thread,
	params,
}: LayoutProps) {
	const { identityPublicId, mailboxSlug } = await params;

	return (
		<KeyboardShortcutsProvider
			identityPublicId={identityPublicId}
			mailboxSlug={mailboxSlug}
		>
			<MailboxSearchHeader params={params} />

			{thread}
			{children}
		</KeyboardShortcutsProvider>
	);
}
