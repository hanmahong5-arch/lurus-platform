import type { Contact, ContactRequest, Email, Phone, Address } from './types';

/**
 * Parse vCard string to ContactRequest
 */
export function parseVCard(vcard: string): ContactRequest | null {
  try {
    const lines = vcard.split(/\r?\n/);
    const contact: ContactRequest = {
      emails: [],
      phones: [],
      addresses: [],
      labels: [],
    };

    let inVCard = false;
    let currentValue = '';

    for (let i = 0; i < lines.length; i++) {
      let line = lines[i];

      // Handle line folding (continuation lines start with space or tab)
      while (i + 1 < lines.length && /^[ \t]/.test(lines[i + 1])) {
        i++;
        line += lines[i].substring(1);
      }

      if (line.startsWith('BEGIN:VCARD')) {
        inVCard = true;
        continue;
      }

      if (line.startsWith('END:VCARD')) {
        inVCard = false;
        break;
      }

      if (!inVCard) continue;

      // Parse property
      const colonIndex = line.indexOf(':');
      if (colonIndex === -1) continue;

      const propertyPart = line.substring(0, colonIndex);
      const value = line.substring(colonIndex + 1);

      // Parse property name and parameters
      const [propertyName, ...params] = propertyPart.split(';');
      const typeParam = params.find((p) => p.startsWith('TYPE='))?.substring(5).split(',')[0].toLowerCase();

      switch (propertyName.toUpperCase()) {
        case 'N':
          const [lastName, firstName, middleName, prefix, suffix] = value.split(';');
          contact.lastName = lastName || undefined;
          contact.firstName = firstName || undefined;
          contact.middleName = middleName || undefined;
          contact.prefix = prefix || undefined;
          contact.suffix = suffix || undefined;
          break;

        case 'FN':
          // Full name - we'll compute displayName from N fields
          break;

        case 'NICKNAME':
          contact.nickname = value;
          break;

        case 'ORG':
          const [company, department] = value.split(';');
          contact.company = company || undefined;
          contact.department = department || undefined;
          break;

        case 'TITLE':
          contact.jobTitle = value;
          break;

        case 'EMAIL':
          contact.emails!.push({
            type: (typeParam || 'other') as any,
            value: value,
          });
          break;

        case 'TEL':
          contact.phones!.push({
            type: (typeParam || 'other') as any,
            value: value,
          });
          break;

        case 'ADR':
          const [, , street, city, state, postalCode, country] = value.split(';');
          contact.addresses!.push({
            type: (typeParam || 'other') as any,
            street: street || undefined,
            city: city || undefined,
            state: state || undefined,
            postalCode: postalCode || undefined,
            country: country || undefined,
          });
          break;

        case 'BDAY':
          contact.birthday = parseVCardDate(value);
          break;

        case 'ANNIVERSARY':
          contact.anniversary = parseVCardDate(value);
          break;

        case 'URL':
          contact.website = value;
          break;

        case 'NOTE':
          contact.notes = value.replace(/\\n/g, '\n').replace(/\\,/g, ',');
          break;

        case 'CATEGORIES':
          contact.labels = value.split(',').map((l) => l.trim());
          break;
      }
    }

    return contact;
  } catch (e) {
    console.error('Failed to parse vCard:', e);
    return null;
  }
}

/**
 * Generate vCard string from Contact
 */
export function generateVCard(contact: Contact): string {
  const lines: string[] = ['BEGIN:VCARD', 'VERSION:3.0'];

  // UID
  lines.push(`UID:${contact.uid}`);

  // N (structured name)
  const n = [
    contact.lastName || '',
    contact.firstName || '',
    contact.middleName || '',
    contact.prefix || '',
    contact.suffix || '',
  ].join(';');
  lines.push(`N:${n}`);

  // FN (formatted name)
  lines.push(`FN:${escapeVCardValue(contact.displayName)}`);

  // Nickname
  if (contact.nickname) {
    lines.push(`NICKNAME:${escapeVCardValue(contact.nickname)}`);
  }

  // ORG
  if (contact.company || contact.department) {
    lines.push(`ORG:${escapeVCardValue(contact.company || '')};${escapeVCardValue(contact.department || '')}`);
  }

  // TITLE
  if (contact.jobTitle) {
    lines.push(`TITLE:${escapeVCardValue(contact.jobTitle)}`);
  }

  // EMAIL
  for (const email of contact.emails) {
    const type = email.type.toUpperCase();
    lines.push(`EMAIL;TYPE=${type}:${email.value}`);
  }

  // TEL
  for (const phone of contact.phones) {
    const type = phone.type.toUpperCase();
    lines.push(`TEL;TYPE=${type}:${phone.value}`);
  }

  // ADR
  for (const addr of contact.addresses) {
    const type = addr.type.toUpperCase();
    const adr = [
      '', // PO Box
      '', // Extended
      addr.street || '',
      addr.city || '',
      addr.state || '',
      addr.postalCode || '',
      addr.country || '',
    ].join(';');
    lines.push(`ADR;TYPE=${type}:${adr}`);
  }

  // BDAY
  if (contact.birthday) {
    lines.push(`BDAY:${formatVCardDate(contact.birthday)}`);
  }

  // ANNIVERSARY
  if (contact.anniversary) {
    lines.push(`ANNIVERSARY:${formatVCardDate(contact.anniversary)}`);
  }

  // URL
  if (contact.website) {
    lines.push(`URL:${contact.website}`);
  }

  // NOTE
  if (contact.notes) {
    lines.push(`NOTE:${escapeVCardValue(contact.notes)}`);
  }

  // CATEGORIES (labels)
  if (contact.labels.length > 0) {
    lines.push(`CATEGORIES:${contact.labels.join(',')}`);
  }

  // REV (revision timestamp)
  lines.push(`REV:${formatVCardDate(contact.updatedAt)}`);

  lines.push('END:VCARD');

  return lines.join('\r\n');
}

/**
 * Escape special characters in vCard values
 */
function escapeVCardValue(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/,/g, '\\,')
    .replace(/;/g, '\\;')
    .replace(/\n/g, '\\n');
}

/**
 * Parse vCard date format
 */
function parseVCardDate(value: string): Date | undefined {
  try {
    // Try YYYY-MM-DD
    if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
      return new Date(value);
    }
    // Try YYYYMMDD
    if (/^\d{8}$/.test(value)) {
      const year = parseInt(value.substring(0, 4));
      const month = parseInt(value.substring(4, 6)) - 1;
      const day = parseInt(value.substring(6, 8));
      return new Date(year, month, day);
    }
    return undefined;
  } catch {
    return undefined;
  }
}

/**
 * Format date for vCard
 */
function formatVCardDate(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

/**
 * Parse multiple vCards from a string (for import)
 */
export function parseVCards(content: string): ContactRequest[] {
  const contacts: ContactRequest[] = [];
  const vcards = content.split(/(?=BEGIN:VCARD)/gi);

  for (const vcard of vcards) {
    if (!vcard.trim()) continue;
    const contact = parseVCard(vcard);
    if (contact) {
      contacts.push(contact);
    }
  }

  return contacts;
}

/**
 * Generate multiple vCards for export
 */
export function generateVCards(contacts: Contact[]): string {
  return contacts.map(generateVCard).join('\r\n');
}
