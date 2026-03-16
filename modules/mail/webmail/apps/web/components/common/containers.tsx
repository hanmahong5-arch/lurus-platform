type ContainerVariant = "wide" | "medium" | "narrow" | "full";

export function Container({
	children,
	variant = "medium",
	className = "",
}: {
	children: React.ReactNode;
	variant?: ContainerVariant;
	className?: string;
}) {
	const base = "w-full mx-auto px-4 sm:px-6 lg:px-8";

	const variants: Record<ContainerVariant, string> = {
		wide: "min-w-full sm:min-w-2xl lg:min-w-5xl max-w-7xl",
		medium: "min-w-full sm:min-w-xl lg:min-w-3xl max-w-3xl lg:max-w-5xl",
		narrow: "min-w-full sm:min-w-md lg:min-w-xl max-w-xl lg:max-w-3xl",
		full: "min-w-full max-w-none",
	};

	return (
		<div className={`${base} ${variants[variant]} ${className}`}>
			{children}
		</div>
	);
}
