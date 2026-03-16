import { eq } from "drizzle-orm";
import { PgTable, PgColumn } from "drizzle-orm/pg-core";

import { db } from "./init-db";
import { providers, secretsMeta, smtpAccounts } from "./schema";
import { getSecretAdmin } from "./vault";

type FetchArgs = {
	linkTable: PgTable;
	foreignCol: PgColumn;
	secretIdCol: PgColumn;
	ownerId: string;
	parentId?: string;
};

export async function decryptAdminSecrets({
	linkTable,
	foreignCol,
	secretIdCol,
	ownerId,
	parentId,
}: FetchArgs) {
	let q = db
		.select({
			linkRow: linkTable,
			metaId: secretsMeta.id,
			provider: providers,
			smtpAccount: smtpAccounts,
		})
		.from(linkTable)
		.leftJoin(secretsMeta, eq(secretIdCol as any, secretsMeta.id))
		.leftJoin(providers, eq(foreignCol as any, providers.id))
		.leftJoin(smtpAccounts, eq(foreignCol as any, smtpAccounts.id))
		.where(eq(secretsMeta.ownerId, ownerId))
		.$dynamic();

	if (parentId) {
		q = q.where(eq(foreignCol as any, parentId));
	}

	const rows = await q;

	return Promise.all(
		rows.map(async (r) => {
			const metaId = String(r.metaId);
			const { vault } = await getSecretAdmin(metaId);

			return {
				linkRow: r.linkRow,
				metaId,
				vault,
				providerId: (r as any)?.linkRow?.providerId,
				accountId: (r as any)?.linkRow?.accountId,
				provider: r.provider,
				smtpAccount: r.smtpAccount,
			};
		}),
	);
}
