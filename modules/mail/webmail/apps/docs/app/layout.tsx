import "@/app/global.css";
import { RootProvider } from "fumadocs-ui/provider/next";
import { Inter } from "next/font/google";

const inter = Inter({
	subsets: ["latin"],
});

export const metadata = {
	title: {
		default: "Lurus Mail — Instant webmail for any email provider",
		template: "%s | Lurus Mail Docs",
	},
	description:
		"Self-hosted email web client with IMAP, SMTP, SES, Mailgun, Postmark, Sendgrid provider integrations.",
	keywords: [
		"Lurus Mail",
		"email",
		"self-hosted",
		"IMAP",
		"SMTP",
		"open source",
		"postmark",
		"mailgun",
		"sendgrid",
		"ses",
		"webmail",
	],
	openGraph: {
		title: "Lurus Mail — Instant webmail",
		description: "Your own self-hosted webmail platform.",
		url: "https://mail.lurus.cn",
		siteName: "Lurus Mail",
		images: [
			{
				url: "https://mail.lurus.cn/light-mailbox.png",
				width: 1200,
				height: 630,
			},
		],
		locale: "en_US",
		type: "website",
	},
	twitter: {
		card: "summary_large_image",
		title: "Lurus Mail — Instant webmail",
		description: "Your own self-hosted webmail platform.",
		images: ["https://mail.lurus.cn/light-mailbox.png"],
	},
	metadataBase: new URL("https://mail.lurus.cn"),
};

export default function Layout({ children }: LayoutProps<"/">) {
	return (
		<html lang="en" className={inter.className} suppressHydrationWarning>
			<body className="flex flex-col min-h-screen">
				<RootProvider>{children}</RootProvider>
			</body>
		</html>
	);
}
