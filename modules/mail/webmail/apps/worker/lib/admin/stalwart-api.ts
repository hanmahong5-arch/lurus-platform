/**
 * Stalwart Mail Server Management API client.
 *
 * Communicates with Stalwart's REST management API to perform
 * account provisioning, alias management, and domain operations.
 *
 * API reference: https://stalw.art/docs/api/management/overview
 */

export interface StalwartApiConfig {
	/** Base URL, e.g. "http://stalwart.mail.svc.cluster.local:8080" */
	baseUrl: string;
	/** Admin credentials for Basic auth */
	adminUser: string;
	adminPassword: string;
}

export interface StalwartAccount {
	name: string;
	type: "individual" | "group" | "list";
	description?: string;
	secrets?: string[];
	emails?: string[];
	memberOf?: string[];
	members?: string[];
	quota?: number;
}

export interface StalwartApiResult<T = unknown> {
	ok: boolean;
	data?: T;
	error?: string;
	status?: number;
}

export class StalwartAdminApi {
	private baseUrl: string;
	private authHeader: string;

	constructor(config: StalwartApiConfig) {
		this.baseUrl = config.baseUrl.replace(/\/$/, "");
		const credentials = Buffer.from(
			`${config.adminUser}:${config.adminPassword}`,
		).toString("base64");
		this.authHeader = `Basic ${credentials}`;
	}

	private async request<T>(
		method: string,
		path: string,
		body?: unknown,
	): Promise<StalwartApiResult<T>> {
		try {
			const url = `${this.baseUrl}${path}`;
			const headers: Record<string, string> = {
				Authorization: this.authHeader,
				"Content-Type": "application/json",
			};

			const res = await fetch(url, {
				method,
				headers,
				body: body ? JSON.stringify(body) : undefined,
			});

			if (!res.ok) {
				const text = await res.text().catch(() => "");
				return {
					ok: false,
					error: `HTTP ${res.status}: ${text || res.statusText}`,
					status: res.status,
				};
			}

			// Some endpoints return empty body on success
			const contentType = res.headers.get("content-type") || "";
			if (contentType.includes("application/json")) {
				const data = (await res.json()) as T;
				return { ok: true, data, status: res.status };
			}

			return { ok: true, status: res.status };
		} catch (err: any) {
			return {
				ok: false,
				error: err?.message ?? String(err),
			};
		}
	}

	/**
	 * Create a new mail account in Stalwart.
	 */
	async createAccount(
		account: StalwartAccount,
	): Promise<StalwartApiResult<void>> {
		return this.request("POST", "/api/principal", account);
	}

	/**
	 * Get account details by name.
	 */
	async getAccount(
		name: string,
	): Promise<StalwartApiResult<StalwartAccount>> {
		return this.request<StalwartAccount>("GET", `/api/principal/${encodeURIComponent(name)}`);
	}

	/**
	 * Update an existing account.
	 */
	async updateAccount(
		name: string,
		updates: Partial<StalwartAccount>,
	): Promise<StalwartApiResult<void>> {
		return this.request("PUT", `/api/principal/${encodeURIComponent(name)}`, updates);
	}

	/**
	 * Delete an account.
	 */
	async deleteAccount(name: string): Promise<StalwartApiResult<void>> {
		return this.request("DELETE", `/api/principal/${encodeURIComponent(name)}`);
	}

	/**
	 * List all accounts.
	 */
	async listAccounts(): Promise<StalwartApiResult<string[]>> {
		return this.request<string[]>("GET", "/api/principal");
	}

	/**
	 * Change account password.
	 */
	async changePassword(
		name: string,
		newPassword: string,
	): Promise<StalwartApiResult<void>> {
		return this.updateAccount(name, {
			secrets: [newPassword],
		});
	}

	/**
	 * Add email alias to an account.
	 */
	async addAlias(
		name: string,
		alias: string,
	): Promise<StalwartApiResult<void>> {
		const existing = await this.getAccount(name);
		if (!existing.ok || !existing.data) {
			return { ok: false, error: existing.error || "Account not found" };
		}
		const emails = [...new Set([...(existing.data.emails || []), alias])];
		return this.updateAccount(name, { emails });
	}

	/**
	 * Remove email alias from an account.
	 */
	async removeAlias(
		name: string,
		alias: string,
	): Promise<StalwartApiResult<void>> {
		const existing = await this.getAccount(name);
		if (!existing.ok || !existing.data) {
			return { ok: false, error: existing.error || "Account not found" };
		}
		const emails = (existing.data.emails || []).filter((e) => e !== alias);
		return this.updateAccount(name, { emails });
	}

	/**
	 * Create a mailing list / distribution group.
	 */
	async createMailingList(
		name: string,
		email: string,
		members: string[],
	): Promise<StalwartApiResult<void>> {
		return this.createAccount({
			name,
			type: "list",
			emails: [email],
			members,
		});
	}

	/**
	 * Provision a new user mailbox.
	 * Convenience method: creates account + sets password + primary email.
	 */
	async provisionUser(opts: {
		username: string;
		password: string;
		email: string;
		displayName?: string;
		quota?: number;
	}): Promise<StalwartApiResult<void>> {
		return this.createAccount({
			name: opts.username,
			type: "individual",
			description: opts.displayName || opts.username,
			secrets: [opts.password],
			emails: [opts.email],
			quota: opts.quota,
		});
	}

	/**
	 * Health check - verify API is reachable.
	 */
	async healthCheck(): Promise<boolean> {
		const result = await this.request("GET", "/healthz");
		return result.ok;
	}
}

/**
 * Create StalwartAdminApi from environment variables.
 */
export function createStalwartApi(env?: {
	STALWART_API_URL?: string;
	STALWART_ADMIN_USER?: string;
	STALWART_ADMIN_PASSWORD?: string;
}): StalwartAdminApi {
	const e = env ?? process.env;
	return new StalwartAdminApi({
		baseUrl:
			(e as any).STALWART_API_URL ||
			"http://stalwart.mail.svc.cluster.local:8080",
		adminUser: (e as any).STALWART_ADMIN_USER || "admin",
		adminPassword: (e as any).STALWART_ADMIN_PASSWORD || "",
	});
}
