import { ProviderSpec } from "@schema";
import {
	fetchDecryptedSecrets,
	SyncProvidersRow,
} from "@/lib/actions/dashboard";
import ProviderCard from "@/components/dashboard/providers/provider-card";
import { providerSecrets } from "@db";

type Props = {
	userProviders: SyncProvidersRow[];
	spec: ProviderSpec;
};

export default async function ProviderCardShell({
	userProviders,
	spec,
}: Props) {
	const userProvider = userProviders.find((p) => p.type === spec.key);

	// Only fetch secrets if userProvider exists to avoid passing "undefined" as parentId
	if (!userProvider) {
		return null;
	}

	const [decryptedSecret] = await fetchDecryptedSecrets({
		linkTable: providerSecrets,
		foreignCol: providerSecrets.providerId,
		secretIdCol: providerSecrets.secretId,
		parentId: userProvider.id,
	});

	return (
		<ProviderCard
			spec={spec}
			userProvider={userProvider}
			decryptedSecret={decryptedSecret}
		/>
	);
}
