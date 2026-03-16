import { describe, it, expect, vi, beforeEach } from 'vitest';

// Testing the rule matching logic through pure functions
// Note: The main processRules function requires database, so we test the matching logic separately

// Re-implementing the pure functions for testing (these should be extracted and exported in the actual code)
type AddressValue = { address?: string | null; name?: string | null };
type AddressObjectLike =
  | string
  | null
  | undefined
  | { text?: string; html?: string; value?: AddressValue[] }
  | AddressValue[]
  | { address?: string | null }
  | Array<unknown>;

function extractEmails(v: AddressObjectLike): string {
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

  walk(v);
  return Array.from(new Set(out)).join(', ');
}

function asLowerString(v: unknown): string {
  if (v === null || v === undefined) return '';
  return String(v).trim().toLowerCase();
}

function isEmptyValue(v: unknown): boolean {
  return v === null || v === undefined || (typeof v === 'string' && v.trim() === '');
}

function toNumber(v: unknown): number {
  const n = typeof v === 'number' ? v : Number(String(v));
  return Number.isFinite(n) ? n : 0;
}

function toBool(v: unknown): boolean {
  if (typeof v === 'boolean') return v;
  if (typeof v === 'number') return v !== 0;
  const s = asLowerString(v);
  if (s === 'true') return true;
  if (s === 'false') return false;
  return Boolean(s);
}

function tryRegex(pattern: string): RegExp | null {
  try {
    return new RegExp(pattern, 'i');
  } catch {
    return null;
  }
}

type MailRuleField =
  | 'from'
  | 'to'
  | 'cc'
  | 'bcc'
  | 'reply_to'
  | 'subject'
  | 'text'
  | 'snippet'
  | 'list_id'
  | 'subscription_key'
  | 'has_attachments'
  | 'size_bytes';

type MailRuleOp =
  | 'exists'
  | 'not_exists'
  | 'eq'
  | 'not_eq'
  | 'contains'
  | 'not_contains'
  | 'starts_with'
  | 'ends_with'
  | 'regex'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'in'
  | 'not_in';

type MatchCondition = {
  field: MailRuleField;
  op: MailRuleOp;
  value?: unknown;
};

type MailRuleMatchV1 = {
  version: 1;
  logic: 'all' | 'any';
  conditions: MatchCondition[];
};

function getMessageFieldValue(message: Record<string, unknown>, field: MailRuleField): unknown {
  switch (field) {
    case 'from':
      return extractEmails(message.from ?? (message.headersJson as Record<string, unknown>)?.from);
    case 'to':
      return extractEmails(message.to ?? (message.headersJson as Record<string, unknown>)?.to);
    case 'cc':
      return extractEmails(message.cc ?? (message.headersJson as Record<string, unknown>)?.cc);
    case 'bcc':
      return extractEmails(message.bcc ?? (message.headersJson as Record<string, unknown>)?.bcc);
    case 'reply_to':
      return extractEmails(
        message.replyTo ??
          message.reply_to ??
          (message.headersJson as Record<string, unknown>)?.['reply-to']
      );
    case 'subject':
      return message.subject ?? '';
    case 'text':
      return message.text ?? '';
    case 'snippet':
      return message.snippet ?? '';
    case 'list_id':
      return message.listId ?? message.list_id ?? '';
    case 'subscription_key':
      return message.subscriptionKey ?? message.subscription_key ?? '';
    case 'has_attachments':
      return message.hasAttachments ?? message.has_attachments ?? false;
    case 'size_bytes':
      return message.sizeBytes ?? message.size_bytes ?? message.rawSizeBytes ?? 0;
    default:
      return '';
  }
}

function evalCondition(message: Record<string, unknown>, c: MatchCondition): boolean {
  const left = getMessageFieldValue(message, c.field);
  const op = c.op;
  const right = c.value;

  if (op === 'exists') return !isEmptyValue(left);
  if (op === 'not_exists') return isEmptyValue(left);

  if (c.field === 'has_attachments') {
    const l = toBool(left);
    const r = toBool(right);
    if (op === 'eq') return l === r;
    if (op === 'not_eq') return l !== r;
    return false;
  }

  if (c.field === 'size_bytes') {
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

function evalMatch(message: Record<string, unknown>, match: MailRuleMatchV1): boolean {
  const results = match.conditions.map((c) => evalCondition(message, c));
  return match.logic === 'any' ? results.some(Boolean) : results.every(Boolean);
}

describe('extractEmails', () => {
  it('should extract email from plain string', () => {
    expect(extractEmails('user@example.com')).toBe('user@example.com');
  });

  it('should extract email from string with name', () => {
    expect(extractEmails('John Doe <john@example.com>')).toBe('john@example.com');
  });

  it('should extract multiple emails', () => {
    const result = extractEmails('john@example.com, jane@test.org');
    expect(result).toContain('john@example.com');
    expect(result).toContain('jane@test.org');
  });

  it('should extract from object with address property', () => {
    expect(extractEmails({ address: 'user@example.com' })).toBe('user@example.com');
  });

  it('should extract from object with value array', () => {
    const obj = {
      value: [
        { address: 'user1@example.com', name: 'User 1' },
        { address: 'user2@example.com', name: 'User 2' },
      ],
    };
    const result = extractEmails(obj);
    expect(result).toContain('user1@example.com');
    expect(result).toContain('user2@example.com');
  });

  it('should extract from text property', () => {
    expect(extractEmails({ text: 'Contact: support@example.com' })).toBe('support@example.com');
  });

  it('should return empty string for null/undefined', () => {
    expect(extractEmails(null)).toBe('');
    expect(extractEmails(undefined)).toBe('');
  });

  it('should handle array input', () => {
    const arr = [{ address: 'a@example.com' }, { address: 'b@example.com' }];
    const result = extractEmails(arr);
    expect(result).toContain('a@example.com');
    expect(result).toContain('b@example.com');
  });

  it('should lowercase all emails', () => {
    expect(extractEmails('USER@EXAMPLE.COM')).toBe('user@example.com');
  });

  it('should deduplicate emails', () => {
    const result = extractEmails('user@example.com, USER@EXAMPLE.COM');
    expect(result).toBe('user@example.com');
  });
});

describe('evalCondition - existence checks', () => {
  it('should return true for exists when field has value', () => {
    const message = { subject: 'Hello World' };
    expect(evalCondition(message, { field: 'subject', op: 'exists' })).toBe(true);
  });

  it('should return false for exists when field is empty', () => {
    const message = { subject: '' };
    expect(evalCondition(message, { field: 'subject', op: 'exists' })).toBe(false);
  });

  it('should return true for not_exists when field is empty', () => {
    const message = { subject: '' };
    expect(evalCondition(message, { field: 'subject', op: 'not_exists' })).toBe(true);
  });

  it('should return false for not_exists when field has value', () => {
    const message = { subject: 'Hello' };
    expect(evalCondition(message, { field: 'subject', op: 'not_exists' })).toBe(false);
  });
});

describe('evalCondition - string operations', () => {
  const message = { subject: 'Important Newsletter Update' };

  it('should match eq (case insensitive)', () => {
    expect(
      evalCondition(message, { field: 'subject', op: 'eq', value: 'important newsletter update' })
    ).toBe(true);
    expect(evalCondition(message, { field: 'subject', op: 'eq', value: 'different' })).toBe(false);
  });

  it('should match not_eq', () => {
    expect(evalCondition(message, { field: 'subject', op: 'not_eq', value: 'different' })).toBe(
      true
    );
    expect(
      evalCondition(message, { field: 'subject', op: 'not_eq', value: 'important newsletter update' })
    ).toBe(false);
  });

  it('should match contains', () => {
    expect(evalCondition(message, { field: 'subject', op: 'contains', value: 'newsletter' })).toBe(
      true
    );
    expect(evalCondition(message, { field: 'subject', op: 'contains', value: 'missing' })).toBe(
      false
    );
  });

  it('should match not_contains', () => {
    expect(
      evalCondition(message, { field: 'subject', op: 'not_contains', value: 'missing' })
    ).toBe(true);
    expect(
      evalCondition(message, { field: 'subject', op: 'not_contains', value: 'newsletter' })
    ).toBe(false);
  });

  it('should match starts_with', () => {
    expect(
      evalCondition(message, { field: 'subject', op: 'starts_with', value: 'important' })
    ).toBe(true);
    expect(evalCondition(message, { field: 'subject', op: 'starts_with', value: 'update' })).toBe(
      false
    );
  });

  it('should match ends_with', () => {
    expect(evalCondition(message, { field: 'subject', op: 'ends_with', value: 'update' })).toBe(
      true
    );
    expect(
      evalCondition(message, { field: 'subject', op: 'ends_with', value: 'important' })
    ).toBe(false);
  });

  it('should match regex', () => {
    expect(evalCondition(message, { field: 'subject', op: 'regex', value: '^Important' })).toBe(
      true
    );
    expect(
      evalCondition(message, { field: 'subject', op: 'regex', value: 'Newsletter.*Update' })
    ).toBe(true);
    expect(evalCondition(message, { field: 'subject', op: 'regex', value: '^Update' })).toBe(false);
  });

  it('should handle invalid regex gracefully', () => {
    expect(evalCondition(message, { field: 'subject', op: 'regex', value: '[invalid(' })).toBe(
      false
    );
  });
});

describe('evalCondition - set operations', () => {
  const message = { subject: 'Newsletter' };

  it('should match in array', () => {
    expect(
      evalCondition(message, { field: 'subject', op: 'in', value: ['newsletter', 'alert', 'news'] })
    ).toBe(true);
    expect(
      evalCondition(message, { field: 'subject', op: 'in', value: ['alert', 'news'] })
    ).toBe(false);
  });

  it('should match not_in array', () => {
    expect(
      evalCondition(message, { field: 'subject', op: 'not_in', value: ['alert', 'news'] })
    ).toBe(true);
    expect(
      evalCondition(message, { field: 'subject', op: 'not_in', value: ['newsletter', 'alert'] })
    ).toBe(false);
  });
});

describe('evalCondition - numeric operations', () => {
  const message = { sizeBytes: 1000000 }; // 1MB

  it('should match gt', () => {
    expect(evalCondition(message, { field: 'size_bytes', op: 'gt', value: 500000 })).toBe(true);
    expect(evalCondition(message, { field: 'size_bytes', op: 'gt', value: 2000000 })).toBe(false);
  });

  it('should match gte', () => {
    expect(evalCondition(message, { field: 'size_bytes', op: 'gte', value: 1000000 })).toBe(true);
    expect(evalCondition(message, { field: 'size_bytes', op: 'gte', value: 1000001 })).toBe(false);
  });

  it('should match lt', () => {
    expect(evalCondition(message, { field: 'size_bytes', op: 'lt', value: 2000000 })).toBe(true);
    expect(evalCondition(message, { field: 'size_bytes', op: 'lt', value: 500000 })).toBe(false);
  });

  it('should match lte', () => {
    expect(evalCondition(message, { field: 'size_bytes', op: 'lte', value: 1000000 })).toBe(true);
    expect(evalCondition(message, { field: 'size_bytes', op: 'lte', value: 999999 })).toBe(false);
  });

  it('should match eq for numbers', () => {
    expect(evalCondition(message, { field: 'size_bytes', op: 'eq', value: 1000000 })).toBe(true);
    expect(evalCondition(message, { field: 'size_bytes', op: 'eq', value: 999999 })).toBe(false);
  });
});

describe('evalCondition - boolean operations', () => {
  it('should match has_attachments true', () => {
    const message = { hasAttachments: true };
    expect(evalCondition(message, { field: 'has_attachments', op: 'eq', value: true })).toBe(true);
    expect(evalCondition(message, { field: 'has_attachments', op: 'eq', value: false })).toBe(
      false
    );
  });

  it('should match has_attachments false', () => {
    const message = { hasAttachments: false };
    expect(evalCondition(message, { field: 'has_attachments', op: 'eq', value: false })).toBe(true);
    expect(evalCondition(message, { field: 'has_attachments', op: 'not_eq', value: true })).toBe(
      true
    );
  });
});

describe('evalMatch - logic types', () => {
  const message = {
    subject: 'Newsletter Update',
    from: { address: 'newsletter@example.com' },
    sizeBytes: 50000,
  };

  it('should match all conditions with "all" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'subject', op: 'contains', value: 'newsletter' },
        { field: 'from', op: 'contains', value: 'example.com' },
      ],
    };
    expect(evalMatch(message, match)).toBe(true);
  });

  it('should fail if any condition fails with "all" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'subject', op: 'contains', value: 'newsletter' },
        { field: 'from', op: 'contains', value: 'other.com' },
      ],
    };
    expect(evalMatch(message, match)).toBe(false);
  });

  it('should match if any condition passes with "any" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'any',
      conditions: [
        { field: 'subject', op: 'contains', value: 'missing' },
        { field: 'from', op: 'contains', value: 'example.com' },
      ],
    };
    expect(evalMatch(message, match)).toBe(true);
  });

  it('should fail if all conditions fail with "any" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'any',
      conditions: [
        { field: 'subject', op: 'contains', value: 'missing' },
        { field: 'from', op: 'contains', value: 'other.com' },
      ],
    };
    expect(evalMatch(message, match)).toBe(false);
  });

  it('should return true for empty conditions with "all" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [],
    };
    expect(evalMatch(message, match)).toBe(true);
  });

  it('should return false for empty conditions with "any" logic', () => {
    const match: MailRuleMatchV1 = {
      version: 1,
      logic: 'any',
      conditions: [],
    };
    expect(evalMatch(message, match)).toBe(false);
  });
});

describe('getMessageFieldValue', () => {
  it('should extract from field correctly', () => {
    const message = { from: { address: 'sender@example.com' } };
    expect(getMessageFieldValue(message, 'from')).toBe('sender@example.com');
  });

  it('should extract to field from headersJson', () => {
    const message = { headersJson: { to: 'recipient@example.com' } };
    expect(getMessageFieldValue(message, 'to')).toBe('recipient@example.com');
  });

  it('should return empty string for missing field', () => {
    expect(getMessageFieldValue({}, 'subject')).toBe('');
  });

  it('should return 0 for missing size_bytes', () => {
    expect(getMessageFieldValue({}, 'size_bytes')).toBe(0);
  });

  it('should return false for missing has_attachments', () => {
    expect(getMessageFieldValue({}, 'has_attachments')).toBe(false);
  });
});

describe('helper functions', () => {
  describe('asLowerString', () => {
    it('should convert to lowercase trimmed string', () => {
      expect(asLowerString('  HELLO WORLD  ')).toBe('hello world');
    });

    it('should return empty string for null/undefined', () => {
      expect(asLowerString(null)).toBe('');
      expect(asLowerString(undefined)).toBe('');
    });
  });

  describe('toNumber', () => {
    it('should return number as-is', () => {
      expect(toNumber(42)).toBe(42);
    });

    it('should parse string to number', () => {
      expect(toNumber('42')).toBe(42);
    });

    it('should return 0 for invalid number', () => {
      expect(toNumber('not a number')).toBe(0);
      expect(toNumber(NaN)).toBe(0);
      expect(toNumber(Infinity)).toBe(0);
    });
  });

  describe('toBool', () => {
    it('should return boolean as-is', () => {
      expect(toBool(true)).toBe(true);
      expect(toBool(false)).toBe(false);
    });

    it('should parse string "true"/"false"', () => {
      expect(toBool('true')).toBe(true);
      expect(toBool('false')).toBe(false);
      expect(toBool('TRUE')).toBe(true);
    });

    it('should convert 0 to false and non-zero to true', () => {
      expect(toBool(0)).toBe(false);
      expect(toBool(1)).toBe(true);
      expect(toBool(-1)).toBe(true);
    });
  });

  describe('tryRegex', () => {
    it('should return RegExp for valid pattern', () => {
      const re = tryRegex('^test');
      expect(re).toBeInstanceOf(RegExp);
      expect(re?.test('test')).toBe(true);
    });

    it('should return null for invalid pattern', () => {
      expect(tryRegex('[invalid(')).toBeNull();
    });
  });
});
