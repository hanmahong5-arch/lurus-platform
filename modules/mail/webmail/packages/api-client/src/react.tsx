'use client';

import React, { createContext, useContext, useMemo } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createFetchClient, createApiClient } from './client';
import type { ApiClientConfig } from './types';

/**
 * API context value
 */
type ApiContextValue = {
  client: ReturnType<typeof createFetchClient>;
  trpc: ReturnType<typeof createApiClient>;
  config: ApiClientConfig;
};

const ApiContext = createContext<ApiContextValue | null>(null);

/**
 * Default query client configuration
 */
function createDefaultQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 60 * 5, // 5 minutes
        gcTime: 1000 * 60 * 30, // 30 minutes (formerly cacheTime)
        retry: (failureCount, error) => {
          // Don't retry on 4xx errors
          if ((error as any)?.status >= 400 && (error as any)?.status < 500) {
            return false;
          }
          return failureCount < 3;
        },
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

/**
 * API Provider props
 */
type ApiProviderProps = {
  config: ApiClientConfig;
  queryClient?: QueryClient;
  children: React.ReactNode;
};

/**
 * API Provider component
 *
 * Wraps the application with QueryClient and API client context.
 */
export function ApiProvider({ config, queryClient, children }: ApiProviderProps) {
  const defaultQueryClient = useMemo(() => createDefaultQueryClient(), []);
  const client = useMemo(() => createFetchClient(config), [config]);
  const trpc = useMemo(() => createApiClient(config), [config]);

  const value = useMemo(
    () => ({
      client,
      trpc,
      config,
    }),
    [client, trpc, config]
  );

  return (
    <QueryClientProvider client={queryClient || defaultQueryClient}>
      <ApiContext.Provider value={value}>{children}</ApiContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * Hook to access API client
 */
export function useApiClient() {
  const context = useContext(ApiContext);
  if (!context) {
    throw new Error('useApiClient must be used within an ApiProvider');
  }
  return context;
}

/**
 * Hook to access fetch client directly
 */
export function useFetchClient() {
  const { client } = useApiClient();
  return client;
}

/**
 * Hook to access tRPC client directly
 */
export function useTrpcClient() {
  const { trpc } = useApiClient();
  return trpc;
}

/**
 * Hook to access API config
 */
export function useApiConfig() {
  const { config } = useApiClient();
  return config;
}
