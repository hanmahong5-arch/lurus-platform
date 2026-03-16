import { MessageEntity } from "./drizzle-types";
import { AddressObjectJSON } from "@schema";

export const fromName = (message: MessageEntity) => {
	const from: AddressObjectJSON | string = message.from as any;

	if (!from) return null;

	if (typeof from === "string") {
		// string form has no display name, return just the email part before @ if you want
		return from.split("@")[0] ?? null;
	}

	// object form with parsed name
	return from.value?.[0]?.name ?? null;
};

export const fromAddress = (message: MessageEntity) => {
	const from: AddressObjectJSON = message.from as any;

	if (!from) return null;

	if (typeof from === "string") {
		return from;
	}

	// object form with parsed email
	return from.value?.[0]?.address ?? null;
};
