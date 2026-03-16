import { z } from 'zod';

/**
 * Email address schema with optional display name
 */
export const AddressSchema = z.object({
  email: z.string().email(),
  name: z.string().optional(),
});

export type Address = z.infer<typeof AddressSchema>;

/**
 * Email message schema
 */
export const MessageSchema = z.object({
  id: z.string(),
  threadId: z.string(),
  mailboxId: z.string(),
  ownerId: z.string(),
  messageId: z.string().nullable(), // RFC 5322 Message-ID
  inReplyTo: z.string().nullable(),
  references: z.array(z.string()),
  subject: z.string(),
  from: AddressSchema,
  to: z.array(AddressSchema),
  cc: z.array(AddressSchema).optional(),
  bcc: z.array(AddressSchema).optional(),
  replyTo: AddressSchema.optional(),
  date: z.date(),
  text: z.string().optional(),
  html: z.string().optional(),
  snippet: z.string(),
  seen: z.boolean(),
  flagged: z.boolean(),
  answered: z.boolean(),
  draft: z.boolean(),
  hasAttachments: z.boolean(),
  sizeBytes: z.number(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Message = z.infer<typeof MessageSchema>;

/**
 * Thread schema (collection of messages)
 */
export const ThreadSchema = z.object({
  id: z.string(),
  ownerId: z.string(),
  subject: z.string(),
  snippet: z.string(),
  messageCount: z.number(),
  unreadCount: z.number(),
  starred: z.boolean(),
  hasAttachments: z.boolean(),
  latestMessageAt: z.date(),
  participants: z.array(AddressSchema),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Thread = z.infer<typeof ThreadSchema>;

/**
 * Mailbox schema
 */
export const MailboxSchema = z.object({
  id: z.string(),
  identityId: z.string(),
  ownerId: z.string(),
  name: z.string(),
  slug: z.string(),
  role: z.enum(['inbox', 'sent', 'drafts', 'trash', 'spam', 'archive', 'custom']),
  icon: z.string().optional(),
  color: z.string().optional(),
  unreadCount: z.number(),
  totalCount: z.number(),
  order: z.number(),
  isSystem: z.boolean(),
  isHidden: z.boolean(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Mailbox = z.infer<typeof MailboxSchema>;

/**
 * Attachment schema
 */
export const AttachmentSchema = z.object({
  id: z.string(),
  messageId: z.string(),
  filename: z.string(),
  contentType: z.string(),
  sizeBytes: z.number(),
  contentId: z.string().optional(), // For inline images
  storagePath: z.string(),
  isInline: z.boolean(),
});

export type Attachment = z.infer<typeof AttachmentSchema>;

/**
 * Compose email request
 */
export const ComposeRequestSchema = z.object({
  identityId: z.string(),
  to: z.array(AddressSchema).min(1),
  cc: z.array(AddressSchema).optional(),
  bcc: z.array(AddressSchema).optional(),
  subject: z.string(),
  text: z.string().optional(),
  html: z.string().optional(),
  inReplyTo: z.string().optional(),
  references: z.array(z.string()).optional(),
  attachments: z
    .array(
      z.object({
        filename: z.string(),
        contentType: z.string(),
        content: z.string(), // Base64 encoded
      })
    )
    .optional(),
  scheduledAt: z.date().optional(),
  saveToDrafts: z.boolean().optional(),
});

export type ComposeRequest = z.infer<typeof ComposeRequestSchema>;

/**
 * Mail search query
 */
export const SearchQuerySchema = z.object({
  query: z.string(),
  mailboxId: z.string().optional(),
  from: z.string().optional(),
  to: z.string().optional(),
  subject: z.string().optional(),
  hasAttachment: z.boolean().optional(),
  isUnread: z.boolean().optional(),
  isStarred: z.boolean().optional(),
  after: z.date().optional(),
  before: z.date().optional(),
  limit: z.number().min(1).max(100).default(20),
  cursor: z.string().optional(),
});

export type SearchQuery = z.infer<typeof SearchQuerySchema>;
