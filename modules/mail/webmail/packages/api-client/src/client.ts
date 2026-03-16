import { createTRPCClient, httpBatchLink, TRPCClientError } from '@trpc/client';
import superjson from 'superjson';
import type { ApiClientConfig, ApiError } from './types';

/**
 * tRPC Router type (to be defined by server)
 * This is a placeholder - actual type comes from server
 */
export type AppRouter = {
  // Placeholder for router type inference
  _def: {
    procedures: Record<string, unknown>;
  };
};

/**
 * Create tRPC client with configuration
 */
export function createApiClient(config: ApiClientConfig) {
  const { baseUrl, headers = {}, timeout = 30000, onError, onUnauthorized } = config;

  const client = createTRPCClient<AppRouter>({
    links: [
      httpBatchLink({
        url: `${baseUrl}/api/trpc`,
        headers: () => ({
          ...headers,
        }),
        transformer: superjson,
        fetch: async (url, options) => {
          const controller = new AbortController();
          const timeoutId = setTimeout(() => controller.abort(), timeout);

          try {
            const response = await fetch(url, {
              ...options,
              signal: controller.signal,
            });

            if (response.status === 401) {
              onUnauthorized?.();
            }

            return response;
          } finally {
            clearTimeout(timeoutId);
          }
        },
      }),
    ],
  });

  return client;
}

/**
 * Create vanilla fetch-based API client (non-tRPC)
 */
export function createFetchClient(config: ApiClientConfig) {
  const { baseUrl, headers = {}, timeout = 30000, onError, onUnauthorized } = config;

  async function request<T>(
    method: string,
    path: string,
    body?: unknown,
    options?: { signal?: AbortSignal; headers?: Record<string, string> }
  ): Promise<T> {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);

    try {
      const response = await fetch(`${baseUrl}${path}`, {
        method,
        headers: {
          'Content-Type': 'application/json',
          ...headers,
          ...options?.headers,
        },
        body: body ? JSON.stringify(body) : undefined,
        signal: options?.signal || controller.signal,
      });

      if (response.status === 401) {
        onUnauthorized?.();
      }

      const data = await response.json();

      if (!response.ok) {
        const error = new Error(data.message || 'Request failed') as ApiError;
        (error as any).code = data.code || 'UNKNOWN_ERROR';
        (error as any).status = response.status;
        (error as any).details = data.details;
        throw error;
      }

      return data;
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        const error = new Error('Request timeout') as ApiError;
        (error as any).code = 'TIMEOUT';
        (error as any).status = 0;
        throw error;
      }
      onError?.(err as Error);
      throw err;
    } finally {
      clearTimeout(timeoutId);
    }
  }

  return {
    get: <T>(path: string, options?: { signal?: AbortSignal; headers?: Record<string, string> }) =>
      request<T>('GET', path, undefined, options),

    post: <T>(path: string, body: unknown, options?: { signal?: AbortSignal; headers?: Record<string, string> }) =>
      request<T>('POST', path, body, options),

    put: <T>(path: string, body: unknown, options?: { signal?: AbortSignal; headers?: Record<string, string> }) =>
      request<T>('PUT', path, body, options),

    patch: <T>(path: string, body: unknown, options?: { signal?: AbortSignal; headers?: Record<string, string> }) =>
      request<T>('PATCH', path, body, options),

    delete: <T>(path: string, options?: { signal?: AbortSignal; headers?: Record<string, string> }) =>
      request<T>('DELETE', path, undefined, options),
  };
}

/**
 * Check if error is a tRPC client error
 */
export function isTRPCClientError(error: unknown): error is TRPCClientError<AppRouter> {
  return error instanceof TRPCClientError;
}

/**
 * Extract error message from tRPC error
 */
export function getTRPCErrorMessage(error: unknown): string {
  if (isTRPCClientError(error)) {
    return error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return 'An unknown error occurred';
}
