import { z } from 'zod';

/**
 * API Response wrapper
 */
export type ApiResponse<T> = {
  success: boolean;
  data: T | null;
  error?: {
    code: string;
    message: string;
    details?: unknown;
  };
};

/**
 * Pagination params
 */
export const PaginationSchema = z.object({
  limit: z.number().min(1).max(100).default(20),
  cursor: z.string().optional(),
  direction: z.enum(['forward', 'backward']).default('forward'),
});

export type Pagination = z.infer<typeof PaginationSchema>;

/**
 * Paginated response
 */
export type PaginatedResponse<T> = {
  items: T[];
  nextCursor?: string;
  prevCursor?: string;
  hasMore: boolean;
  total?: number;
};

/**
 * Sort params
 */
export const SortSchema = z.object({
  field: z.string(),
  direction: z.enum(['asc', 'desc']).default('desc'),
});

export type Sort = z.infer<typeof SortSchema>;

/**
 * API client configuration
 */
export type ApiClientConfig = {
  baseUrl: string;
  headers?: Record<string, string>;
  timeout?: number;
  onError?: (error: Error) => void;
  onUnauthorized?: () => void;
};

/**
 * Request options
 */
export type RequestOptions = {
  signal?: AbortSignal;
  headers?: Record<string, string>;
};

/**
 * API error
 */
export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
    public details?: unknown
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/**
 * Mail-related types for API
 */
export type MailIdentity = {
  id: string;
  publicId: string;
  email: string;
  name: string;
  isDefault: boolean;
};

export type MailboxSummary = {
  id: string;
  name: string;
  slug: string;
  unreadCount: number;
  totalCount: number;
};

export type ThreadSummary = {
  threadId: string;
  mailboxId: string;
  subject: string;
  snippet: string;
  from: { email: string; name?: string };
  unreadCount: number;
  starred: boolean;
  hasAttachments: boolean;
  latestAt: Date;
};

export type MessageDetail = {
  id: string;
  threadId: string;
  subject: string;
  from: { email: string; name?: string };
  to: Array<{ email: string; name?: string }>;
  cc?: Array<{ email: string; name?: string }>;
  date: Date;
  html?: string;
  text?: string;
  attachments: Array<{
    id: string;
    filename: string;
    contentType: string;
    size: number;
  }>;
};

/**
 * Calendar-related types for API
 */
export type CalendarSummary = {
  id: string;
  name: string;
  color: string;
  isDefault: boolean;
};

export type EventSummary = {
  id: string;
  calendarId: string;
  summary: string;
  start: Date;
  end: Date;
  allDay: boolean;
  color?: string;
};

/**
 * Contact-related types for API
 */
export type ContactSummary = {
  id: string;
  displayName: string;
  email?: string;
  phone?: string;
  photoUrl?: string;
};
