import { describe, it, expect } from 'vitest';
import {
  SmtpAccountFormSchema,
  DomainIdentityFormSchema,
  ProviderAccountFormSchema,
} from '../src/types/form-shapes';

describe('SmtpAccountFormSchema', () => {
  it('should validate valid SMTP account data', () => {
    const validData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: 'My SMTP Account',
      required: {
        SMTP_HOST: 'smtp.example.com',
        SMTP_PORT: '587',
      },
      optional: {},
    };

    const result = SmtpAccountFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should require ulid', () => {
    const invalidData = {
      label: 'My SMTP Account',
    };

    const result = SmtpAccountFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should require label', () => {
    const invalidData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: '',
    };

    const result = SmtpAccountFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should trim label', () => {
    const data = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: '  My Account  ',
    };

    const result = SmtpAccountFormSchema.safeParse(data);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.label).toBe('My Account');
    }
  });

  it('should clean empty values from required', () => {
    const data = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: 'My Account',
      required: {
        SMTP_HOST: 'smtp.example.com',
        SMTP_PORT: '',
        SMTP_USER: null,
      },
    };

    const result = SmtpAccountFormSchema.safeParse(data);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.required).toHaveProperty('SMTP_HOST');
      expect(result.data.required).not.toHaveProperty('SMTP_PORT');
      expect(result.data.required).not.toHaveProperty('SMTP_USER');
    }
  });

  it('should allow optional secretId and accountId', () => {
    const data = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: 'My Account',
      secretId: 'secret-123',
      accountId: 'account-456',
    };

    const result = SmtpAccountFormSchema.safeParse(data);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.secretId).toBe('secret-123');
      expect(result.data.accountId).toBe('account-456');
    }
  });

  it('should allow null secretId and accountId', () => {
    const data = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      label: 'My Account',
      secretId: null,
      accountId: null,
    };

    const result = SmtpAccountFormSchema.safeParse(data);
    expect(result.success).toBe(true);
  });
});

describe('DomainIdentityFormSchema', () => {
  it('should validate valid domain identity', () => {
    const validData = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'domain' as const,
    };

    const result = DomainIdentityFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should require providerOption', () => {
    const invalidData = {
      value: 'example.com',
      kind: 'domain' as const,
    };

    const result = DomainIdentityFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should validate domain format', () => {
    const validDomains = [
      'example.com',
      'mail.example.com',
      'sub.domain.example.org',
      'example.co.uk',
    ];

    for (const domain of validDomains) {
      const data = {
        providerOption: 'ses',
        value: domain,
        kind: 'domain' as const,
      };
      const result = DomainIdentityFormSchema.safeParse(data);
      expect(result.success).toBe(true);
    }
  });

  it('should reject invalid domain format', () => {
    const invalidDomains = [
      'not-a-domain',
      'localhost',
      '-invalid.com',
      'invalid-.com',
      'example',
      'http://example.com',
    ];

    for (const domain of invalidDomains) {
      const data = {
        providerOption: 'ses',
        value: domain,
        kind: 'domain' as const,
      };
      const result = DomainIdentityFormSchema.safeParse(data);
      expect(result.success).toBe(false);
    }
  });

  it('should validate mailFromSubdomain', () => {
    const validData = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'domain' as const,
      mailFromSubdomain: 'mail',
    };

    const result = DomainIdentityFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should allow subdomain format for mailFromSubdomain', () => {
    const validData = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'domain' as const,
      mailFromSubdomain: 'mail.bounce',
    };

    const result = DomainIdentityFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should allow empty mailFromSubdomain', () => {
    const validData = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'domain' as const,
      mailFromSubdomain: '',
    };

    const result = DomainIdentityFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should handle incomingDomain enum', () => {
    const data = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'domain' as const,
      incomingDomain: 'true' as const,
    };

    const result = DomainIdentityFormSchema.safeParse(data);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.incomingDomain).toBe('true');
    }
  });

  it('should only allow domain kind', () => {
    const data = {
      providerOption: 'ses',
      value: 'example.com',
      kind: 'email' as const,
    };

    const result = DomainIdentityFormSchema.safeParse(data);
    expect(result.success).toBe(false);
  });
});

describe('ProviderAccountFormSchema', () => {
  it('should validate valid provider account', () => {
    const validData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      providerId: 'ses',
      required: {
        SES_ACCESS_KEY_ID: 'AKIAIOSFODNN7EXAMPLE',
        SES_SECRET_ACCESS_KEY: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
        SES_REGION: 'us-east-1',
      },
    };

    const result = ProviderAccountFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });

  it('should require ulid', () => {
    const invalidData = {
      providerId: 'ses',
      required: {},
    };

    const result = ProviderAccountFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should require providerId', () => {
    const invalidData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      required: {},
    };

    const result = ProviderAccountFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should reject empty required values', () => {
    const invalidData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      providerId: 'ses',
      required: {
        SES_ACCESS_KEY_ID: '',
        SES_SECRET_ACCESS_KEY: 'valid-key',
      },
    };

    const result = ProviderAccountFormSchema.safeParse(invalidData);
    expect(result.success).toBe(false);
  });

  it('should require all required env values to be non-empty', () => {
    const validData = {
      ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      providerId: 'sendgrid',
      required: {
        SENDGRID_API_KEY: 'SG.valid-api-key',
      },
    };

    const result = ProviderAccountFormSchema.safeParse(validData);
    expect(result.success).toBe(true);
  });
});
