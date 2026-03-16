import { describe, it, expect } from 'vitest';
import {
  MailboxKindEnum,
  mailboxKindsList,
  MailboxKindDisplay,
  MessageStateEnum,
  messageStatesList,
  MessageStateDisplay,
  SYSTEM_MAILBOXES,
  SMTP_MAILBOXES,
  mailboxSyncPhase,
  messagePriorityList,
  imapQuotaList,
  defaultImapQuota,
  draftMessageStates,
  mailSubscriptionStatusList,
} from '../src/types/mail';

describe('MailboxKindEnum', () => {
  it('should validate all mailbox kinds', () => {
    for (const kind of mailboxKindsList) {
      expect(MailboxKindEnum.safeParse(kind).success).toBe(true);
    }
  });

  it('should reject invalid mailbox kinds', () => {
    expect(MailboxKindEnum.safeParse('invalid').success).toBe(false);
    expect(MailboxKindEnum.safeParse('').success).toBe(false);
    expect(MailboxKindEnum.safeParse(null).success).toBe(false);
    expect(MailboxKindEnum.safeParse(undefined).success).toBe(false);
    expect(MailboxKindEnum.safeParse(123).success).toBe(false);
  });

  it('should have display labels for all kinds', () => {
    for (const kind of mailboxKindsList) {
      expect(MailboxKindDisplay[kind]).toBeDefined();
      expect(typeof MailboxKindDisplay[kind]).toBe('string');
      expect(MailboxKindDisplay[kind].length).toBeGreaterThan(0);
    }
  });

  it('should include all expected mailbox kinds', () => {
    expect(mailboxKindsList).toContain('inbox');
    expect(mailboxKindsList).toContain('sent');
    expect(mailboxKindsList).toContain('drafts');
    expect(mailboxKindsList).toContain('archive');
    expect(mailboxKindsList).toContain('spam');
    expect(mailboxKindsList).toContain('trash');
    expect(mailboxKindsList).toContain('outbox');
    expect(mailboxKindsList).toContain('custom');
  });
});

describe('MessageStateEnum', () => {
  it('should validate all message states', () => {
    for (const state of messageStatesList) {
      expect(MessageStateEnum.safeParse(state).success).toBe(true);
    }
  });

  it('should reject invalid message states', () => {
    expect(MessageStateEnum.safeParse('invalid').success).toBe(false);
    expect(MessageStateEnum.safeParse('').success).toBe(false);
    expect(MessageStateEnum.safeParse(null).success).toBe(false);
  });

  it('should have display labels for all states', () => {
    for (const state of messageStatesList) {
      expect(MessageStateDisplay[state]).toBeDefined();
      expect(typeof MessageStateDisplay[state]).toBe('string');
    }
  });

  it('should include all expected states', () => {
    expect(messageStatesList).toContain('normal');
    expect(messageStatesList).toContain('bounced');
    expect(messageStatesList).toContain('queued');
    expect(messageStatesList).toContain('failed');
  });
});

describe('SYSTEM_MAILBOXES', () => {
  it('should have required properties', () => {
    for (const mailbox of SYSTEM_MAILBOXES) {
      expect(mailbox).toHaveProperty('kind');
      expect(mailbox).toHaveProperty('isDefault');
      expect(mailboxKindsList).toContain(mailbox.kind);
      expect(typeof mailbox.isDefault).toBe('boolean');
    }
  });

  it('should have exactly one default mailbox', () => {
    const defaults = SYSTEM_MAILBOXES.filter(m => m.isDefault);
    expect(defaults.length).toBe(1);
    expect(defaults[0].kind).toBe('inbox');
  });

  it('should include standard system mailboxes', () => {
    const kinds = SYSTEM_MAILBOXES.map(m => m.kind);
    expect(kinds).toContain('inbox');
    expect(kinds).toContain('sent');
    expect(kinds).toContain('trash');
    expect(kinds).toContain('spam');
  });
});

describe('SMTP_MAILBOXES', () => {
  it('should have inbox as the only mailbox', () => {
    expect(SMTP_MAILBOXES.length).toBe(1);
    expect(SMTP_MAILBOXES[0].kind).toBe('inbox');
    expect(SMTP_MAILBOXES[0].isDefault).toBe(true);
  });
});

describe('Mailbox Sync Phases', () => {
  it('should include all sync phases', () => {
    expect(mailboxSyncPhase).toContain('BOOTSTRAP');
    expect(mailboxSyncPhase).toContain('BACKFILL');
    expect(mailboxSyncPhase).toContain('IDLE');
    expect(mailboxSyncPhase.length).toBe(3);
  });
});

describe('Message Priority', () => {
  it('should include all priority levels', () => {
    expect(messagePriorityList).toContain('low');
    expect(messagePriorityList).toContain('medium');
    expect(messagePriorityList).toContain('high');
    expect(messagePriorityList.length).toBe(3);
  });
});

describe('IMAP Quota', () => {
  it('should have valid quota values', () => {
    for (const quota of imapQuotaList) {
      expect(quota).toHaveProperty('label');
      expect(quota).toHaveProperty('value');
      expect(typeof quota.label).toBe('string');
      expect(typeof quota.value).toBe('number');
      expect(quota.value).toBeGreaterThan(0);
    }
  });

  it('should have correct default quota', () => {
    expect(defaultImapQuota).toBe(500 * 1024 * 1024); // 500 MB
  });

  it('should have quotas in ascending order', () => {
    for (let i = 1; i < imapQuotaList.length; i++) {
      expect(imapQuotaList[i].value).toBeGreaterThan(imapQuotaList[i - 1].value);
    }
  });
});

describe('Draft Message States', () => {
  it('should include all draft states', () => {
    expect(draftMessageStates).toContain('draft');
    expect(draftMessageStates).toContain('scheduled');
    expect(draftMessageStates).toContain('sending');
    expect(draftMessageStates).toContain('sent');
    expect(draftMessageStates).toContain('canceled');
    expect(draftMessageStates).toContain('failed');
    expect(draftMessageStates.length).toBe(6);
  });
});

describe('Mail Subscription Status', () => {
  it('should include all subscription statuses', () => {
    expect(mailSubscriptionStatusList).toContain('subscribed');
    expect(mailSubscriptionStatusList).toContain('unsubscribed');
    expect(mailSubscriptionStatusList).toContain('pending');
    expect(mailSubscriptionStatusList).toContain('failed');
    expect(mailSubscriptionStatusList.length).toBe(4);
  });
});
