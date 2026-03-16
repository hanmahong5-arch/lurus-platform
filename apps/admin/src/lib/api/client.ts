const IDENTITY_URL =
  process.env.IDENTITY_SERVICE_URL ||
  "http://identity-service.lurus-identity.svc.cluster.local:18104";
const NOTIFICATION_URL =
  process.env.NOTIFICATION_SERVICE_URL ||
  "http://lurus-notification.lurus-system.svc.cluster.local:18900";

interface FetchOptions extends RequestInit {
  params?: Record<string, string | number | undefined>;
}

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "APIError";
  }
}

async function request<T>(
  baseUrl: string,
  path: string,
  token: string,
  options: FetchOptions = {},
): Promise<T> {
  const { params, ...fetchOpts } = options;

  let url = `${baseUrl}${path}`;
  if (params) {
    const searchParams = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined) searchParams.set(key, String(value));
    }
    const qs = searchParams.toString();
    if (qs) url += `?${qs}`;
  }

  const res = await fetch(url, {
    ...fetchOpts,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...fetchOpts.headers,
    },
  });

  if (!res.ok) {
    const body = await res.text();
    let message: string;
    try {
      const parsed = JSON.parse(body);
      message = parsed.error || parsed.message || body;
    } catch {
      message = body;
    }
    throw new APIError(res.status, message);
  }

  const text = await res.text();
  if (!text) return {} as T;
  return JSON.parse(text);
}

export function identityAdmin<T>(
  path: string,
  token: string,
  options?: FetchOptions,
) {
  return request<T>(IDENTITY_URL, `/admin/v1${path}`, token, options);
}

export function notificationAdmin<T>(
  path: string,
  token: string,
  options?: FetchOptions,
) {
  return request<T>(NOTIFICATION_URL, `/admin/v1${path}`, token, options);
}
