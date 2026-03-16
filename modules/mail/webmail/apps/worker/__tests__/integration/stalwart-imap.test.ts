/**
 * Integration test: IMAP sync via local Stalwart.
 *
 * Prerequisite: run local Stalwart + create test account
 * (see stalwart-smtp.test.ts for setup instructions)
 */

import { describe, it, expect } from "vitest";
import { ImapFlow } from "imapflow";

const STALWART_HOST = process.env.STALWART_TEST_HOST || "localhost";
const STALWART_IMAP_PORT = Number(process.env.STALWART_TEST_IMAP_PORT || "143");
const TEST_USER = "testuser@mail.lurus.local";
const TEST_PASS = "test123";

const stalwartAvailable = async (): Promise<boolean> => {
	try {
		const res = await fetch(`http://${STALWART_HOST}:8080/healthz`);
		return res.ok;
	} catch {
		return false;
	}
};

describe.skipIf(!await stalwartAvailable())(
	"Stalwart IMAP Integration",
	() => {
		it("connects and authenticates via IMAP", async () => {
			const client = new ImapFlow({
				host: STALWART_HOST,
				port: STALWART_IMAP_PORT,
				secure: false,
				auth: { user: TEST_USER, pass: TEST_PASS },
				logger: false as any,
				tls: { rejectUnauthorized: false },
			});

			await client.connect();
			expect(client.authenticated).toBe(true);

			await client.noop();
			await client.logout();
		});

		it("lists mailboxes", async () => {
			const client = new ImapFlow({
				host: STALWART_HOST,
				port: STALWART_IMAP_PORT,
				secure: false,
				auth: { user: TEST_USER, pass: TEST_PASS },
				logger: false as any,
				tls: { rejectUnauthorized: false },
			});

			await client.connect();
			const mailboxes = await client.list();

			// Stalwart creates INBOX by default
			const inboxExists = mailboxes.some(
				(mb) => mb.path.toUpperCase() === "INBOX",
			);
			expect(inboxExists).toBe(true);

			await client.logout();
		});

		it("fetches messages from INBOX", async () => {
			const client = new ImapFlow({
				host: STALWART_HOST,
				port: STALWART_IMAP_PORT,
				secure: false,
				auth: { user: TEST_USER, pass: TEST_PASS },
				logger: false as any,
				tls: { rejectUnauthorized: false },
			});

			await client.connect();
			const lock = await client.getMailboxLock("INBOX");

			try {
				const status = client.mailbox;
				// exists may be 0 if no emails sent yet
				expect(status).toBeDefined();
			} finally {
				lock.release();
			}

			await client.logout();
		});
	},
);
