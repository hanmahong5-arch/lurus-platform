export const dynamic = "force-dynamic"; // or

import React, { useEffect, useState } from "react";
import IsVerifiedStatus from "../providers/is-verified-status";
import {
	FetchUserIdentitiesResult,
	getIdentityById,
} from "@/lib/actions/dashboard";

function EmailIdentityStatus({
	userIdentity,
}: {
	userIdentity: FetchUserIdentitiesResult[number];
}) {
	const [incoming, setIncoming] = useState<boolean>(false);
	const evaluateStatus = async () => {
		if (userIdentity.identities.domainIdentityId) {
			const domain = await getIdentityById(
				userIdentity.identities.domainIdentityId,
			);
			setIncoming(!!domain.incomingDomain);
		}
	};

	useEffect(() => {
		if (userIdentity) {
			evaluateStatus();
		}
	}, [userIdentity]);

	return (
		<>
			<IsVerifiedStatus verified={true} statusName="Outgoing" />
			<IsVerifiedStatus verified={incoming} statusName="Incoming" />
		</>
	);
}

export default EmailIdentityStatus;
