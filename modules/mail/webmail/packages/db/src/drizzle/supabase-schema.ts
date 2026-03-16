import {
	jsonb,
	uuid,
	varchar,
	text,
	pgSchema,
	pgView,
} from "drizzle-orm/pg-core";

const authSchema = pgSchema("auth");
const storageSchema = pgSchema("storage");
const vaultSchema = pgSchema("vault");

export const users = authSchema.table("users", {
	id: uuid("id").primaryKey(),
	email: varchar(),
	raw_user_meta_data: jsonb(),
});

export const decryptedSecrets = pgView("decrypted_secrets", {
	id: uuid("id"),
	name: text("name"),
	description: text("description"),
	decrypted_secret: text("decrypted_secret"),
}).existing();

export const objects = storageSchema.table("objects", {
	id: uuid("id").primaryKey(),
	name: text(),
});
