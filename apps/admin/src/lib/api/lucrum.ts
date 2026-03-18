// --- Types ---

export type StrategyStatus = "active" | "pending" | "suspended" | "rejected";

export type PriceType = "free" | "per_run" | "monthly";

export interface MarketplaceStrategy {
  id: number;
  strategy_history_id: number;
  author_user_id: string | null;
  title: string;
  description: string | null;
  price_type: PriceType;
  price_per_run: number;
  price_monthly: number;
  author_identity_account_id: string | null;
  grade_score: string | null;
  total_runs: number;
  total_subscribers: number;
  staked_lb: number;
  status: StrategyStatus;
  published_at: string;
}

export interface StrategiesResponse {
  data: MarketplaceStrategy[];
  total: number;
  page: number;
  page_size: number;
}

export interface StrategyActionResponse {
  ok: boolean;
  message?: string;
}

// --- Client-side API helpers (call through Next.js proxy) ---

const PROXY_BASE = "/api/proxy/lucrum";

async function proxyRequest<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${PROXY_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    ...options,
  });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `Request failed: ${res.status}`);
  }

  const text = await res.text();
  if (!text) return {} as T;
  return JSON.parse(text);
}

export async function fetchStrategies(params: {
  page?: number;
  page_size?: number;
  status?: string;
  q?: string;
}): Promise<StrategiesResponse> {
  const searchParams = new URLSearchParams();
  if (params.page) searchParams.set("page", String(params.page));
  if (params.page_size) searchParams.set("page_size", String(params.page_size));
  if (params.status) searchParams.set("status", params.status);
  if (params.q) searchParams.set("q", params.q);

  const qs = searchParams.toString();
  const path = `/api/lurus/marketplace/strategies${qs ? `?${qs}` : ""}`;
  return proxyRequest<StrategiesResponse>(path);
}

export async function updateStrategyStatus(
  id: number,
  status: StrategyStatus,
): Promise<StrategyActionResponse> {
  return proxyRequest<StrategyActionResponse>(
    `/api/lurus/marketplace/strategies/${id}/status`,
    {
      method: "PATCH",
      body: JSON.stringify({ status }),
    },
  );
}
