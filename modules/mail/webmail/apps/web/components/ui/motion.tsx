"use client";
import React from "react";
import { motion, AnimatePresence, Variants } from "framer-motion";

// Animation variants for common patterns
export const fadeInUp: Variants = {
	initial: { opacity: 0, y: 10 },
	animate: { opacity: 1, y: 0 },
	exit: { opacity: 0, y: -10 },
};

export const fadeIn: Variants = {
	initial: { opacity: 0 },
	animate: { opacity: 1 },
	exit: { opacity: 0 },
};

export const slideInLeft: Variants = {
	initial: { opacity: 0, x: -20 },
	animate: { opacity: 1, x: 0 },
	exit: { opacity: 0, x: 20 },
};

export const slideInRight: Variants = {
	initial: { opacity: 0, x: 20 },
	animate: { opacity: 1, x: 0 },
	exit: { opacity: 0, x: -20 },
};

export const scaleIn: Variants = {
	initial: { opacity: 0, scale: 0.95 },
	animate: { opacity: 1, scale: 1 },
	exit: { opacity: 0, scale: 0.95 },
};

export const collapseVariants: Variants = {
	initial: { height: 0, opacity: 0 },
	animate: { height: "auto", opacity: 1 },
	exit: { height: 0, opacity: 0 },
};

// Stagger children animation
export const staggerContainer: Variants = {
	initial: {},
	animate: {
		transition: {
			staggerChildren: 0.05,
		},
	},
};

export const staggerItem: Variants = {
	initial: { opacity: 0, y: 10 },
	animate: { opacity: 1, y: 0 },
};

// Reusable motion components
export const MotionDiv = motion.div;
export const MotionLi = motion.li;
export const MotionUl = motion.ul;
export const MotionSpan = motion.span;

// Animated list item for mail list
type AnimatedListItemProps = {
	children: React.ReactNode;
	index?: number;
	className?: string;
	layoutId?: string;
	onClick?: () => void;
};

export function AnimatedListItem({
	children,
	index = 0,
	className,
	layoutId,
	onClick,
}: AnimatedListItemProps) {
	return (
		<motion.div
			layout
			layoutId={layoutId}
			initial={{ opacity: 0, y: 10 }}
			animate={{ opacity: 1, y: 0 }}
			exit={{ opacity: 0, x: -100, height: 0 }}
			transition={{
				duration: 0.2,
				delay: Math.min(index * 0.02, 0.2), // Cap delay at 200ms
			}}
			className={className}
			onClick={onClick}
		>
			{children}
		</motion.div>
	);
}

// Collapsible section with animation
type CollapsibleSectionProps = {
	isOpen: boolean;
	children: React.ReactNode;
	className?: string;
};

export function CollapsibleSection({
	isOpen,
	children,
	className,
}: CollapsibleSectionProps) {
	return (
		<AnimatePresence mode="wait">
			{isOpen && (
				<motion.div
					initial={{ height: 0, opacity: 0 }}
					animate={{ height: "auto", opacity: 1 }}
					exit={{ height: 0, opacity: 0 }}
					transition={{ duration: 0.2, ease: "easeInOut" }}
					className={className}
					style={{ overflow: "hidden" }}
				>
					{children}
				</motion.div>
			)}
		</AnimatePresence>
	);
}

// Fade in on mount
type FadeInProps = {
	children: React.ReactNode;
	delay?: number;
	duration?: number;
	className?: string;
};

export function FadeIn({
	children,
	delay = 0,
	duration = 0.3,
	className,
}: FadeInProps) {
	return (
		<motion.div
			initial={{ opacity: 0 }}
			animate={{ opacity: 1 }}
			transition={{ delay, duration }}
			className={className}
		>
			{children}
		</motion.div>
	);
}

// Slide in from direction
type SlideInProps = {
	children: React.ReactNode;
	direction?: "left" | "right" | "up" | "down";
	delay?: number;
	duration?: number;
	className?: string;
};

export function SlideIn({
	children,
	direction = "up",
	delay = 0,
	duration = 0.3,
	className,
}: SlideInProps) {
	const directionMap = {
		left: { x: -20, y: 0 },
		right: { x: 20, y: 0 },
		up: { x: 0, y: 20 },
		down: { x: 0, y: -20 },
	};

	const { x, y } = directionMap[direction];

	return (
		<motion.div
			initial={{ opacity: 0, x, y }}
			animate={{ opacity: 1, x: 0, y: 0 }}
			transition={{ delay, duration }}
			className={className}
		>
			{children}
		</motion.div>
	);
}

// Pop in animation (for badges, notifications)
type PopInProps = {
	children: React.ReactNode;
	delay?: number;
	className?: string;
};

export function PopIn({ children, delay = 0, className }: PopInProps) {
	return (
		<motion.div
			initial={{ opacity: 0, scale: 0.8 }}
			animate={{ opacity: 1, scale: 1 }}
			exit={{ opacity: 0, scale: 0.8 }}
			transition={{
				type: "spring",
				stiffness: 500,
				damping: 30,
				delay,
			}}
			className={className}
		>
			{children}
		</motion.div>
	);
}

// Export AnimatePresence for convenience
export { AnimatePresence };
