import { describe, it, expect } from 'vitest';
import {
  ProvidersEnum,
  providersList,
  ProviderLabels,
  PROVIDERS,
  SMTP_SPEC,
  STORAGE_PROVIDERS,
} from '../src/types/providers';

describe('ProvidersEnum', () => {
  it('should validate all providers', () => {
    for (const provider of providersList) {
      expect(ProvidersEnum.safeParse(provider).success).toBe(true);
    }
  });

  it('should reject invalid providers', () => {
    expect(ProvidersEnum.safeParse('invalid').success).toBe(false);
    expect(ProvidersEnum.safeParse('').success).toBe(false);
    expect(ProvidersEnum.safeParse(null).success).toBe(false);
    expect(ProvidersEnum.safeParse(undefined).success).toBe(false);
    expect(ProvidersEnum.safeParse(123).success).toBe(false);
  });

  it('should include all expected providers', () => {
    expect(providersList).toContain('smtp');
    expect(providersList).toContain('ses');
    expect(providersList).toContain('mailgun');
    expect(providersList).toContain('postmark');
    expect(providersList).toContain('sendgrid');
    expect(providersList).toContain('s3');
  });
});

describe('ProviderLabels', () => {
  it('should have labels for all providers', () => {
    for (const provider of providersList) {
      expect(ProviderLabels[provider]).toBeDefined();
      expect(typeof ProviderLabels[provider]).toBe('string');
      expect(ProviderLabels[provider].length).toBeGreaterThan(0);
    }
  });

  it('should have correct human-readable labels', () => {
    expect(ProviderLabels.smtp).toBe('Generic SMTP');
    expect(ProviderLabels.ses).toBe('Amazon SES');
    expect(ProviderLabels.mailgun).toBe('Mailgun');
    expect(ProviderLabels.postmark).toBe('Postmark');
    expect(ProviderLabels.sendgrid).toBe('SendGrid');
    expect(ProviderLabels.s3).toBe('AWS S3');
  });
});

describe('PROVIDERS (API Providers)', () => {
  it('should have valid provider specs', () => {
    for (const provider of PROVIDERS) {
      expect(provider).toHaveProperty('key');
      expect(provider).toHaveProperty('name');
      expect(provider).toHaveProperty('docsUrl');
      expect(provider).toHaveProperty('requiredEnv');

      expect(typeof provider.key).toBe('string');
      expect(typeof provider.name).toBe('string');
      expect(typeof provider.docsUrl).toBe('string');
      expect(Array.isArray(provider.requiredEnv)).toBe(true);

      // Validate URL format
      expect(provider.docsUrl).toMatch(/^https?:\/\//);

      // Required env should have at least one item
      expect(provider.requiredEnv.length).toBeGreaterThan(0);
    }
  });

  it('should not include smtp (SMTP is separate)', () => {
    const keys = PROVIDERS.map(p => p.key);
    expect(keys).not.toContain('smtp');
  });

  it('should have SES provider with correct required env', () => {
    const ses = PROVIDERS.find(p => p.key === 'ses');
    expect(ses).toBeDefined();
    expect(ses?.requiredEnv).toContain('SES_ACCESS_KEY_ID');
    expect(ses?.requiredEnv).toContain('SES_SECRET_ACCESS_KEY');
    expect(ses?.requiredEnv).toContain('SES_REGION');
  });

  it('should have SendGrid provider with API key requirement', () => {
    const sendgrid = PROVIDERS.find(p => p.key === 'sendgrid');
    expect(sendgrid).toBeDefined();
    expect(sendgrid?.requiredEnv).toContain('SENDGRID_API_KEY');
    expect(sendgrid?.requiredEnv.length).toBe(1);
  });

  it('should have Mailgun provider with API key requirement', () => {
    const mailgun = PROVIDERS.find(p => p.key === 'mailgun');
    expect(mailgun).toBeDefined();
    expect(mailgun?.requiredEnv).toContain('MAILGUN_API_KEY');
    expect(mailgun?.requiredEnv.length).toBe(1);
  });

  it('should have Postmark provider with token requirements', () => {
    const postmark = PROVIDERS.find(p => p.key === 'postmark');
    expect(postmark).toBeDefined();
    expect(postmark?.requiredEnv).toContain('POSTMARK_SERVER_TOKEN');
    expect(postmark?.requiredEnv).toContain('POSTMARK_ACCOUNT_TOKEN');
  });
});

describe('SMTP_SPEC', () => {
  it('should have correct key', () => {
    expect(SMTP_SPEC.key).toBe('smtp');
  });

  it('should have all required SMTP env vars', () => {
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_HOST');
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_PORT');
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_USERNAME');
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_PASSWORD');
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_SECURE');
    expect(SMTP_SPEC.requiredEnv).toContain('SMTP_POOL');
  });

  it('should have optional IMAP env vars', () => {
    expect(SMTP_SPEC.optionalEnv).toContain('IMAP_HOST');
    expect(SMTP_SPEC.optionalEnv).toContain('IMAP_PORT');
    expect(SMTP_SPEC.optionalEnv).toContain('IMAP_USERNAME');
    expect(SMTP_SPEC.optionalEnv).toContain('IMAP_PASSWORD');
    expect(SMTP_SPEC.optionalEnv).toContain('IMAP_SECURE');
  });

  it('should have valid docs URL', () => {
    expect(SMTP_SPEC.docsUrl).toMatch(/^https?:\/\//);
  });

  it('should have help text', () => {
    expect(SMTP_SPEC.help).toBeDefined();
    expect(typeof SMTP_SPEC.help).toBe('string');
    expect(SMTP_SPEC.help.length).toBeGreaterThan(0);
  });
});

describe('STORAGE_PROVIDERS', () => {
  it('should include S3 provider', () => {
    expect(STORAGE_PROVIDERS.length).toBe(1);
    expect(STORAGE_PROVIDERS[0].key).toBe('s3');
  });

  it('should have S3 with correct required env', () => {
    const s3 = STORAGE_PROVIDERS[0];
    expect(s3.requiredEnv).toContain('S3_ACCESS_KEY_ID');
    expect(s3.requiredEnv).toContain('S3_SECRET_ACCESS_KEY');
    expect(s3.requiredEnv).toContain('S3_REGION');
  });

  it('should have valid S3 docs URL', () => {
    expect(STORAGE_PROVIDERS[0].docsUrl).toMatch(/^https:\/\/docs\.aws\.amazon\.com\/s3/);
  });
});
