import { describe, it, expect, vi, beforeEach } from "vitest";
import { SendCloudMailer, createSendCloudMailer } from "../../../lib/smtp/sendcloud";

// Mock nodemailer
vi.mock("nodemailer", () => ({
	default: {
		createTransport: vi.fn(() => ({
			sendMail: vi.fn().mockResolvedValue({ messageId: "<test-id@sendcloud>" }),
			verify: vi.fn().mockResolvedValue(true),
			close: vi.fn(),
		})),
	},
}));

describe("SendCloudMailer", () => {
	let mailer: SendCloudMailer;

	beforeEach(() => {
		mailer = new SendCloudMailer({
			host: "smtp.sendcloud.net",
			port: 587,
			secure: false,
			apiUser: "test-user",
			apiKey: "test-key",
		});
	});

	it("sends email successfully", async () => {
		const result = await mailer.send({
			from: "sender@lurus.cn",
			to: ["user@qq.com"],
			subject: "Test",
			text: "Hello",
		});
		expect(result.success).toBe(true);
		expect(result.messageId).toBeDefined();
	});

	it("includes reply headers when provided", async () => {
		const result = await mailer.send({
			from: "sender@lurus.cn",
			to: ["user@163.com"],
			subject: "Re: Test",
			html: "<p>Reply</p>",
			inReplyTo: "<orig-id@example.com>",
			references: ["<ref1@example.com>", "<ref2@example.com>"],
		});
		expect(result.success).toBe(true);
	});

	it("verifies connection", async () => {
		const ok = await mailer.verify();
		expect(ok).toBe(true);
	});

	it("closes without error", () => {
		expect(() => mailer.close()).not.toThrow();
	});
});

describe("createSendCloudMailer", () => {
	it("creates mailer from env vars", () => {
		const mailer = createSendCloudMailer({
			SENDCLOUD_HOST: "custom-host",
			SENDCLOUD_PORT: "465",
			SENDCLOUD_API_USER: "my-user",
			SENDCLOUD_API_KEY: "my-key",
		});
		expect(mailer).toBeInstanceOf(SendCloudMailer);
	});

	it("uses defaults when env vars missing", () => {
		const mailer = createSendCloudMailer({});
		expect(mailer).toBeInstanceOf(SendCloudMailer);
	});
});
