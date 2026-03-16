import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
	StalwartAdminApi,
	createStalwartApi,
} from "../../../lib/admin/stalwart-api";

const mockFetch = vi.fn();

describe("StalwartAdminApi", () => {
	let api: StalwartAdminApi;

	beforeEach(() => {
		mockFetch.mockReset();
		vi.stubGlobal("fetch", mockFetch);
		api = new StalwartAdminApi({
			baseUrl: "http://stalwart.mail.svc.cluster.local:8080",
			adminUser: "admin",
			adminPassword: "secret",
		});
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	function mockResponse(status: number, body?: unknown, contentType = "application/json") {
		return {
			ok: status >= 200 && status < 300,
			status,
			statusText: status === 200 ? "OK" : "Error",
			headers: {
				get: (name: string) => (name === "content-type" ? contentType : null),
			},
			json: () => Promise.resolve(body),
			text: () => Promise.resolve(JSON.stringify(body)),
		};
	}

	describe("createAccount", () => {
		it("sends POST to /api/principal", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, null, ""));

			const result = await api.createAccount({
				name: "testuser",
				type: "individual",
				secrets: ["password123"],
				emails: ["testuser@lurus.cn"],
			});

			expect(result.ok).toBe(true);
			expect(mockFetch).toHaveBeenCalledWith(
				"http://stalwart.mail.svc.cluster.local:8080/api/principal",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: expect.stringContaining("Basic"),
					}),
				}),
			);
		});

		it("returns error on failure", async () => {
			mockFetch.mockResolvedValue(mockResponse(409, "Account exists", "text/plain"));

			const result = await api.createAccount({
				name: "existing",
				type: "individual",
			});

			expect(result.ok).toBe(false);
			expect(result.status).toBe(409);
		});
	});

	describe("getAccount", () => {
		it("retrieves account details", async () => {
			const account = {
				name: "testuser",
				type: "individual",
				emails: ["testuser@lurus.cn"],
			};
			mockFetch.mockResolvedValue(mockResponse(200, account));

			const result = await api.getAccount("testuser");

			expect(result.ok).toBe(true);
			expect(result.data?.name).toBe("testuser");
		});

		it("encodes special characters in name", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, { name: "user@domain" }));
			await api.getAccount("user@domain");

			expect(mockFetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/principal/user%40domain"),
				expect.any(Object),
			);
		});
	});

	describe("deleteAccount", () => {
		it("sends DELETE request", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, null, ""));
			const result = await api.deleteAccount("testuser");
			expect(result.ok).toBe(true);
			expect(mockFetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/principal/testuser"),
				expect.objectContaining({ method: "DELETE" }),
			);
		});
	});

	describe("listAccounts", () => {
		it("returns list of account names", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, ["admin", "user1", "user2"]));
			const result = await api.listAccounts();
			expect(result.ok).toBe(true);
			expect(result.data).toEqual(["admin", "user1", "user2"]);
		});
	});

	describe("provisionUser", () => {
		it("creates individual account with email and password", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, null, ""));
			const result = await api.provisionUser({
				username: "newuser",
				password: "secure123",
				email: "newuser@lurus.cn",
				displayName: "New User",
				quota: 1073741824, // 1GB
			});
			expect(result.ok).toBe(true);

			const body = JSON.parse(mockFetch.mock.calls[0][1].body);
			expect(body.name).toBe("newuser");
			expect(body.type).toBe("individual");
			expect(body.secrets).toEqual(["secure123"]);
			expect(body.emails).toEqual(["newuser@lurus.cn"]);
			expect(body.quota).toBe(1073741824);
		});
	});

	describe("addAlias", () => {
		it("appends alias to existing emails", async () => {
			// First call: getAccount
			mockFetch.mockResolvedValueOnce(
				mockResponse(200, {
					name: "user",
					type: "individual",
					emails: ["user@lurus.cn"],
				}),
			);
			// Second call: updateAccount
			mockFetch.mockResolvedValueOnce(mockResponse(200, null, ""));

			const result = await api.addAlias("user", "alias@lurus.cn");
			expect(result.ok).toBe(true);

			const updateBody = JSON.parse(mockFetch.mock.calls[1][1].body);
			expect(updateBody.emails).toContain("user@lurus.cn");
			expect(updateBody.emails).toContain("alias@lurus.cn");
		});

		it("deduplicates aliases", async () => {
			mockFetch.mockResolvedValueOnce(
				mockResponse(200, {
					name: "user",
					type: "individual",
					emails: ["user@lurus.cn", "alias@lurus.cn"],
				}),
			);
			mockFetch.mockResolvedValueOnce(mockResponse(200, null, ""));

			await api.addAlias("user", "alias@lurus.cn");
			const updateBody = JSON.parse(mockFetch.mock.calls[1][1].body);
			expect(updateBody.emails).toHaveLength(2);
		});
	});

	describe("healthCheck", () => {
		it("returns true when API is reachable", async () => {
			mockFetch.mockResolvedValue(mockResponse(200, null, ""));
			expect(await api.healthCheck()).toBe(true);
		});

		it("returns false on network error", async () => {
			mockFetch.mockRejectedValue(new Error("Connection refused"));
			expect(await api.healthCheck()).toBe(false);
		});
	});
});

describe("createStalwartApi", () => {
	it("creates API from env vars", () => {
		const api = createStalwartApi({
			STALWART_API_URL: "http://localhost:8080",
			STALWART_ADMIN_USER: "admin",
			STALWART_ADMIN_PASSWORD: "pass",
		});
		expect(api).toBeInstanceOf(StalwartAdminApi);
	});
});
