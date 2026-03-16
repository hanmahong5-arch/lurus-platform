'use client';

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useFetchClient } from '../react';
import type { ApiResponse, MailIdentity } from '../types';

/**
 * Query key factory for identities
 */
export const identityKeys = {
  all: ['identities'] as const,
  lists: () => [...identityKeys.all, 'list'] as const,
  list: (filters?: { ownerId?: string }) => [...identityKeys.lists(), filters] as const,
  details: () => [...identityKeys.all, 'detail'] as const,
  detail: (id: string) => [...identityKeys.details(), id] as const,
};

/**
 * Fetch all identities
 */
export function useIdentities(options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: identityKeys.list(),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MailIdentity[]>>('/api/mail/identities');
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch identities');
      }
      return response.data;
    },
    ...options,
  });
}

/**
 * Fetch single identity by ID
 */
export function useIdentity(id: string, options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: identityKeys.detail(id),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MailIdentity>>(`/api/mail/identities/${id}`);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch identity');
      }
      return response.data;
    },
    enabled: !!id && (options?.enabled !== false),
  });
}

/**
 * Update identity mutation
 */
export function useUpdateIdentity() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: Partial<MailIdentity> }) => {
      const response = await client.patch<ApiResponse<MailIdentity>>(`/api/mail/identities/${id}`, data);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to update identity');
      }
      return response.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(identityKeys.detail(data.id), data);
      queryClient.invalidateQueries({ queryKey: identityKeys.lists() });
    },
  });
}

/**
 * Delete identity mutation
 */
export function useDeleteIdentity() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      const response = await client.delete<ApiResponse<null>>(`/api/mail/identities/${id}`);
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to delete identity');
      }
      return id;
    },
    onSuccess: (id) => {
      queryClient.removeQueries({ queryKey: identityKeys.detail(id) });
      queryClient.invalidateQueries({ queryKey: identityKeys.lists() });
    },
  });
}
