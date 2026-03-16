'use client';

import { useQuery, useMutation, useQueryClient, useInfiniteQuery } from '@tanstack/react-query';
import { useFetchClient } from '../react';
import type { ApiResponse, PaginatedResponse, ThreadSummary } from '../types';

/**
 * Query key factory for threads
 */
export const threadKeys = {
  all: ['threads'] as const,
  lists: () => [...threadKeys.all, 'list'] as const,
  list: (mailboxId: string, filters?: Record<string, unknown>) => [...threadKeys.lists(), mailboxId, filters] as const,
  infiniteLists: () => [...threadKeys.all, 'infinite'] as const,
  infiniteList: (mailboxId: string) => [...threadKeys.infiniteLists(), mailboxId] as const,
  details: () => [...threadKeys.all, 'detail'] as const,
  detail: (id: string) => [...threadKeys.details(), id] as const,
};

/**
 * Fetch threads for a mailbox (paginated)
 */
export function useThreads(
  mailboxId: string,
  options?: {
    limit?: number;
    cursor?: string;
    enabled?: boolean;
  }
) {
  const client = useFetchClient();
  const { limit = 20, cursor, enabled = true } = options || {};

  return useQuery({
    queryKey: threadKeys.list(mailboxId, { limit, cursor }),
    queryFn: async () => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (cursor) params.set('cursor', cursor);

      const response = await client.get<ApiResponse<PaginatedResponse<ThreadSummary>>>(
        `/api/mail/mailboxes/${mailboxId}/threads?${params}`
      );
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch threads');
      }
      return response.data;
    },
    enabled: !!mailboxId && enabled,
  });
}

/**
 * Fetch threads with infinite scrolling
 */
export function useInfiniteThreads(mailboxId: string, options?: { limit?: number; enabled?: boolean }) {
  const client = useFetchClient();
  const { limit = 20, enabled = true } = options || {};

  return useInfiniteQuery({
    queryKey: threadKeys.infiniteList(mailboxId),
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (pageParam) params.set('cursor', pageParam);

      const response = await client.get<ApiResponse<PaginatedResponse<ThreadSummary>>>(
        `/api/mail/mailboxes/${mailboxId}/threads?${params}`
      );
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch threads');
      }
      return response.data;
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: !!mailboxId && enabled,
  });
}

/**
 * Mark threads as read mutation
 */
export function useMarkThreadsRead() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (threadIds: string[]) => {
      const response = await client.post<ApiResponse<null>>('/api/mail/threads/mark-read', { threadIds });
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to mark threads as read');
      }
      return threadIds;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: threadKeys.lists() });
    },
  });
}

/**
 * Mark threads as unread mutation
 */
export function useMarkThreadsUnread() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (threadIds: string[]) => {
      const response = await client.post<ApiResponse<null>>('/api/mail/threads/mark-unread', { threadIds });
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to mark threads as unread');
      }
      return threadIds;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: threadKeys.lists() });
    },
  });
}

/**
 * Star/unstar threads mutation
 */
export function useToggleThreadsStar() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ threadIds, starred }: { threadIds: string[]; starred: boolean }) => {
      const response = await client.post<ApiResponse<null>>('/api/mail/threads/star', { threadIds, starred });
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to update star status');
      }
      return { threadIds, starred };
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: threadKeys.lists() });
    },
  });
}

/**
 * Move threads to mailbox mutation
 */
export function useMoveThreads() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ threadIds, targetMailboxId }: { threadIds: string[]; targetMailboxId: string }) => {
      const response = await client.post<ApiResponse<null>>('/api/mail/threads/move', {
        threadIds,
        targetMailboxId,
      });
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to move threads');
      }
      return { threadIds, targetMailboxId };
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: threadKeys.lists() });
    },
  });
}

/**
 * Delete threads mutation
 */
export function useDeleteThreads() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (threadIds: string[]) => {
      const response = await client.post<ApiResponse<null>>('/api/mail/threads/delete', { threadIds });
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to delete threads');
      }
      return threadIds;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: threadKeys.lists() });
    },
  });
}
