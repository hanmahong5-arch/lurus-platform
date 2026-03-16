/**
 * Integration test: End-to-end email routing.
 *
 * Tests the full routing pipeline:
 * 1. routeEmail() determines channel
 * 2. Email sent via appropriate channel
 * 3. Received in Stalwart INBOX (for local recipients)
 *
 * Prerequisite: local Stalwart running
 */

import { describe, it, expect } from "vitest";
import { routeEmail, classifyRecipients } from "../../../lib/smtp/router";
import { StalwartAdminApi } from "../../../lib/admin/stalwart-api";
import nodemailer from "nodemailer";
import { ImapFlow } from "imapflow";

const STALWART_HOST = process.env.STALWART_TEST_HOST || "localhost";
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
	"Email Routing Integration",
	() => {
		it("routes local email through Stalwart and delivers", async () => {
			// 1. Send email via SMTP
			const transporter = nodemailer.createTransport({
				host: STALWART_HOST,
				port: 587,
				secure: false,
				auth: { user: TEST_USER, pass: TEST_PASS },
				tls: { rejectUnauthorized: false },
			});

			const uniqueSubject = `Routing Test ${Date.now()}`;
			await transporter.sendMail({
				from: TEST_USER,
				to: TEST_USER,
				subject: uniqueSubject,
				text: "End-to-end routing test",
			});
			(transporter as any).close?.();

			// 2. Wait for delivery (local should be instant)
			await new Promise((r) => setTimeout(r, 2000));

			// 3. Verify via IMAP
			const client = new ImapFlow({
				host: STALWART_HOST,
				port: 143,
				secure: false,
				auth: { user: TEST_USER, pass: TEST_PASS },
				logger: false as any,
				tls: { rejectUnauthorized: false },
			});

			await client.connect();
			const lock = await client.getMailboxLock("INBOX");

			try {
				// Search for our specific message
				const uids = await client.search({
					subject: uniqueSubject,
				});
				expect(uids.length).toBeGreaterThanOrEqual(1);
			} finally {
				lock.release();
			}

			await client.logout();
		});

		it("Stalwart admin API can provision accounts", async () => {
			const api = new StalwartAdminApi({
				baseUrl: `http://${STALWART_HOST}:8080`,
				adminUser: "admin",
				adminPassword: "changeme",
			});

			const ok = await api.healthCheck();
			expect(ok).toBe(true);

			// Attempt to list accounts
			const result = await api.listAccounts();
			expect(result.ok).toBe(true);
			expect(Array.isArray(result.data)).toBe(true);
		});

		it("routing classification works correctly", () => {
			const result = classifyRecipients([
				"user@qq.com",
				"user@gmail.com",
				"user@163.com",
				"user@outlook.com",
				"user@lurus.cn",
			]);

			expect(result.china).toEqual(["user@qq.com", "user@163.com"]);
			expect(result.international).toEqual([
				"user@gmail.com",
				"user@outlook.com",
				"user@lurus.cn",
			]);
		});

		it("routes mixed recipients through SendCloud", () => {
			const route = routeEmail([
				"user@qq.com",
				"user@gmail.com",
			]);
			expect(route.channel).toBe("sendcloud");
		});

		it("routes pure international through Stalwart", () => {
			const route = routeEmail([
				"user@gmail.com",
				"user@outlook.com",
			]);
			expect(route.channel).toBe("stalwart");
		});
	},
);
