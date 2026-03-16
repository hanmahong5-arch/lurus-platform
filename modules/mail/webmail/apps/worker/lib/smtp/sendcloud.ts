/**
 * SendCloud SMTP relay integration for China domestic email delivery.
 *
 * SendCloud (https://www.sendcloud.net) provides reliable SMTP relay
 * for delivering to Chinese email providers (QQ, 163, 126, etc.)
 * which may reject direct delivery from overseas/new IPs.
 */

import nodemailer from "nodemailer";
import type { Transporter } from "nodemailer";
import type SMTPTransport from "nodemailer/lib/smtp-transport";

export interface SendCloudConfig {
	host: string;
	port: number;
	secure: boolean;
	apiUser: string;
	apiKey: string;
}

export interface SendCloudEmailOptions {
	from: string;
	to: string[];
	subject: string;
	text?: string;
	html?: string;
	inReplyTo?: string;
	references?: string[];
	attachments?: {
		filename: string;
		content: Buffer;
		contentType: string;
	}[];
}

export interface SendCloudResult {
	success: boolean;
	messageId?: string;
	error?: string;
}

export class SendCloudMailer {
	private transporter: Transporter;
	private config: SendCloudConfig;

	constructor(config: SendCloudConfig) {
		this.config = config;
		this.transporter = nodemailer.createTransport({
			host: config.host,
			port: config.port,
			secure: config.secure,
			auth: {
				user: config.apiUser,
				pass: config.apiKey,
			},
			pool: true,
			maxConnections: 5,
			maxMessages: 100,
		} as SMTPTransport.Options);
	}

	/**
	 * Send an email via SendCloud SMTP relay.
	 */
	async send(opts: SendCloudEmailOptions): Promise<SendCloudResult> {
		try {
			const headers: Record<string, string> = {};
			if (opts.inReplyTo) headers["In-Reply-To"] = opts.inReplyTo;
			if (opts.references?.length)
				headers["References"] = opts.references.join(" ");

			const info = await this.transporter.sendMail({
				from: opts.from,
				to: opts.to.join(","),
				subject: opts.subject,
				text: opts.text || undefined,
				html: opts.html || undefined,
				headers,
				attachments: opts.attachments,
			});

			return {
				success: true,
				messageId: String(info.messageId || ""),
			};
		} catch (err: any) {
			console.error("[SendCloud] Send failed:", err?.message ?? err);
			return {
				success: false,
				error: err?.message ?? String(err),
			};
		}
	}

	/**
	 * Verify the SendCloud SMTP connection.
	 */
	async verify(): Promise<boolean> {
		try {
			return !!(await this.transporter.verify());
		} catch {
			return false;
		}
	}

	/**
	 * Close the transporter pool connections.
	 */
	close(): void {
		try {
			(this.transporter as any).close?.();
		} catch {
			// best-effort
		}
	}
}

/**
 * Create a SendCloudMailer from environment variables.
 */
export function createSendCloudMailer(env?: {
	SENDCLOUD_HOST?: string;
	SENDCLOUD_PORT?: string;
	SENDCLOUD_API_USER?: string;
	SENDCLOUD_API_KEY?: string;
}): SendCloudMailer {
	const e = env ?? process.env;
	return new SendCloudMailer({
		host: (e as any).SENDCLOUD_HOST || "smtp.sendcloud.net",
		port: Number.parseInt((e as any).SENDCLOUD_PORT || "587", 10),
		secure: false,
		apiUser: (e as any).SENDCLOUD_API_USER || "",
		apiKey: (e as any).SENDCLOUD_API_KEY || "",
	});
}
