import { useCallback, useEffect, useRef, useState } from "react";

export interface DebouncedSearchOptions {
	/** Debounce delay in milliseconds (default: 300) */
	delay?: number;
	/** Minimum query length before triggering search (default: 1) */
	minLength?: number;
	/** Callback when debounced value changes */
	onSearch?: (query: string) => void;
}

/**
 * Hook for debounced search input.
 * Returns the immediate value and the debounced value.
 */
export function useDebouncedSearch({
	delay = 300,
	minLength = 1,
	onSearch,
}: DebouncedSearchOptions = {}) {
	const [query, setQuery] = useState("");
	const [debouncedQuery, setDebouncedQuery] = useState("");
	const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

	useEffect(() => {
		if (timerRef.current) {
			clearTimeout(timerRef.current);
		}

		timerRef.current = setTimeout(() => {
			const value = query.length >= minLength ? query : "";
			setDebouncedQuery(value);
			onSearch?.(value);
		}, delay);

		return () => {
			if (timerRef.current) {
				clearTimeout(timerRef.current);
			}
		};
	}, [query, delay, minLength, onSearch]);

	const clear = useCallback(() => {
		setQuery("");
		setDebouncedQuery("");
		if (timerRef.current) {
			clearTimeout(timerRef.current);
		}
		onSearch?.("");
	}, [onSearch]);

	return {
		query,
		setQuery,
		debouncedQuery,
		clear,
		isSearching: query !== debouncedQuery,
	};
}
