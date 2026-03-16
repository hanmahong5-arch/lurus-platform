/**
 * SMTP dual-channel router for China domestic vs international delivery.
 *
 * China domains route through SendCloud relay for deliverability;
 * all other domains route through Stalwart direct delivery.
 */

export type SmtpChannel = "sendcloud" | "stalwart";

export interface RouteResult {
	channel: SmtpChannel;
	host: string;
	port: number;
	secure: boolean;
	/** Whether authentication is required for this channel */
	requiresAuth: boolean;
}

// China domestic email provider domains
const CHINA_DOMAINS: ReadonlySet<string> = new Set([
	// Tencent
	"qq.com",
	"foxmail.com",
	"vip.qq.com",
	// Netease
	"163.com",
	"126.com",
	"yeah.net",
	"188.com",
	"vip.163.com",
	"vip.126.com",
	// Sina / Sohu
	"sina.com",
	"sina.cn",
	"sohu.com",
	// Alibaba
	"aliyun.com",
	"alimail.com",
	"mxhichina.com",
	// Tencent Enterprise
	"exmail.qq.com",
	// China Mobile / Unicom / Telecom
	"139.com",
	"wo.cn",
	"189.cn",
	// Tom
	"tom.com",
	// 21CN
	"21cn.com",
]);

export interface StalwartConfig {
	host: string;
	port: number;
	secure: boolean;
}

export interface SendCloudConfig {
	host: string;
	port: number;
	secure: boolean;
	apiUser: string;
	apiKey: string;
}

export interface RouterConfig {
	stalwart: StalwartConfig;
	sendcloud: SendCloudConfig;
}

const DEFAULT_STALWART: StalwartConfig = {
	host: "stalwart.mail.svc.cluster.local",
	port: 587,
	secure: false,
};

const DEFAULT_SENDCLOUD: SendCloudConfig = {
	host: "smtp.sendcloud.net",
	port: 587,
	secure: false,
	apiUser: "",
	apiKey: "",
};

/**
 * Extract domain from email address.
 */
export function extractDomain(email: string): string {
	const atIndex = email.lastIndexOf("@");
	if (atIndex === -1) return "";
	return email.slice(atIndex + 1).toLowerCase().trim();
}

/**
 * Check if a domain is a known China domestic email provider.
 */
export function isChinaDomain(domain: string): boolean {
	return CHINA_DOMAINS.has(domain.toLowerCase());
}

/**
 * Route an outbound email to the appropriate SMTP channel.
 *
 * Strategy:
 *   - If ANY recipient is on a China domestic domain, route ALL via SendCloud
 *     (mixed routing per-recipient is complex; SendCloud handles international too)
 *   - Otherwise route via Stalwart direct delivery
 */
export function routeEmail(
	recipients: string[],
	config?: Partial<RouterConfig>,
): RouteResult {
	const stalwartCfg = { ...DEFAULT_STALWART, ...config?.stalwart };
	const sendcloudCfg = { ...DEFAULT_SENDCLOUD, ...config?.sendcloud };

	const hasChinaRecipient = recipients.some((r) =>
		isChinaDomain(extractDomain(r)),
	);

	if (hasChinaRecipient) {
		return {
			channel: "sendcloud",
			host: sendcloudCfg.host,
			port: sendcloudCfg.port,
			secure: sendcloudCfg.secure,
			requiresAuth: true,
		};
	}

	return {
		channel: "stalwart",
		host: stalwartCfg.host,
		port: stalwartCfg.port,
		secure: stalwartCfg.secure,
		requiresAuth: true,
	};
}

/**
 * Classify recipients into China vs international groups.
 * Useful for logging/metrics; actual routing uses routeEmail().
 */
export function classifyRecipients(recipients: string[]): {
	china: string[];
	international: string[];
} {
	const china: string[] = [];
	const international: string[] = [];

	for (const r of recipients) {
		if (isChinaDomain(extractDomain(r))) {
			china.push(r);
		} else {
			international.push(r);
		}
	}

	return { china, international };
}
