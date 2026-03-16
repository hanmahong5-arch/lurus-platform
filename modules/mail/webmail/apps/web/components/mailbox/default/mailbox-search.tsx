"use client";

import * as React from "react";
import {
	CommandDialog,
	CommandInput,
	CommandSeparator,
} from "@/components/ui/command";
import { Badge } from "@/components/ui/badge";
import { Search, Paperclip, Clock, X, HelpCircle } from "lucide-react";
import { initSearch } from "@/lib/actions/mailbox";
import type { User } from "@supabase/supabase-js";
import type { ThreadHit, SearchThreadsResponse } from "@schema";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { IconStar, IconStarFilled } from "@tabler/icons-react";
import {
	parseSearchQuery,
	highlightQuery,
	type HighlightedPart,
} from "@/lib/search/query-parser";
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from "@/components/ui/tooltip";

// Search history management
const SEARCH_HISTORY_KEY = "webmail-search-history";
const MAX_HISTORY_ITEMS = 10;

function getSearchHistory(): string[] {
	if (typeof window === "undefined") return [];
	try {
		const stored = localStorage.getItem(SEARCH_HISTORY_KEY);
		return stored ? JSON.parse(stored) : [];
	} catch {
		return [];
	}
}

function addToSearchHistory(query: string): void {
	if (typeof window === "undefined" || !query.trim()) return;
	try {
		const history = getSearchHistory();
		const filtered = history.filter((q) => q !== query);
		filtered.unshift(query);
		localStorage.setItem(
			SEARCH_HISTORY_KEY,
			JSON.stringify(filtered.slice(0, MAX_HISTORY_ITEMS))
		);
	} catch {
		// Ignore storage errors
	}
}

function removeFromHistory(query: string): void {
	if (typeof window === "undefined") return;
	try {
		const history = getSearchHistory().filter((q) => q !== query);
		localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(history));
	} catch {
		// Ignore storage errors
	}
}

// Search syntax help
const SEARCH_SYNTAX_HELP = [
	{ syntax: "from:", example: "from:user@example.com", desc: "Filter by sender" },
	{ syntax: "to:", example: "to:recipient@example.com", desc: "Filter by recipient" },
	{ syntax: "subject:", example: "subject:meeting", desc: "Search in subject" },
	{ syntax: "has:attachment", example: "has:attachment", desc: "Has attachments" },
	{ syntax: "is:unread", example: "is:unread", desc: "Unread messages" },
	{ syntax: "is:starred", example: "is:starred", desc: "Starred messages" },
	{ syntax: "after:", example: "after:2024-01-01", desc: "After date" },
	{ syntax: "before:", example: "before:2024-12-31", desc: "Before date" },
	{ syntax: "larger:", example: "larger:10MB", desc: "Larger than size" },
	{ syntax: "\"phrase\"", example: "\"exact match\"", desc: "Exact phrase" },
];

export default function MailboxSearch({
	user,
	publicId,
	mailboxSlug,
}: {
	user: User | null;
	publicId: string;
	mailboxSlug: string;
}) {
	const [open, setOpen] = React.useState(false);
	const [query, setQuery] = React.useState("");
	const [hasAttachment, setHasAttachment] = React.useState(false);
	const [onlyUnread, setOnlyUnread] = React.useState(false);
	const [isStarred, setIsStarred] = React.useState(false);

	const [loading, setLoading] = React.useState(false);
	const [items, setItems] = React.useState<ThreadHit[]>([]);
	const [totalThreads, setTotalThreads] = React.useState(0);
	const [totalMessages, setTotalMessages] = React.useState(0);

	// Search history state
	const [history, setHistory] = React.useState<string[]>([]);
	const [showSyntaxHelp, setShowSyntaxHelp] = React.useState(false);

	// Load search history on mount
	React.useEffect(() => {
		setHistory(getSearchHistory());
	}, []);

	// Parse query for advanced syntax
	const parsedQuery = React.useMemo(() => parseSearchQuery(query), [query]);
	const highlightedParts = React.useMemo(() => highlightQuery(query), [query]);

	// Apply parsed filters to state
	React.useEffect(() => {
		const hasAttachFilter = parsedQuery.filters.find(
			(f) => f.type === "has" && f.value === "attachment"
		);
		const isUnreadFilter = parsedQuery.filters.find(
			(f) => f.type === "is" && f.value === "unread"
		);
		const isStarredFilter = parsedQuery.filters.find(
			(f) => f.type === "is" && (f.value === "starred" || f.value === "flagged")
		);

		if (hasAttachFilter) setHasAttachment(!hasAttachFilter.negated);
		if (isUnreadFilter) setOnlyUnread(!isUnreadFilter.negated);
		if (isStarredFilter) setIsStarred(!isStarredFilter.negated);
	}, [parsedQuery]);

	React.useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
				e.preventDefault();
				setOpen((v) => !v);
			}
		};

		// Listen for keyboard shortcut event from shortcuts provider
		const onSearchShortcut = () => setOpen(true);

		window.addEventListener("keydown", onKey);
		window.addEventListener("keyboard-shortcut-search", onSearchShortcut);

		return () => {
			window.removeEventListener("keydown", onKey);
			window.removeEventListener("keyboard-shortcut-search", onSearchShortcut);
		};
	}, []);

	const doSearch = React.useMemo(() => {
		let t: ReturnType<typeof setTimeout> | null = null;
		return (q: string, attach: boolean, unread: boolean, starred: boolean) => {
			if (t) clearTimeout(t);
			t = setTimeout(async () => {
				if (!q.trim()) {
					setItems([]);
					setTotalThreads(0);
					setTotalMessages(0);
					return;
				}
				try {
					setLoading(true);
					const res = (await initSearch(
						q,
						String(user?.id),
						attach,
						unread,
						starred,
						1,
					)) as SearchThreadsResponse;

					setItems(res.items || []);
					setTotalThreads(res.totalThreads ?? res.items?.length ?? 0);
					setTotalMessages(res.totalMessages ?? res.items?.length ?? 0);
				} catch (e) {
					setItems([]);
					setTotalThreads(0);
					setTotalMessages(0);
				} finally {
					setLoading(false);
				}
			}, 250);
		};
	}, [user?.id]);

	React.useEffect(() => {
		doSearch(query, hasAttachment, onlyUnread, isStarred);
	}, [query, hasAttachment, onlyUnread, isStarred, doSearch]);

	const toggle = (setter: React.Dispatch<React.SetStateAction<boolean>>) =>
		setter((v) => !v);

	const router = useRouter();

	const pathName = usePathname();

	return (
		<>
			<button
				type="button"
				onClick={() => setOpen(true)}
				className="flex w-full items-center gap-2 rounded-lg border bg-background px-4 py-2.5 text-muted-foreground hover:bg-muted/30"
			>
				<Search className="h-4 w-4 opacity-60" />
				<span className="text-sm">Search all mailboxes (⌘K)</span>
			</button>

			<CommandDialog open={open} onOpenChange={setOpen}>
				<div className="flex items-center gap-2 px-3">
					<CommandInput
						autoFocus
						placeholder="Search mail… (try from: to: is:unread has:attachment)"
						value={query}
						onValueChange={setQuery}
						onKeyDown={(e) => {
							if (e.key === "Enter") {
								e.preventDefault();
								addToSearchHistory(query);
								setHistory(getSearchHistory());
								setOpen(false);
								router.push(
									`${pathName.match("/dashboard/mail") ? "/dashboard" : ""}/mail/${publicId}/${mailboxSlug}/search?q=${encodeURIComponent(query)}&has=${hasAttachment ? "1" : "0"}&unread=${onlyUnread ? "1" : "0"}&starred=${isStarred ? "1" : "0"}`,
								);
							}
						}}
					/>
					<TooltipProvider>
						<Tooltip>
							<TooltipTrigger asChild>
								<button
									type="button"
									onClick={() => setShowSyntaxHelp((v) => !v)}
									className="p-1 hover:bg-muted rounded"
								>
									<HelpCircle className="h-4 w-4 text-muted-foreground" />
								</button>
							</TooltipTrigger>
							<TooltipContent side="left">
								<p>Search syntax help</p>
							</TooltipContent>
						</Tooltip>
					</TooltipProvider>
				</div>

				<div className="sticky top-0 z-10 flex flex-wrap gap-2 border-b bg-background px-4 py-2">
					<Badge
						onClick={() => toggle(setHasAttachment)}
						className={`cursor-pointer rounded-full px-3 py-1 text-sm ${
							hasAttachment ? "bg-primary text-primary-foreground" : ""
						}`}
						variant={hasAttachment ? "default" : "secondary"}
					>
						Has attachment
					</Badge>

					<Badge
						onClick={() => toggle(setOnlyUnread)}
						className={`cursor-pointer rounded-full px-3 py-1 text-sm ${
							onlyUnread ? "bg-primary text-primary-foreground" : ""
						}`}
						variant={onlyUnread ? "default" : "secondary"}
					>
						Unread only
					</Badge>

					<Badge
						onClick={() => toggle(setIsStarred)}
						className={`cursor-pointer rounded-full px-3 py-1 text-sm ${
							isStarred ? "bg-primary text-primary-foreground" : ""
						}`}
						variant={isStarred ? "default" : "secondary"}
					>
						Starred only
					</Badge>
				</div>

				{/* Syntax Help Panel */}
				{showSyntaxHelp && (
					<div className="border-b bg-muted/30 px-4 py-3">
						<div className="text-xs font-medium mb-2 text-foreground">Search Operators</div>
						<div className="grid grid-cols-2 gap-x-4 gap-y-1">
							{SEARCH_SYNTAX_HELP.map((item) => (
								<div key={item.syntax} className="flex items-center gap-2 text-xs">
									<code className="bg-muted px-1.5 py-0.5 rounded text-primary font-mono">
										{item.syntax}
									</code>
									<span className="text-muted-foreground">{item.desc}</span>
								</div>
							))}
						</div>
					</div>
				)}

				{/* Search History */}
				{!query && history.length > 0 && (
					<div className="border-b px-4 py-2">
						<div className="text-xs font-medium mb-2 text-muted-foreground flex items-center gap-1">
							<Clock className="h-3 w-3" />
							Recent Searches
						</div>
						<div className="flex flex-wrap gap-2">
							{history.map((h) => (
								<Badge
									key={h}
									variant="secondary"
									className="cursor-pointer group flex items-center gap-1 pr-1"
									onClick={() => setQuery(h)}
								>
									<span className="truncate max-w-[150px]">{h}</span>
									<button
										type="button"
										onClick={(e) => {
											e.stopPropagation();
											removeFromHistory(h);
											setHistory(getSearchHistory());
										}}
										className="p-0.5 hover:bg-muted-foreground/20 rounded opacity-0 group-hover:opacity-100 transition-opacity"
									>
										<X className="h-3 w-3" />
									</button>
								</Badge>
							))}
						</div>
					</div>
				)}

				<div className="px-4 py-2 text-xs text-muted-foreground">
					{loading
						? "Searching…"
						: query
							? `Threads: ${totalThreads} · Messages: ${totalMessages}`
							: "Type to search or use operators like from:, to:, is:unread"}
				</div>

				<div className="max-h-[60vh] overflow-auto px-2 pb-2">
					{items.length === 0 && !loading ? (
						<div className="px-4 py-8 text-center text-sm text-muted-foreground">
							No results found.
						</div>
					) : (
						<ul className="space-y-2">
							{items.map((t) => (
								<li key={t.id} className={"my-2"}>
									<Link
										href={`${pathName.match("/dashboard/mail") ? "/dashboard/mail" : "/mail"}/${publicId}/inbox/threads/${t.threadId}`}
										type="button"
										className="w-full rounded-md px-4 py-3 text-left hover:bg-muted/60 block focus:outline-none focus:ring-2 focus:ring-ring"
									>
										<div className="flex items-center gap-2">
											<span className={"flex mt-0.5"}>
												{t.starred ? (
													<IconStarFilled
														className={"text-yellow-400"}
														size={12}
													/>
												) : (
													<IconStar className="h-3 w-3" />
												)}
											</span>
											<span
												aria-hidden
												className={`mt-1 h-2 w-2 shrink-0 rounded-full ${
													t.unread ? "bg-primary" : "bg-muted-foreground/30"
												}`}
											/>
											<div className="truncate text-[15px] font-medium">
												{t.subject || "(no subject)"}
											</div>
											{t.hasAttachment && (
												<span className="ml-1 text-muted-foreground">
													<Paperclip className="h-3.5 w-3.5" />
												</span>
											)}
										</div>

										{t.snippet && (
											<div className="mt-1 line-clamp-2 text-xs text-muted-foreground">
												{t.snippet}
											</div>
										)}

										<div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
											{t.fromName && (
												<span className="truncate">{t.fromName}</span>
											)}
											{t.fromEmail && !t.fromName && (
												<span className="truncate">{t.fromEmail}</span>
											)}
											{t.participants?.length > 0 && (
												<>
													<span>•</span>
													<span className="truncate">
														{t.participants.slice(0, 2).join(", ")}
														{t.participants.length > 2 ? " …" : ""}
													</span>
												</>
											)}
											{t.createdAt ? (
												<div className={"flex min-w-16"}>
													<span>•&nbsp;</span>
													<span>
														{new Date(t.createdAt).toLocaleDateString(
															undefined,
															{
																month: "short",
																day: "numeric",
															},
														)}
													</span>
												</div>
											) : null}
										</div>
									</Link>
								</li>
							))}
						</ul>
					)}
				</div>

				<CommandSeparator />

				{/* FOOTER */}
				<div className="flex items-center justify-between rounded-b-lg bg-background/95 px-4 py-3 backdrop-blur">
					<div className="flex items-center gap-2 text-sm text-muted-foreground">
						<Search className="h-4 w-4 opacity-70" />
						<span>
							All search results for{" "}
							<span className="font-medium text-foreground">
								{`‘${query || ""}’`}
							</span>
						</span>
					</div>
					<div className="text-xs text-muted-foreground">Press ENTER</div>
				</div>
			</CommandDialog>
		</>
	);
}
