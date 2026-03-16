import { describe, it, expect } from 'vitest';
import {
  IdentitesEnum,
  identityTypesList,
  IdentityStatusEnum,
  identityStatusList,
  IdentityStatusDisplay,
  IdentityStatusMeta,
} from '../src/types/identities';

describe('IdentitesEnum', () => {
  it('should validate all identity types', () => {
    for (const type of identityTypesList) {
      expect(IdentitesEnum.safeParse(type).success).toBe(true);
    }
  });

  it('should reject invalid identity types', () => {
    expect(IdentitesEnum.safeParse('invalid').success).toBe(false);
    expect(IdentitesEnum.safeParse('').success).toBe(false);
    expect(IdentitesEnum.safeParse(null).success).toBe(false);
    expect(IdentitesEnum.safeParse(undefined).success).toBe(false);
  });

  it('should include domain and email types', () => {
    expect(identityTypesList).toContain('domain');
    expect(identityTypesList).toContain('email');
    expect(identityTypesList.length).toBe(2);
  });
});

describe('IdentityStatusEnum', () => {
  it('should validate all identity statuses', () => {
    for (const status of identityStatusList) {
      expect(IdentityStatusEnum.safeParse(status).success).toBe(true);
    }
  });

  it('should reject invalid identity statuses', () => {
    expect(IdentityStatusEnum.safeParse('invalid').success).toBe(false);
    expect(IdentityStatusEnum.safeParse('').success).toBe(false);
    expect(IdentityStatusEnum.safeParse(null).success).toBe(false);
  });

  it('should include all expected statuses', () => {
    expect(identityStatusList).toContain('unverified');
    expect(identityStatusList).toContain('pending');
    expect(identityStatusList).toContain('verified');
    expect(identityStatusList).toContain('failed');
    expect(identityStatusList.length).toBe(4);
  });
});

describe('IdentityStatusDisplay', () => {
  it('should have display text for all statuses', () => {
    for (const status of identityStatusList) {
      expect(IdentityStatusDisplay[status]).toBeDefined();
      expect(typeof IdentityStatusDisplay[status]).toBe('string');
      expect(IdentityStatusDisplay[status].length).toBeGreaterThan(0);
    }
  });

  it('should have correct display text', () => {
    expect(IdentityStatusDisplay.unverified).toBe('Not verified');
    expect(IdentityStatusDisplay.pending).toBe('DNS not set up yet');
    expect(IdentityStatusDisplay.verified).toBe('Verified');
    expect(IdentityStatusDisplay.failed).toBe('Verification failed');
  });
});

describe('IdentityStatusMeta', () => {
  it('should have meta for all statuses', () => {
    for (const status of identityStatusList) {
      expect(IdentityStatusMeta[status]).toBeDefined();
      expect(IdentityStatusMeta[status]).toHaveProperty('label');
      expect(IdentityStatusMeta[status]).toHaveProperty('note');
    }
  });

  it('should have string label and note', () => {
    for (const status of identityStatusList) {
      expect(typeof IdentityStatusMeta[status].label).toBe('string');
      expect(typeof IdentityStatusMeta[status].note).toBe('string');
      expect(IdentityStatusMeta[status].label.length).toBeGreaterThan(0);
      expect(IdentityStatusMeta[status].note.length).toBeGreaterThan(0);
    }
  });

  it('should have informative note text', () => {
    // Each status should have a meaningful note
    expect(IdentityStatusMeta.unverified.note).toMatch(/verification/i);
    expect(IdentityStatusMeta.pending.note).toMatch(/dns/i);
    expect(IdentityStatusMeta.verified.note).toMatch(/verified|ready/i);
    expect(IdentityStatusMeta.failed.note).toMatch(/check|restart/i);
  });
});
