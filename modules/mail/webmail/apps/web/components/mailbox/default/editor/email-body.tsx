import React, { useMemo, useRef, useState, useEffect } from "react";

type Props = { html?: string | null };

export function EmailBody({ html }: Props) {
	const [hideQuotes, setHideQuotes] = useState(true);
	const hostRef = useRef<HTMLDivElement>(null);

	// Only show the toggle if we detect quoted blocks
	const hasQuotes = useMemo(() => {
		if (!html) return false;
		return /<blockquote\b|class="gmail_quote"|blockquote\s+type="cite"|class="moz-cite-prefix"/i.test(
			html,
		);
	}, [html]);

	// Optional: ensure links open in new tab and no scripts sneak in
	useEffect(() => {
		const el = hostRef.current;
		if (!el) return;
		for (const a of el.querySelectorAll<HTMLAnchorElement>("a[href]")) {
			a.rel = "nofollow noopener noreferrer";
			a.target = "_blank";
		}
	}, [html]);

	return (
		<div className="space-y-2">
			{hasQuotes && (
				<button
					type="button"
					onClick={() => setHideQuotes((v) => !v)}
					className="text-sm px-2 py-1 rounded border hover:bg-gray-50"
				>
					{hideQuotes ? "Show quoted text" : "Hide quoted text"}
				</button>
			)}

			<div
				ref={hostRef}
				className={hideQuotes ? "email-body hide-quotes" : "email-body"}
				// you already sanitize or trust your stored HTML; if not, sanitize first
				dangerouslySetInnerHTML={{ __html: html || "" }}
			/>

			{/* Scoped CSS to collapse quoted sections */}
			<style>{`
        .email-body.hide-quotes blockquote,
        .email-body.hide-quotes blockquote[type="cite"],
        .email-body.hide-quotes .gmail_quote,
        .email-body.hide-quotes .moz-cite-prefix + blockquote {
          display: none !important;
        }
      `}</style>
		</div>
	);
}
