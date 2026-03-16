import type { Contact, ContactRequest, Email, Phone } from './types';

/**
 * Generate a unique contact UID
 */
export function generateContactUid(domain = 'lurus.cn'): string {
  const timestamp = Date.now();
  const random = Math.random().toString(36).substring(2, 15);
  return `${timestamp}-${random}@${domain}`;
}

/**
 * Generate display name from contact fields
 */
export function generateDisplayName(contact: ContactRequest): string {
  const parts: string[] = [];

  if (contact.prefix) parts.push(contact.prefix);
  if (contact.firstName) parts.push(contact.firstName);
  if (contact.middleName) parts.push(contact.middleName);
  if (contact.lastName) parts.push(contact.lastName);
  if (contact.suffix) parts.push(contact.suffix);

  if (parts.length > 0) {
    return parts.join(' ');
  }

  if (contact.nickname) return contact.nickname;
  if (contact.company) return contact.company;

  // Fall back to primary email
  const primaryEmail = contact.emails?.find((e) => (e as any).isPrimary) || contact.emails?.[0];
  if (primaryEmail) return primaryEmail.value;

  return 'Unknown Contact';
}

/**
 * Get primary email address
 */
export function getPrimaryEmail(contact: Contact): Email | undefined {
  return contact.emails.find((e) => e.isPrimary) || contact.emails[0];
}

/**
 * Get primary phone number
 */
export function getPrimaryPhone(contact: Contact): Phone | undefined {
  return contact.phones.find((p) => p.isPrimary) || contact.phones[0];
}

/**
 * Get contact initials for avatar
 */
export function getContactInitials(contact: Contact): string {
  const name = contact.displayName;

  const words = name.trim().split(/\s+/);
  if (words.length >= 2) {
    return (words[0][0] + words[words.length - 1][0]).toUpperCase();
  }

  return name.substring(0, 2).toUpperCase();
}

/**
 * Validate contact request
 */
export function validateContactRequest(request: ContactRequest): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  // Must have at least name or email
  const hasName = request.firstName || request.lastName || request.nickname || request.company;
  const hasEmail = request.emails && request.emails.length > 0;

  if (!hasName && !hasEmail) {
    errors.push('Contact must have a name or email address');
  }

  // Validate emails
  for (const email of request.emails || []) {
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value)) {
      errors.push(`Invalid email address: ${email.value}`);
    }
  }

  // Validate phone numbers (basic)
  for (const phone of request.phones || []) {
    if (phone.value && phone.value.replace(/[\s\-\(\)\.]/g, '').length < 7) {
      errors.push(`Invalid phone number: ${phone.value}`);
    }
  }

  // Validate website
  if (request.website) {
    try {
      new URL(request.website);
    } catch {
      errors.push('Invalid website URL');
    }
  }

  return { valid: errors.length === 0, errors };
}

/**
 * Search contacts by query
 */
export function searchContacts(contacts: Contact[], query: string): Contact[] {
  const q = query.toLowerCase().trim();
  if (!q) return contacts;

  return contacts.filter((contact) => {
    // Search display name
    if (contact.displayName.toLowerCase().includes(q)) return true;

    // Search email addresses
    for (const email of contact.emails) {
      if (email.value.toLowerCase().includes(q)) return true;
    }

    // Search phone numbers
    for (const phone of contact.phones) {
      if (phone.value.replace(/\D/g, '').includes(q.replace(/\D/g, ''))) return true;
    }

    // Search company
    if (contact.company?.toLowerCase().includes(q)) return true;

    // Search notes
    if (contact.notes?.toLowerCase().includes(q)) return true;

    return false;
  });
}

/**
 * Sort contacts alphabetically
 */
export function sortContacts(contacts: Contact[], field: 'displayName' | 'lastName' | 'company' = 'displayName'): Contact[] {
  return [...contacts].sort((a, b) => {
    const aValue = (a[field] || a.displayName).toLowerCase();
    const bValue = (b[field] || b.displayName).toLowerCase();
    return aValue.localeCompare(bValue);
  });
}

/**
 * Group contacts by first letter
 */
export function groupContactsByLetter(contacts: Contact[]): Map<string, Contact[]> {
  const grouped = new Map<string, Contact[]>();

  for (const contact of contacts) {
    const firstChar = contact.displayName.charAt(0).toUpperCase();
    const key = /[A-Z]/.test(firstChar) ? firstChar : '#';

    const existing = grouped.get(key) || [];
    existing.push(contact);
    grouped.set(key, existing);
  }

  return grouped;
}

/**
 * Merge duplicate contacts
 */
export function findPotentialDuplicates(contacts: Contact[]): Contact[][] {
  const duplicates: Contact[][] = [];
  const processed = new Set<string>();

  for (let i = 0; i < contacts.length; i++) {
    if (processed.has(contacts[i].id)) continue;

    const matches: Contact[] = [contacts[i]];

    for (let j = i + 1; j < contacts.length; j++) {
      if (processed.has(contacts[j].id)) continue;

      if (areContactsSimilar(contacts[i], contacts[j])) {
        matches.push(contacts[j]);
        processed.add(contacts[j].id);
      }
    }

    if (matches.length > 1) {
      duplicates.push(matches);
      processed.add(contacts[i].id);
    }
  }

  return duplicates;
}

/**
 * Check if two contacts are similar (potential duplicates)
 */
function areContactsSimilar(a: Contact, b: Contact): boolean {
  // Same email
  const aEmails = new Set(a.emails.map((e) => e.value.toLowerCase()));
  for (const email of b.emails) {
    if (aEmails.has(email.value.toLowerCase())) return true;
  }

  // Same phone
  const aPhones = new Set(a.phones.map((p) => p.value.replace(/\D/g, '')));
  for (const phone of b.phones) {
    if (aPhones.has(phone.value.replace(/\D/g, ''))) return true;
  }

  // Very similar name
  const aName = a.displayName.toLowerCase();
  const bName = b.displayName.toLowerCase();
  if (aName === bName) return true;

  return false;
}
