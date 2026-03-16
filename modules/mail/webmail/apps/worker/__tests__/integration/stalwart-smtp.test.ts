/**
 * Integration test: SMTP send via local Stalwart.
 *
 * Prerequisite: run local Stalwart via Docker Compose
 *   docker compose -f deploy/stalwart/docker-compose.yml up -d
 *
 * Then create a test account:
 *   curl -X POST http://localhost:8080/api/principal \
 *     -u admin:changeme \
 *     -H 'Content-Type: application/json' \
 *     -d '{"name":"testuser","type":"individual","secrets":["test123"],"emails":["testuser@mail.lurus.local"]}'
 */

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import nodemailer from "nodemailer";

const STALWART_HOST = process.env.STALWART_TEST_HOST || "localhost";
const STALWART_SMTP_PORT = Number(process.env.STALWART_TEST_SMTP_PORT || "587");
const TEST_USER = "testuser@mail.lurus.local";
const TEST_PASS = "test123";

// Skip if Stalwart not available
const stalwartAvailable = async (): Promise<boolean> => {
	try {
		const res = await fetch(`http://${STALWART_HOST}:8080/healthz`);
		return res.ok;
	} catch {
		return false;
	}
};

describe.skipIf(!await stalwartAvailable())(
	"Stalwart SMTP Integration",
	() => {
		let transporter: nodemailer.Transporter;

		beforeAll(() => {
			transporter = nodemailer.createTransport({
				host: STALWART_HOST,
				port: STALWART_SMTP_PORT,
				secure: false,
				auth: {
					user: TEST_USER,
					pass: TEST_PASS,
				},
				tls: {
					rejectUnauthorized: false, // self-signed cert in dev
				},
			});
		});

		afterAll(() => {
			try {
				(transporter as any).close?.();
			} catch {}
		});

		it("verifies SMTP connection", async () => {
			const ok = await transporter.verify();
			expect(ok).toBe(true);
		});

		it("sends a test email", async () => {
			const info = await transporter.sendMail({
				from: TEST_USER,
				to: TEST_USER,
				subject: "Integration Test",
				text: "This is an automated integration test email.",
			});

			expect(info.messageId).toBeDefined();
			expect(info.accepted).toContain(TEST_USER);
		});

		it("sends email with attachments", async () => {
			const info = await transporter.sendMail({
				from: TEST_USER,
				to: TEST_USER,
				subject: "Integration Test with Attachment",
				text: "See attached.",
				attachments: [
					{
						filename: "test.txt",
						content: Buffer.from("Hello from integration test"),
						contentType: "text/plain",
					},
				],
			});

			expect(info.messageId).toBeDefined();
		});

		it("sends email with reply headers", async () => {
			const info = await transporter.sendMail({
				from: TEST_USER,
				to: TEST_USER,
				subject: "Re: Integration Test",
				text: "This is a reply.",
				headers: {
					"In-Reply-To": "<original-msg-id@mail.lurus.local>",
					References: "<original-msg-id@mail.lurus.local>",
				},
			});

			expect(info.messageId).toBeDefined();
		});
	},
);
