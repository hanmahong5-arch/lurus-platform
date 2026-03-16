/**
 * Advanced Search Query Parser
 *
 * Supports Gmail-like search syntax:
 * - from:user@example.com
 * - to:recipient@example.com
 * - subject:meeting
 * - has:attachment
 * - is:unread
 * - is:starred
 * - after:2024-01-01
 * - before:2024-12-31
 * - label:work
 * - in:inbox
 * - larger:10MB
 * - smaller:1MB
 * - "exact phrase"
 */

export type SearchFilter = {
	type:
		| "from"
		| "to"
		| "subject"
		| "has"
		| "is"
		| "after"
		| "before"
		| "label"
		| "in"
		| "larger"
		| "smaller"
		| "text";
	value: string;
	negated?: boolean;
};

export type ParsedQuery = {
	filters: SearchFilter[];
	freeText: string;
};

// Size unit conversion
const SIZE_UNITS: Record<string, number> = {
	b: 1,
	kb: 1024,
	k: 1024,
	mb: 1024 * 1024,
	m: 1024 * 1024,
	gb: 1024 * 1024 * 1024,
	g: 1024 * 1024 * 1024,
};

function parseSize(value: string): number | null {
	const match = value.match(/^(\d+(?:\.\d+)?)\s*(b|kb?|mb?|gb?)?$/i);
	if (!match) return null;

	const num = parseFloat(match[1]);
	const unit = (match[2] || "b").toLowerCase();
	const multiplier = SIZE_UNITS[unit] || 1;

	return Math.floor(num * multiplier);
}

function parseDate(value: string): Date | null {
	// Support formats: YYYY-MM-DD, YYYY/MM/DD, MM/DD/YYYY
	const patterns = [
		/^(\d{4})-(\d{2})-(\d{2})$/, // YYYY-MM-DD
		/^(\d{4})\/(\d{2})\/(\d{2})$/, // YYYY/MM/DD
		/^(\d{2})\/(\d{2})\/(\d{4})$/, // MM/DD/YYYY
	];

	for (const pattern of patterns) {
		const match = value.match(pattern);
		if (match) {
			let year: number, month: number, day: number;

			if (pattern === patterns[2]) {
				// MM/DD/YYYY
				month = parseInt(match[1], 10);
				day = parseInt(match[2], 10);
				year = parseInt(match[3], 10);
			} else {
				// YYYY-MM-DD or YYYY/MM/DD
				year = parseInt(match[1], 10);
				month = parseInt(match[2], 10);
				day = parseInt(match[3], 10);
			}

			const date = new Date(year, month - 1, day);
			if (!isNaN(date.getTime())) {
				return date;
			}
		}
	}

	// Try relative dates
	const now = new Date();
	const relative: Record<string, number> = {
		today: 0,
		yesterday: 1,
		"1d": 1,
		"7d": 7,
		"1w": 7,
		"30d": 30,
		"1m": 30,
		"90d": 90,
		"3m": 90,
		"1y": 365,
	};

	const lower = value.toLowerCase();
	if (lower in relative) {
		const daysAgo = relative[lower];
		now.setDate(now.getDate() - daysAgo);
		return now;
	}

	return null;
}

export function parseSearchQuery(query: string): ParsedQuery {
	const filters: SearchFilter[] = [];
	let freeText = "";

	// Regex to match operators: operator:value or -operator:value (negated)
	const operatorRegex = /(-?)(\w+):(?:"([^"]+)"|(\S+))/g;

	// Regex to match quoted phrases
	const phraseRegex = /"([^"]+)"/g;

	// Extract all phrases first
	const phrases: string[] = [];
	let phraseMatch;
	while ((phraseMatch = phraseRegex.exec(query)) !== null) {
		phrases.push(phraseMatch[1]);
	}

	// Remove phrases from query for processing operators
	let processedQuery = query.replace(phraseRegex, "");

	// Extract operators
	let match;
	while ((match = operatorRegex.exec(processedQuery)) !== null) {
		const negated = match[1] === "-";
		const operator = match[2].toLowerCase();
		const value = match[3] || match[4];

		switch (operator) {
			case "from":
			case "to":
			case "subject":
			case "label":
			case "in":
				filters.push({ type: operator, value, negated });
				break;

			case "has":
				if (value === "attachment" || value === "attachments") {
					filters.push({ type: "has", value: "attachment", negated });
				}
				break;

			case "is":
				if (["unread", "read", "starred", "flagged"].includes(value)) {
					filters.push({ type: "is", value, negated });
				}
				break;

			case "after":
			case "before":
				const date = parseDate(value);
				if (date) {
					filters.push({
						type: operator,
						value: date.toISOString().split("T")[0],
						negated,
					});
				}
				break;

			case "larger":
			case "smaller":
				const size = parseSize(value);
				if (size !== null) {
					filters.push({ type: operator, value: size.toString(), negated });
				}
				break;

			default:
				// Unknown operator, treat as text
				freeText += ` ${operator}:${value}`;
		}
	}

	// Remove matched operators from query
	processedQuery = processedQuery.replace(operatorRegex, "").trim();

	// Combine remaining text and phrases
	const textParts = [processedQuery, ...phrases].filter(Boolean);
	freeText = textParts.join(" ").trim();

	return { filters, freeText };
}

// Convert parsed query back to a display string
export function formatSearchQuery(parsed: ParsedQuery): string {
	const parts: string[] = [];

	for (const filter of parsed.filters) {
		const prefix = filter.negated ? "-" : "";
		const value = filter.value.includes(" ") ? `"${filter.value}"` : filter.value;
		parts.push(`${prefix}${filter.type}:${value}`);
	}

	if (parsed.freeText) {
		parts.push(parsed.freeText);
	}

	return parts.join(" ");
}

// Convert parsed query to Typesense filter format
export function toTypesenseFilter(parsed: ParsedQuery): {
	q: string;
	filterBy?: string;
} {
	const conditions: string[] = [];

	for (const filter of parsed.filters) {
		const op = filter.negated ? "!=" : "=";

		switch (filter.type) {
			case "from":
				conditions.push(`fromEmail:${filter.value}`);
				break;

			case "to":
				// This would need to be handled differently based on schema
				conditions.push(`toEmail:${filter.value}`);
				break;

			case "is":
				if (filter.value === "unread") {
					conditions.push(`isRead:${filter.negated ? "true" : "false"}`);
				} else if (filter.value === "read") {
					conditions.push(`isRead:${filter.negated ? "false" : "true"}`);
				} else if (filter.value === "starred" || filter.value === "flagged") {
					conditions.push(`isStarred:${filter.negated ? "false" : "true"}`);
				}
				break;

			case "has":
				if (filter.value === "attachment") {
					conditions.push(`hasAttachment:${filter.negated ? "false" : "true"}`);
				}
				break;

			case "after":
				conditions.push(`createdAt:>${new Date(filter.value).getTime() / 1000}`);
				break;

			case "before":
				conditions.push(`createdAt:<${new Date(filter.value).getTime() / 1000}`);
				break;

			case "larger":
				conditions.push(`sizeBytes:>${filter.value}`);
				break;

			case "smaller":
				conditions.push(`sizeBytes:<${filter.value}`);
				break;
		}
	}

	return {
		q: parsed.freeText || "*",
		filterBy: conditions.length > 0 ? conditions.join(" && ") : undefined,
	};
}

// Highlight operators in the query for display
export type HighlightedPart = {
	type: "operator" | "value" | "text";
	text: string;
};

export function highlightQuery(query: string): HighlightedPart[] {
	const parts: HighlightedPart[] = [];
	let remaining = query;

	const operatorRegex = /(-?)(\w+):(?:"([^"]+)"|(\S+))/g;
	let lastIndex = 0;
	let match;

	while ((match = operatorRegex.exec(query)) !== null) {
		// Add text before this match
		if (match.index > lastIndex) {
			const text = query.slice(lastIndex, match.index);
			if (text.trim()) {
				parts.push({ type: "text", text: text.trim() });
			}
		}

		// Add operator
		parts.push({
			type: "operator",
			text: `${match[1]}${match[2]}:`,
		});

		// Add value
		parts.push({
			type: "value",
			text: match[3] || match[4],
		});

		lastIndex = match.index + match[0].length;
	}

	// Add remaining text
	if (lastIndex < query.length) {
		const text = query.slice(lastIndex).trim();
		if (text) {
			parts.push({ type: "text", text });
		}
	}

	return parts;
}
