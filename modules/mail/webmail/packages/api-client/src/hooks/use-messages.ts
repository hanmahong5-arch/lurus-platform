'use client';

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useFetchClient } from '../react';
import type { ApiResponse, MessageDetail } from '../types';

/**
 * Query key factory for messages
 */
export const messageKeys = {
  all: ['messages'] as const,
  lists: () => [...messageKeys.all, 'list'] as const,
  list: (threadId: string) => [...messageKeys.lists(), threadId] as const,
  details: () => [...messageKeys.all, 'detail'] as const,
  detail: (id: string) => [...messageKeys.details(), id] as const,
};

/**
 * Fetch messages for a thread
 */
export function useMessages(threadId: string, options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: messageKeys.list(threadId),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MessageDetail[]>>(`/api/mail/threads/${threadId}/messages`);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch messages');
      }
      return response.data;
    },
    enabled: !!threadId && (options?.enabled !== false),
  });
}

/**
 * Fetch single message
 */
export function useMessage(id: string, options?: { enabled?: boolean }) {
  const client = useFetchClient();

  return useQuery({
    queryKey: messageKeys.detail(id),
    queryFn: async () => {
      const response = await client.get<ApiResponse<MessageDetail>>(`/api/mail/messages/${id}`);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to fetch message');
      }
      return response.data;
    },
    enabled: !!id && (options?.enabled !== false),
  });
}

/**
 * Send email mutation
 */
export type SendEmailInput = {
  identityId: string;
  to: Array<{ email: string; name?: string }>;
  cc?: Array<{ email: string; name?: string }>;
  bcc?: Array<{ email: string; name?: string }>;
  subject: string;
  html?: string;
  text?: string;
  inReplyTo?: string;
  attachments?: Array<{
    filename: string;
    contentType: string;
    content: string; // Base64
  }>;
};

export function useSendEmail() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: SendEmailInput) => {
      const response = await client.post<ApiResponse<{ messageId: string }>>('/api/mail/email/send', input);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to send email');
      }
      return response.data;
    },
    onSuccess: () => {
      // Invalidate sent mailbox threads
      queryClient.invalidateQueries({ queryKey: ['threads'] });
    },
  });
}

/**
 * Save draft mutation
 */
export type SaveDraftInput = {
  identityId: string;
  to?: Array<{ email: string; name?: string }>;
  cc?: Array<{ email: string; name?: string }>;
  bcc?: Array<{ email: string; name?: string }>;
  subject?: string;
  html?: string;
  text?: string;
  inReplyTo?: string;
  existingDraftId?: string;
};

export function useSaveDraft() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: SaveDraftInput) => {
      const response = await client.post<ApiResponse<{ draftId: string }>>('/api/mail/drafts/save', input);
      if (!response.success || !response.data) {
        throw new Error(response.error?.message || 'Failed to save draft');
      }
      return response.data;
    },
    onSuccess: () => {
      // Invalidate drafts mailbox threads
      queryClient.invalidateQueries({ queryKey: ['threads'] });
    },
  });
}

/**
 * Delete draft mutation
 */
export function useDeleteDraft() {
  const client = useFetchClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (draftId: string) => {
      const response = await client.delete<ApiResponse<null>>(`/api/mail/drafts/${draftId}`);
      if (!response.success) {
        throw new Error(response.error?.message || 'Failed to delete draft');
      }
      return draftId;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['threads'] });
    },
  });
}

/**
 * Download attachment
 */
export function useDownloadAttachment() {
  const client = useFetchClient();

  return useMutation({
    mutationFn: async ({ messageId, attachmentId }: { messageId: string; attachmentId: string }) => {
      const response = await fetch(`/api/mail/messages/${messageId}/attachments/${attachmentId}`);
      if (!response.ok) {
        throw new Error('Failed to download attachment');
      }
      const blob = await response.blob();
      const contentDisposition = response.headers.get('content-disposition');
      const filenameMatch = contentDisposition?.match(/filename="(.+)"/);
      const filename = filenameMatch?.[1] || 'attachment';

      return { blob, filename };
    },
  });
}
