import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import MailLogo from "@/components/mail/mail-logo";

/**
 * Shared layout configurations
 *
 * you can customise layouts individually from:
 * Home Layout: app/(home)/layout.tsx
 * Docs Layout: app/docs/layout.tsx
 */
export function baseOptions(): BaseLayoutProps {
	return {
		githubUrl: "https://github.com/hanmahong5-arch/lurus-webmail",
		nav: {
			title: (
				<>
					<MailLogo size={42} />
					<span className={"text-blue-500 text-2xl"}>Lurus Mail</span>
				</>
			),
		},
		// see https://fumadocs.dev/docs/ui/navigation/links
		links: [],
	};
}
