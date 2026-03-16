/**
 * Stalwart-optimized IMAP connector for cluster-internal connections.
 *
 * Connects to Stalwart via cluster service DNS (no TLS overhead on
 * internal network). Uses STARTTLS on port 143 for the internal link.
 *
 * This module wraps the existing imap-client.ts pattern but with
 * Stalwart-specific defaults and connection settings.
 */

import { ImapFlow } from "imapflow";

export interface StalwartImapConfig {
	host: string;
	port: number;
	secure: boolean;
	user: string;
	pass: string;
}

const DEFAULT_STALWART_IMAP: Omit<StalwartImapConfig, "user" | "pass"> = {
	host: process.env.STALWART_SMTP_HOST || "stalwart.mail.svc.cluster.local",
	port: 143,
	secure: false, // STARTTLS on internal network
};

/**
 * Create an ImapFlow client configured for Stalwart cluster-internal access.
 */
export function createStalwartImapClient(
	identityId: string,
	credentials: { user: string; pass: string },
	config?: Partial<Omit<StalwartImapConfig, "user" | "pass">>,
): ImapFlow {
	const cfg = { ...DEFAULT_STALWART_IMAP, ...config };

	return new ImapFlow({
		host: cfg.host,
		port: cfg.port,
		secure: cfg.secure,
		auth: {
			user: credentials.user,
			pass: credentials.pass,
		},
		logger: {
			error(data: any) {
				console.error(`[IMAP-Stalwart:${identityId}]`, data.msg ?? data);
			},
			warn() {},
			info() {},
			debug() {},
		},
		logRaw: false,
		// Stalwart internal: more aggressive keepalive since network is reliable
		keepAlive: true,
	});
}

/**
 * Connect and register a Stalwart IMAP client in the instance map.
 *
 * Follows the same pattern as imap-client.ts initSmtpClient() but
 * with Stalwart-specific defaults.
 */
export async function initStalwartImapClient(
	identityId: string,
	credentials: { user: string; pass: string },
	imapInstances: Map<string, ImapFlow>,
	config?: Partial<Omit<StalwartImapConfig, "user" | "pass">>,
): Promise<ImapFlow | undefined> {
	// Reuse existing healthy connection
	const existing = imapInstances.get(identityId);
	if (existing?.authenticated && existing?.usable) {
		return existing;
	}

	const client = createStalwartImapClient(identityId, credentials, config);

	try {
		await client.connect();
	} catch (err) {
		console.error(`[IMAP-Stalwart:${identityId}] connect() failed:`, err);
		return undefined;
	}

	imapInstances.set(identityId, client);

	// NOOP keepalive every 3 minutes (internal network, more frequent is fine)
	const noopInterval = setInterval(
		async () => {
			try {
				if (client.usable) await client.noop();
			} catch (err) {
				console.error(`[IMAP-Stalwart:${identityId}] NOOP failed:`, err);
			}
		},
		3 * 60 * 1000,
	);

	const cleanup = (reason: string) => {
		clearInterval(noopInterval);
		imapInstances.delete(identityId);
		console.warn(
			`[IMAP-Stalwart:${identityId}] Disconnected (${reason}), will reconnect on next use`,
		);
	};

	client.once("close", () => cleanup("close"));
	client.once("error", (err) => {
		console.error(`[IMAP-Stalwart:${identityId}] Error:`, err);
		cleanup("error");
	});

	return client;
}

/**
 * Check if a given IMAP host points to our Stalwart instance.
 */
export function isStalwartHost(host: string): boolean {
	return (
		host === "stalwart.mail.svc.cluster.local" ||
		host.endsWith(".mail.svc.cluster.local") ||
		host === "mail.lurus.cn" ||
		host === "mail.lurus.local"
	);
}
