import { describe, it, expect } from 'vitest';
import {
  mailRulesLogicList,
  mailRulesFieldsList,
  mailRulesOpsList,
  mailRulesActionsList,
  type MailRuleMatchV1,
} from '../src/types/mail-rules';

describe('Mail Rules Logic', () => {
  it('should include all and any logic types', () => {
    expect(mailRulesLogicList).toContain('all');
    expect(mailRulesLogicList).toContain('any');
    expect(mailRulesLogicList.length).toBe(2);
  });
});

describe('Mail Rules Fields', () => {
  it('should include all expected fields', () => {
    const expectedFields = [
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

    for (const field of expectedFields) {
      expect(mailRulesFieldsList).toContain(field);
    }
  });

  it('should have email address fields', () => {
    const emailFields = ['from', 'to', 'cc', 'bcc', 'reply_to'];
    for (const field of emailFields) {
      expect(mailRulesFieldsList).toContain(field);
    }
  });

  it('should have content fields', () => {
    expect(mailRulesFieldsList).toContain('subject');
    expect(mailRulesFieldsList).toContain('text');
    expect(mailRulesFieldsList).toContain('snippet');
  });

  it('should have metadata fields', () => {
    expect(mailRulesFieldsList).toContain('has_attachments');
    expect(mailRulesFieldsList).toContain('size_bytes');
    expect(mailRulesFieldsList).toContain('list_id');
    expect(mailRulesFieldsList).toContain('subscription_key');
  });
});

describe('Mail Rules Operations', () => {
  it('should include existence checks', () => {
    expect(mailRulesOpsList).toContain('exists');
    expect(mailRulesOpsList).toContain('not_exists');
  });

  it('should include equality operations', () => {
    expect(mailRulesOpsList).toContain('eq');
    expect(mailRulesOpsList).toContain('not_eq');
  });

  it('should include string operations', () => {
    expect(mailRulesOpsList).toContain('contains');
    expect(mailRulesOpsList).toContain('not_contains');
    expect(mailRulesOpsList).toContain('starts_with');
    expect(mailRulesOpsList).toContain('ends_with');
    expect(mailRulesOpsList).toContain('regex');
  });

  it('should include comparison operations', () => {
    expect(mailRulesOpsList).toContain('gt');
    expect(mailRulesOpsList).toContain('gte');
    expect(mailRulesOpsList).toContain('lt');
    expect(mailRulesOpsList).toContain('lte');
  });

  it('should include set operations', () => {
    expect(mailRulesOpsList).toContain('in');
    expect(mailRulesOpsList).toContain('not_in');
  });

  it('should have expected total count', () => {
    expect(mailRulesOpsList.length).toBe(15);
  });
});

describe('Mail Rules Actions', () => {
  it('should include all expected actions', () => {
    expect(mailRulesActionsList).toContain('mark_read');
    expect(mailRulesActionsList).toContain('flag');
    expect(mailRulesActionsList).toContain('add_label');
    expect(mailRulesActionsList).toContain('trash');
    expect(mailRulesActionsList.length).toBe(4);
  });
});

describe('MailRuleMatchV1 Type', () => {
  it('should allow valid rule match objects', () => {
    const validRule: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'from', op: 'contains', value: 'example.com' },
        { field: 'subject', op: 'starts_with', value: '[Newsletter]' },
      ],
    };

    expect(validRule.version).toBe(1);
    expect(validRule.logic).toBe('all');
    expect(validRule.conditions.length).toBe(2);
  });

  it('should allow rule with any logic', () => {
    const rule: MailRuleMatchV1 = {
      version: 1,
      logic: 'any',
      conditions: [
        { field: 'from', op: 'eq', value: 'noreply@example.com' },
        { field: 'from', op: 'eq', value: 'no-reply@example.com' },
      ],
    };

    expect(rule.logic).toBe('any');
  });

  it('should allow existence check without value', () => {
    const rule: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'has_attachments', op: 'exists' },
      ],
    };

    expect(rule.conditions[0].value).toBeUndefined();
  });

  it('should allow size comparison with numeric value', () => {
    const rule: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'size_bytes', op: 'gt', value: 1000000 }, // > 1MB
      ],
    };

    expect(rule.conditions[0].field).toBe('size_bytes');
    expect(rule.conditions[0].op).toBe('gt');
    expect(rule.conditions[0].value).toBe(1000000);
  });

  it('should allow regex operation', () => {
    const rule: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [
        { field: 'subject', op: 'regex', value: '^\\[URGENT\\]' },
      ],
    };

    expect(rule.conditions[0].op).toBe('regex');
  });

  it('should allow empty conditions array', () => {
    const rule: MailRuleMatchV1 = {
      version: 1,
      logic: 'all',
      conditions: [],
    };

    expect(rule.conditions.length).toBe(0);
  });
});
