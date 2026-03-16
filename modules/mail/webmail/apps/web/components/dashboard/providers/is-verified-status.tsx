import { BadgeMinus, Verified } from "lucide-react";

function IsVerifiedStatus({
	verified,
	statusName,
}: {
	verified: boolean;
	statusName?: string;
}) {
	return (
		<>
			{verified ? (
				<div
					className={
						"flex justify-center gap-1 items-center mx-2 text-teal-600 dark:text-brand-foreground font-medium text-xs"
					}
				>
					<Verified size={16} />
					<span>{statusName} Verified</span>
				</div>
			) : (
				<div
					className={
						"flex justify-center gap-1 items-center mx-2 text-red-600 dark:text-brand-foreground font-medium text-xs"
					}
				>
					<BadgeMinus size={16} />
					<span>{statusName} Unverified</span>
				</div>
			)}
		</>
	);
}

export default IsVerifiedStatus;
