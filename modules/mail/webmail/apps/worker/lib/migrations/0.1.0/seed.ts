/**
 * Migration 0.1.0 - Stalwart mail account provisioning
 *
 * Auto-provisions a complete @lurus.cn mailbox for each user:
 * 1. Create Stalwart account via management API
 * 2. Store IMAP/SMTP credentials in vault
 * 3. Create SMTP account + identity in DB
 * 4. Initialize system mailboxes (inbox, sent, trash, spam)
 */

import {
	db,
	identities,
	smtpAccounts,
	smtpAccountSecrets,
	mailboxes,
	createSecretAdmin,
} from "@db";
import { and, eq } from "drizzle-orm";
import { SYSTEM_MAILBOXES, MailboxKindDisplay } from "@schema";
import { createStalwartApi } from "../../admin/stalwart-api";
import crypto from "node:crypto";

const MAIL_DOMAIN = "lurus.cn";
const STALWART_HOST = process.env.STALWART_SMTP_HOST || "stalwart.mail.svc.cluster.local";
const STALWART_IMAP_HOST = STALWART_HOST;

function generatePassword(length = 24): string {
	return crypto.randomBytes(length).toString("base64url").slice(0, length);
}

function slugify(kind: string): string {
	return kind.toLowerCase().replace(/\s+/g, "-");
}

/**
 * Derive a unique Stalwart username from the user's signup email.
 */
async function deriveUsername(userId: string): Promise<string> {
	const [identity] = await db
		.select()
		.from(identities)
		.where(eq(identities.ownerId, userId))
		.limit(1);

	if (identity?.value) {
		const localPart = identity.value.split("@")[0]?.toLowerCase();
		if (localPart) {
			return localPart.replace(/[^a-z0-9._-]/g, "");
		}
	}

	return `user-${userId.slice(0, 12)}`;
}

export default async function seed({ userId }: { userId: string }) {
	// Skip if Stalwart admin password not configured
	if (!process.env.STALWART_ADMIN_PASSWORD) {
		console.info(
			"[Migration 0.1.0] STALWART_ADMIN_PASSWORD not set, skipping",
		);
		return;
	}

	// Check if user already has a @lurus.cn identity
	const [existingIdentity] = await db
		.select()
		.from(identities)
		.where(
			and(
				eq(identities.ownerId, userId),
				eq(identities.kind, "email"),
			),
		)
		.limit(10);

	if (existingIdentity?.value?.endsWith(`@${MAIL_DOMAIN}`)) {
		console.info(
			`[Migration 0.1.0] User already has @${MAIL_DOMAIN} identity, skipping`,
		);
		return;
	}

	const api = createStalwartApi();
	const username = await deriveUsername(userId);
	const email = `${username}@${MAIL_DOMAIN}`;
	const password = generatePassword();

	// --- Step 1: Create Stalwart account ---
	const existing = await api.getAccount(username);
	if (!existing.ok || !existing.data) {
		const result = await api.provisionUser({
			username,
			password,
			email,
			displayName: username,
		});

		if (!result.ok) {
			console.error(
				`[Migration 0.1.0] Stalwart provision failed: ${result.error}`,
			);
			return; // non-fatal
		}
		console.info(`[Migration 0.1.0] Created Stalwart account: ${email}`);
	} else {
		console.info(
			`[Migration 0.1.0] Stalwart account '${username}' exists, reusing`,
		);
	}

	// --- Step 2: Store SMTP/IMAP credentials in vault ---
	const smtpConfig = {
		SMTP_HOST: STALWART_HOST,
		SMTP_PORT: "587",
		SMTP_USERNAME: email,
		SMTP_PASSWORD: password,
		SMTP_SECURE: "false",
		IMAP_HOST: STALWART_IMAP_HOST,
		IMAP_PORT: "143",
		IMAP_USERNAME: email,
		IMAP_PASSWORD: password,
		IMAP_SECURE: "false",
	};

	const secretMeta = await createSecretAdmin({
		ownerId: userId,
		name: `stalwart-${username}`,
		value: JSON.stringify(smtpConfig),
		description: `Stalwart mail credentials for ${email}`,
	});

	// --- Step 3: Create SMTP account + link to secret ---
	const [smtpAccount] = await db
		.insert(smtpAccounts)
		.values({ ownerId: userId })
		.returning();

	await db.insert(smtpAccountSecrets).values({
		accountId: smtpAccount.id,
		secretId: secretMeta.id,
	});

	// --- Step 4: Create identity ---
	const [newIdentity] = await db
		.insert(identities)
		.values({
			ownerId: userId,
			kind: "email",
			value: email,
			displayName: username,
			smtpAccountId: smtpAccount.id,
			status: "verified",
			metaData: {
				stalwart: true,
				dailyQuota: 500,
			},
		})
		.onConflictDoNothing()
		.returning();

	if (!newIdentity) {
		console.info(
			`[Migration 0.1.0] Identity ${email} already exists (conflict), skipping mailboxes`,
		);
		return;
	}

	// --- Step 5: Create system mailboxes ---
	const mailboxRows = SYSTEM_MAILBOXES.map((m) => ({
		ownerId: userId,
		identityId: newIdentity.id,
		kind: m.kind,
		name: MailboxKindDisplay[m.kind],
		slug: slugify(m.kind),
		isDefault: m.isDefault,
	}));

	await db
		.insert(mailboxes)
		.values(mailboxRows)
		.onConflictDoNothing();

	console.info(
		`[Migration 0.1.0] Provisioned ${email} with ${mailboxRows.length} mailboxes`,
	);
}
