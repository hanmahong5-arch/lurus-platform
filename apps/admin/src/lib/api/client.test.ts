import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { APIError } from "./client";

describe("APIError", () => {
  it("stores status and message", () => {
    const err = new APIError(404, "Not found");
    expect(err.status).toBe(404);
    expect(err.message).toBe("Not found");
    expect(err.name).toBe("APIError");
  });

  it("is an instance of Error", () => {
    const err = new APIError(500, "Internal");
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(APIError);
  });
});

// Test the identityAdmin / notificationAdmin helper functions
// by mocking global fetch to verify URL construction and headers
describe("identityAdmin and notificationAdmin", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    // Reset module registry so env vars are re-evaluated
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("identityAdmin builds correct URL and sends auth header", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve(JSON.stringify({ result: "ok" })),
    });

    const { identityAdmin } = await import("./client");
    const result = await identityAdmin<{ result: string }>(
      "/accounts",
      "test-token-123",
    );

    expect(result).toEqual({ result: "ok" });
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);

    const [url, opts] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(url).toContain("/admin/v1/accounts");
    expect(opts.headers.Authorization).toBe("Bearer test-token-123");
    expect(opts.headers["Content-Type"]).toBe("application/json");
  });

  it("notificationAdmin builds correct URL prefix", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve(JSON.stringify([])),
    });

    const { notificationAdmin } = await import("./client");
    await notificationAdmin("/templates", "my-token");

    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toContain("/admin/v1/templates");
  });

  it("appends query params correctly", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve(JSON.stringify({ data: [] })),
    });

    const { identityAdmin } = await import("./client");
    await identityAdmin("/accounts", "token", {
      params: { q: "test", page: 2, page_size: undefined },
    });

    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toContain("q=test");
    expect(url).toContain("page=2");
    // undefined params should be skipped
    expect(url).not.toContain("page_size");
  });

  it("throws APIError on non-ok response", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      text: () => Promise.resolve(JSON.stringify({ error: "Forbidden" })),
    });

    const mod = await import("./client");

    await expect(
      mod.identityAdmin("/accounts", "bad-token"),
    ).rejects.toThrow("Forbidden");

    try {
      await mod.identityAdmin("/accounts", "bad-token");
    } catch (e) {
      // Use name check instead of instanceof because vi.resetModules()
      // creates a separate module instance with a different class identity
      expect((e as Error).name).toBe("APIError");
      expect((e as APIError).status).toBe(403);
      expect((e as APIError).message).toBe("Forbidden");
    }
  });

  it("handles empty response body", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve(""),
    });

    const { identityAdmin } = await import("./client");
    const result = await identityAdmin("/accounts/1", "token");
    expect(result).toEqual({});
  });

  it("handles non-JSON error response body", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      text: () => Promise.resolve("Bad Gateway"),
    });

    const { identityAdmin } = await import("./client");

    try {
      await identityAdmin("/accounts", "token");
    } catch (e) {
      expect((e as APIError).message).toBe("Bad Gateway");
      expect((e as APIError).status).toBe(502);
    }
  });
});
