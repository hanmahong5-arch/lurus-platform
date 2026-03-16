import { z } from "zod";

export const identityTypesList = ["domain", "email"] as const;
export const identityStatusList = [
	"unverified",
	"pending",
	"verified",
	"failed",
] as const;
export const IdentitesEnum = z.enum(identityTypesList);
export type Identities = z.infer<typeof IdentitesEnum>;

export const IdentityStatusEnum = z.enum(identityStatusList);
export type IdentityStatus = z.infer<typeof IdentityStatusEnum>;

export const IdentityStatusDisplay: Record<IdentityStatus, string> = {
	unverified: "Not verified",
	pending: "DNS not set up yet",
	verified: "Verified",
	failed: "Verification failed",
};

export const IdentityStatusMeta: Record<
	IdentityStatus,
	{
		label: string;
		note: string;
	}
> = {
	unverified: {
		label: "Not verified",
		note: "Verification has not been initiated yet.",
	},
	pending: {
		label: "DNS not set up yet",
		note: "Add DNS records at your DNS host to continue verification.",
	},
	verified: {
		label: "Verified",
		note: "This identity is fully verified and ready to use.",
	},
	failed: {
		label: "Verification failed",
		note: "Check your DNS records or restart the verification process.",
	},
};
