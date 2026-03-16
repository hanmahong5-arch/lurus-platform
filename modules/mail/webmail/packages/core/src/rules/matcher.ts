import type { RuleCondition, RuleMatch, RuleField, RuleOperator, MessageForRules } from './types';

/**
 * Extract email addresses from various formats
 */
export function extractEmails(value: unknown): string {
  const out: string[] = [];

  const pushEmail = (e: unknown) => {
    const s = typeof e === 'string' ? e : '';
    if (!s) return;
    const m = s.match(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi) ?? [];
    for (const x of m) out.push(x.toLowerCase());
  };

  const walk = (x: unknown): void => {
    if (x === null || x === undefined) return;

    if (typeof x === 'string') {
      pushEmail(x);
      return;
    }

    if (Array.isArray(x)) {
      for (const item of x) walk(item);
      return;
    }

    if (typeof x === 'object') {
      const obj = x as Record<string, unknown>;
      if (typeof obj.address === 'string' && (obj.address as string).includes('@')) {
        out.push((obj.address as string).trim().toLowerCase());
      }

      if (Array.isArray(obj.value)) {
        for (const item of obj.value) {
          const i = item as Record<string, unknown>;
          if (typeof i?.address === 'string' && (i.address as string).includes('@')) {
            out.push((i.address as string).trim().toLowerCase());
          }
        }
      }

      if (typeof obj.text === 'string') pushEmail(obj.text);
      return;
    }
  };

  walk(value);
  return Array.from(new Set(out)).join(', ');
}

/**
 * Convert value to lowercase trimmed string
 */
function asLowerString(v: unknown): string {
  if (v === null || v === undefined) return '';
  return String(v).trim().toLowerCase();
}

/**
 * Check if value is empty
 */
function isEmptyValue(v: unknown): boolean {
  return v === null || v === undefined || (typeof v === 'string' && v.trim() === '');
}

/**
 * Convert to number
 */
function toNumber(v: unknown): number {
  const n = typeof v === 'number' ? v : Number(String(v));
  return Number.isFinite(n) ? n : 0;
}

/**
 * Convert to boolean
 */
function toBool(v: unknown): boolean {
  if (typeof v === 'boolean') return v;
  if (typeof v === 'number') return v !== 0;
  const s = asLowerString(v);
  if (s === 'true') return true;
  if (s === 'false') return false;
  return Boolean(s);
}

/**
 * Try to create a regex from pattern
 */
function tryRegex(pattern: string): RegExp | null {
  try {
    return new RegExp(pattern, 'i');
  } catch {
    return null;
  }
}

/**
 * Get message field value for rule evaluation
 */
export function getMessageFieldValue(message: MessageForRules, field: RuleField): unknown {
  switch (field) {
    case 'from':
      return extractEmails(message.from ?? (message.headersJson as any)?.from);
    case 'to':
      return extractEmails(message.to ?? (message.headersJson as any)?.to);
    case 'cc':
      return extractEmails(message.cc ?? (message.headersJson as any)?.cc);
    case 'bcc':
      return extractEmails(message.bcc ?? (message.headersJson as any)?.bcc);
    case 'reply_to':
      return extractEmails(message.replyTo ?? (message.headersJson as any)?.['reply-to']);
    case 'subject':
      return message.subject ?? '';
    case 'text':
      return message.text ?? '';
    case 'snippet':
      return message.snippet ?? '';
    case 'list_id':
      return message.listId ?? '';
    case 'subscription_key':
      return message.subscriptionKey ?? '';
    case 'has_attachments':
      return message.hasAttachments ?? false;
    case 'size_bytes':
      return message.sizeBytes ?? 0;
    default:
      return '';
  }
}

/**
 * Evaluate a single rule condition
 */
export function evaluateCondition(message: MessageForRules, condition: RuleCondition): boolean {
  const left = getMessageFieldValue(message, condition.field);
  const op = condition.op;
  const right = condition.value;

  // Existence checks
  if (op === 'exists') return !isEmptyValue(left);
  if (op === 'not_exists') return isEmptyValue(left);

  // Boolean operations
  if (condition.field === 'has_attachments') {
    const l = toBool(left);
    const r = toBool(right);
    if (op === 'eq') return l === r;
    if (op === 'not_eq') return l !== r;
    return false;
  }

  // Numeric operations
  if (condition.field === 'size_bytes') {
    const l = toNumber(left);
    const r = toNumber(right);
    if (op === 'eq') return l === r;
    if (op === 'not_eq') return l !== r;
    if (op === 'gt') return l > r;
    if (op === 'gte') return l >= r;
    if (op === 'lt') return l < r;
    if (op === 'lte') return l <= r;
    if (op === 'in') return Array.isArray(right) ? right.map(toNumber).includes(l) : false;
    if (op === 'not_in') return Array.isArray(right) ? !right.map(toNumber).includes(l) : false;
    return false;
  }

  // String operations
  const l = asLowerString(left);
  const rStr = asLowerString(right);

  if (op === 'eq') return l === rStr;
  if (op === 'not_eq') return l !== rStr;
  if (op === 'contains') return rStr ? l.includes(rStr) : false;
  if (op === 'not_contains') return rStr ? !l.includes(rStr) : true;
  if (op === 'starts_with') return rStr ? l.startsWith(rStr) : false;
  if (op === 'ends_with') return rStr ? l.endsWith(rStr) : false;

  if (op === 'regex') {
    const re = rStr ? tryRegex(String(right)) : null;
    return re ? re.test(String(left ?? '')) : false;
  }

  if (op === 'in') {
    if (!Array.isArray(right)) return false;
    const set = right.map(asLowerString);
    return set.includes(l);
  }

  if (op === 'not_in') {
    if (!Array.isArray(right)) return true;
    const set = right.map(asLowerString);
    return !set.includes(l);
  }

  return false;
}

/**
 * Evaluate rule match (all conditions)
 */
export function evaluateMatch(message: MessageForRules, match: RuleMatch): boolean {
  const results = match.conditions.map((c) => evaluateCondition(message, c));

  if (match.logic === 'any') {
    return results.some(Boolean);
  }

  return results.every(Boolean);
}

/**
 * Validate rule match configuration
 */
export function validateRuleMatch(match: RuleMatch): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  if (match.version !== 1) {
    errors.push('Unsupported rule version');
  }

  if (!['all', 'any'].includes(match.logic)) {
    errors.push('Invalid rule logic: must be "all" or "any"');
  }

  if (!match.conditions || match.conditions.length === 0) {
    errors.push('At least one condition is required');
  }

  for (const condition of match.conditions) {
    // Check field
    const validFields = [
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
    ];
    if (!validFields.includes(condition.field)) {
      errors.push(`Invalid field: ${condition.field}`);
    }

    // Check operator
    const validOps = [
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
    ];
    if (!validOps.includes(condition.op)) {
      errors.push(`Invalid operator: ${condition.op}`);
    }

    // Check value requirement
    const noValueOps = ['exists', 'not_exists'];
    if (!noValueOps.includes(condition.op) && condition.value === undefined) {
      errors.push(`Condition ${condition.field} ${condition.op} requires a value`);
    }

    // Validate regex pattern
    if (condition.op === 'regex' && condition.value) {
      const re = tryRegex(String(condition.value));
      if (!re) {
        errors.push(`Invalid regex pattern: ${condition.value}`);
      }
    }
  }

  return { valid: errors.length === 0, errors };
}
