import * as React from "react";

// Lightweight tooltip primitives without Radix dependency
// For full-featured tooltips, use @radix-ui/react-tooltip at the app level

const TooltipContext = React.createContext<{
	open: boolean;
	setOpen: (open: boolean) => void;
}>({ open: false, setOpen: () => {} });

function TooltipProvider({ children }: { children: React.ReactNode }) {
	return <>{children}</>;
}

function Tooltip({ children }: { children: React.ReactNode }) {
	const [open, setOpen] = React.useState(false);
	return (
		<TooltipContext.Provider value={{ open, setOpen }}>
			<div className="relative inline-flex">{children}</div>
		</TooltipContext.Provider>
	);
}

function TooltipTrigger({
	children,
	asChild,
	...props
}: React.ComponentProps<"button"> & { asChild?: boolean }) {
	const { setOpen } = React.useContext(TooltipContext);
	return (
		<button
			type="button"
			onMouseEnter={() => setOpen(true)}
			onMouseLeave={() => setOpen(false)}
			onFocus={() => setOpen(true)}
			onBlur={() => setOpen(false)}
			{...props}
		>
			{children}
		</button>
	);
}

function TooltipContent({
	children,
	className,
	...props
}: React.ComponentProps<"div">) {
	const { open } = React.useContext(TooltipContext);
	if (!open) return null;
	return (
		<div
			role="tooltip"
			className={`absolute bottom-full left-1/2 z-50 mb-2 -translate-x-1/2 rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground shadow-md ${className ?? ""}`}
			{...props}
		>
			{children}
		</div>
	);
}

export { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent };
