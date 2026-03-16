'use client';

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useFetchClient } from '../react';
import type { ApiResponse, MailboxSummary } from '../types';

/**
 * Query key factory for mailboxes
 */
export const mailboxKeys = {
  all: ['mailboxes'] as const,
  lists: () => [...mailboxKeys.all, 'list'] as const,
  list: (identityId: string) => [...mailboxKeys.lists(), identityId] as const,
  details: () => [...mailboxKeys.all, 'detail'] as const,
  detail: (id: string) => [...mailboxKeys.details(), id] as const,
};

/**
 * Fetch mailboxes for an identity
 */
export function useMailboxes(identityId: string, options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: mailboxKeys.list(identityId),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MailboxSummary[]>>(
        `/api/mail/identities/${identityId}/mailboxes`
      );
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch mailboxes');
      }
      return response.data;
    },
    enabled: !!identityId && (options?.enabled !== false),
  });
}

/**
 * Fetch single mailbox
 */
export function useMailbox(id: string, options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: mailboxKeys.detail(id),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MailboxSummary>>(`/api/mail/mailboxes/${id}`);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch mailbox');
      }
      return response.data;
    },
    enabled: !!id && (options?.enabled !== false),
  });
}

/**
 * Create mailbox mutation
 */
export function useCreateMailbox() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ identityId, data }: { identityId: string; data: { name: string; color?: string } }) => {
      const response = await client.post<ApiResponse<MailboxSummary>>(
        `/api/mail/identities/${identityId}/mailboxes`,
        data
      );
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to create mailbox');
      }
      return response.data;
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: mailboxKeys.list(variables.identityId) });
    },
  });
}

/**
 * Update mailbox mutation
 */
export function useUpdateMailbox() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: Partial<MailboxSummary> }) => {
      const response = await client.patch<ApiResponse<MailboxSummary>>(`/api/mail/mailboxes/${id}`, data);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to update mailbox');
      }
      return response.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(mailboxKeys.detail(data.id), data);
      queryClient.invalidateQueries({ queryKey: mailboxKeys.lists() });
    },
  });
}

/**
 * Delete mailbox mutation
 */
export function useDeleteMailbox() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      const response = await client.delete<ApiResponse<null>>(`/api/mail/mailboxes/${id}`);
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to delete mailbox');
      }
      return id;
    },
    onSuccess: (id) => {
      queryClient.removeQueries({ queryKey: mailboxKeys.detail(id) });
      queryClient.invalidateQueries({ queryKey: mailboxKeys.lists() });
    },
  });
}
