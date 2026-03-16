import { currentSession } from "@/lib/actions/auth";
import { createDrizzleSupabaseClient } from "@db";

export const rlsClient = async () => {
	const session = await currentSession();
	const { rls } = await createDrizzleSupabaseClient(session);
	return rls;
};

export const adminClient = async () => {
	const session = await currentSession();
	const { admin } = await createDrizzleSupabaseClient(session);
	return admin;
};
