import type { Address, Message, ComposeRequest } from './types';

/**
 * Extract email addresses from various formats
 */
export function extractEmails(
  value: string | { address?: string; name?: string } | { value?: Array<{ address?: string }> } | null | undefined
): Address[] {
  if (!value) return [];

  if (typeof value === 'string') {
    const regex = /([A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,})/gi;
    const matches = value.match(regex) || [];
    return matches.map((email) => ({ email: email.toLowerCase() }));
  }

  if (Array.isArray((value as any).value)) {
    return (value as { value: Array<{ address?: string; name?: string }> }).value
      .filter((v) => v.address)
      .map((v) => ({
        email: v.address!.toLowerCase(),
        name: v.name,
      }));
  }

  if ((value as any).address) {
    return [
      {
        email: (value as { address: string }).address.toLowerCase(),
        name: (value as { name?: string }).name,
      },
    ];
  }

  return [];
}

/**
 * Generate message snippet from content
 */
export function generateSnippet(text: string | undefined, html: string | undefined, maxLength = 200): string {
  let content = text || '';

  if (!content && html) {
    // Strip HTML tags and decode entities
    content = html
      .replace(/<style[^>]*>[\s\S]*?<\/style>/gi, '')
      .replace(/<script[^>]*>[\s\S]*?<\/script>/gi, '')
      .replace(/<[^>]+>/g, ' ')
      .replace(/&nbsp;/g, ' ')
      .replace(/&amp;/g, '&')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'");
  }

  // Clean up whitespace
  content = content.replace(/\s+/g, ' ').trim();

  // Truncate
  if (content.length > maxLength) {
    content = content.substring(0, maxLength - 3) + '...';
  }

  return content;
}

/**
 * Check if email has attachments based on content type
 */
export function hasAttachments(contentType: string | undefined): boolean {
  if (!contentType) return false;
  return contentType.toLowerCase().includes('multipart/mixed') || contentType.toLowerCase().includes('attachment');
}

/**
 * Parse In-Reply-To header to get thread reference
 */
export function parseInReplyTo(inReplyTo: string | undefined): string | null {
  if (!inReplyTo) return null;

  const match = inReplyTo.match(/<([^>]+)>/);
  return match ? match[1] : inReplyTo.trim();
}

/**
 * Parse References header to get thread chain
 */
export function parseReferences(references: string | undefined): string[] {
  if (!references) return [];

  const matches = references.match(/<[^>]+>/g) || [];
  return matches.map((ref) => ref.slice(1, -1));
}

/**
 * Format address for display
 */
export function formatAddress(address: Address): string {
  if (address.name) {
    return `${address.name} <${address.email}>`;
  }
  return address.email;
}

/**
 * Format multiple addresses for display
 */
export function formatAddresses(addresses: Address[]): string {
  return addresses.map(formatAddress).join(', ');
}

/**
 * Validate compose request
 */
export function validateComposeRequest(request: ComposeRequest): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  if (!request.to || request.to.length === 0) {
    errors.push('At least one recipient is required');
  }

  if (!request.subject?.trim()) {
    errors.push('Subject is required');
  }

  if (!request.text?.trim() && !request.html?.trim()) {
    errors.push('Message body is required');
  }

  // Validate attachment sizes (max 25MB each)
  const MAX_ATTACHMENT_SIZE = 25 * 1024 * 1024;
  for (const att of request.attachments || []) {
    const size = Buffer.from(att.content, 'base64').length;
    if (size > MAX_ATTACHMENT_SIZE) {
      errors.push(`Attachment "${att.filename}" exceeds maximum size of 25MB`);
    }
  }

  return { valid: errors.length === 0, errors };
}

/**
 * Check if message is a reply
 */
export function isReply(subject: string): boolean {
  return /^(re|fw|fwd):\s*/i.test(subject);
}

/**
 * Get clean subject without Re:/Fwd: prefixes
 */
export function cleanSubject(subject: string): string {
  return subject.replace(/^(re|fw|fwd):\s*/gi, '').trim();
}

/**
 * Compare messages for sorting by date
 */
export function compareByDate(a: Message, b: Message, ascending = true): number {
  const diff = a.date.getTime() - b.date.getTime();
  return ascending ? diff : -diff;
}
