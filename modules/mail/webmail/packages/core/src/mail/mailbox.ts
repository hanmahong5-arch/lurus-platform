import type { Mailbox } from './types';

/**
 * Default mailbox slugs for system folders
 */
export const SYSTEM_MAILBOX_SLUGS = ['inbox', 'sent', 'drafts', 'trash', 'spam', 'archive'] as const;

export type SystemMailboxSlug = (typeof SYSTEM_MAILBOX_SLUGS)[number];

/**
 * Check if mailbox is a system mailbox
 */
export function isSystemMailbox(mailbox: Mailbox): boolean {
  return mailbox.isSystem || SYSTEM_MAILBOX_SLUGS.includes(mailbox.slug as SystemMailboxSlug);
}

/**
 * Get mailbox role from slug
 */
export function getMailboxRole(slug: string): Mailbox['role'] {
  if (SYSTEM_MAILBOX_SLUGS.includes(slug as SystemMailboxSlug)) {
    return slug as Mailbox['role'];
  }
  return 'custom';
}

/**
 * Get default mailbox icon based on role
 */
export function getMailboxIcon(role: Mailbox['role']): string {
  const icons: Record<Mailbox['role'], string> = {
    inbox: 'inbox',
    sent: 'send',
    drafts: 'file-text',
    trash: 'trash-2',
    spam: 'alert-triangle',
    archive: 'archive',
    custom: 'folder',
  };
  return icons[role] || 'folder';
}

/**
 * Get default mailbox order
 */
export function getMailboxOrder(role: Mailbox['role']): number {
  const order: Record<Mailbox['role'], number> = {
    inbox: 0,
    drafts: 1,
    sent: 2,
    spam: 3,
    trash: 4,
    archive: 5,
    custom: 100,
  };
  return order[role] ?? 100;
}

/**
 * Sort mailboxes by standard order
 */
export function sortMailboxes(mailboxes: Mailbox[]): Mailbox[] {
  return [...mailboxes].sort((a, b) => {
    // System mailboxes first
    if (a.isSystem !== b.isSystem) {
      return a.isSystem ? -1 : 1;
    }
    // Then by order
    return a.order - b.order;
  });
}

/**
 * Check if mailbox can be deleted
 */
export function canDeleteMailbox(mailbox: Mailbox): boolean {
  return !mailbox.isSystem && mailbox.role === 'custom';
}

/**
 * Check if mailbox can be renamed
 */
export function canRenameMailbox(mailbox: Mailbox): boolean {
  return !mailbox.isSystem && mailbox.role === 'custom';
}

/**
 * Validate mailbox name
 */
export function validateMailboxName(name: string): { valid: boolean; error?: string } {
  if (!name || name.trim().length === 0) {
    return { valid: false, error: 'Mailbox name is required' };
  }

  if (name.length > 100) {
    return { valid: false, error: 'Mailbox name must be 100 characters or less' };
  }

  // Check for invalid characters
  if (/[<>:"/\\|?*]/.test(name)) {
    return { valid: false, error: 'Mailbox name contains invalid characters' };
  }

  // Check for reserved names
  const reserved = ['inbox', 'sent', 'drafts', 'trash', 'spam', 'archive', 'all', 'starred'];
  if (reserved.includes(name.toLowerCase())) {
    return { valid: false, error: 'This mailbox name is reserved' };
  }

  return { valid: true };
}

/**
 * Generate mailbox slug from name
 */
export function generateMailboxSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
    .substring(0, 50);
}

/**
 * Calculate mailbox statistics
 */
export function calculateMailboxStats(threads: Array<{ unreadCount: number; messageCount: number }>): {
  unreadCount: number;
  totalCount: number;
} {
  return threads.reduce(
    (acc, thread) => ({
      unreadCount: acc.unreadCount + thread.unreadCount,
      totalCount: acc.totalCount + thread.messageCount,
    }),
    { unreadCount: 0, totalCount: 0 }
  );
}
