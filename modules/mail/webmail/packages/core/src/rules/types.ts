import { z } from 'zod';

/**
 * Rule condition field
 */
export const RuleFieldSchema = z.enum([
  'from',
  'to',
  'cc',
  'bcc',
  'reply_to',
  'subject',
  'text',
  'snippet',
  'list_id',
  'subscription_key',
  'has_attachments',
  'size_bytes',
]);

export type RuleField = z.infer<typeof RuleFieldSchema>;

/**
 * Rule condition operator
 */
export const RuleOperatorSchema = z.enum([
  'exists',
  'not_exists',
  'eq',
  'not_eq',
  'contains',
  'not_contains',
  'starts_with',
  'ends_with',
  'regex',
  'gt',
  'gte',
  'lt',
  'lte',
  'in',
  'not_in',
]);

export type RuleOperator = z.infer<typeof RuleOperatorSchema>;

/**
 * Rule condition
 */
export const RuleConditionSchema = z.object({
  field: RuleFieldSchema,
  op: RuleOperatorSchema,
  value: z.any().optional(),
});

export type RuleCondition = z.infer<typeof RuleConditionSchema>;

/**
 * Rule match configuration
 */
export const RuleMatchSchema = z.object({
  version: z.literal(1),
  logic: z.enum(['all', 'any']),
  conditions: z.array(RuleConditionSchema),
});

export type RuleMatch = z.infer<typeof RuleMatchSchema>;

/**
 * Rule action type
 */
export const RuleActionTypeSchema = z.enum([
  'move_to_mailbox',
  'add_label',
  'remove_label',
  'mark_read',
  'mark_unread',
  'star',
  'unstar',
  'archive',
  'trash',
  'delete',
  'forward',
  'reply',
  'skip_inbox',
  'mark_important',
  'mark_not_important',
]);

export type RuleActionType = z.infer<typeof RuleActionTypeSchema>;

/**
 * Rule action
 */
export const RuleActionSchema = z.object({
  type: RuleActionTypeSchema,
  value: z.string().optional(), // Mailbox ID, label ID, forward address, etc.
});

export type RuleAction = z.infer<typeof RuleActionSchema>;

/**
 * Mail rule
 */
export const MailRuleSchema = z.object({
  id: z.string(),
  ownerId: z.string(),
  identityId: z.string(),
  name: z.string(),
  description: z.string().optional(),
  match: RuleMatchSchema,
  actions: z.array(RuleActionSchema),
  isEnabled: z.boolean(),
  stopProcessing: z.boolean(), // If true, don't apply subsequent rules
  order: z.number(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type MailRule = z.infer<typeof MailRuleSchema>;

/**
 * Rule create/update request
 */
export const RuleRequestSchema = z.object({
  name: z.string().min(1),
  description: z.string().optional(),
  match: RuleMatchSchema,
  actions: z.array(RuleActionSchema).min(1),
  isEnabled: z.boolean().optional(),
  stopProcessing: z.boolean().optional(),
});

export type RuleRequest = z.infer<typeof RuleRequestSchema>;

/**
 * Message for rule evaluation (minimal fields needed)
 */
export type MessageForRules = {
  from?: string | { address?: string; name?: string } | null;
  to?: string | Array<{ address?: string }> | null;
  cc?: string | Array<{ address?: string }> | null;
  bcc?: string | Array<{ address?: string }> | null;
  replyTo?: string | { address?: string } | null;
  subject?: string | null;
  text?: string | null;
  snippet?: string | null;
  listId?: string | null;
  subscriptionKey?: string | null;
  hasAttachments?: boolean;
  sizeBytes?: number;
  headersJson?: Record<string, unknown>;
};
