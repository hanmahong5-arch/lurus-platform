import { describe, it, expect } from 'vitest';
import {
  RawSmtpConfigSchema,
  RawSesConfigSchema,
  RawSendgridConfigSchema,
  RawMailgunConfigSchema,
  RawPostmarkConfigSchema,
} from '../src/core';

describe('RawSmtpConfigSchema', () => {
  it('should validate complete SMTP configuration', () => {
    const config = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '587',
      SMTP_USERNAME: 'user@example.com',
      SMTP_PASSWORD: 'password123',
    };

    const result = RawSmtpConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.host).toBe('smtp.example.com');
      expect(result.data.port).toBe(587);
      expect(result.data.auth.user).toBe('user@example.com');
      expect(result.data.auth.pass).toBe('password123');
      expect(result.data.secure).toBe(false);
    }
  });

  it('should coerce port number from string', () => {
    const config = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '465',
      SMTP_USERNAME: 'user',
      SMTP_PASSWORD: 'pass',
    };

    const result = RawSmtpConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.port).toBe(465);
      expect(typeof result.data.port).toBe('number');
    }
  });

  it('should transform SMTP_SECURE to boolean', () => {
    const configTrue = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '465',
      SMTP_USERNAME: 'user',
      SMTP_PASSWORD: 'pass',
      SMTP_SECURE: 'true',
    };

    const configFalse = {
      ...configTrue,
      SMTP_SECURE: 'false',
    };

    const resultTrue = RawSmtpConfigSchema.safeParse(configTrue);
    const resultFalse = RawSmtpConfigSchema.safeParse(configFalse);

    expect(resultTrue.success).toBe(true);
    expect(resultFalse.success).toBe(true);

    if (resultTrue.success) {
      expect(resultTrue.data.secure).toBe(true);
    }
    if (resultFalse.success) {
      expect(resultFalse.data.secure).toBe(false);
    }
  });

  it('should transform SMTP_POOL to boolean', () => {
    const config = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '587',
      SMTP_USERNAME: 'user',
      SMTP_PASSWORD: 'pass',
      SMTP_POOL: 'true',
    };

    const result = RawSmtpConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.pool).toBe(true);
    }
  });

  it('should parse IMAP configuration when all required fields present', () => {
    const config = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '587',
      SMTP_USERNAME: 'user',
      SMTP_PASSWORD: 'pass',
      IMAP_HOST: 'imap.example.com',
      IMAP_PORT: '993',
      IMAP_USERNAME: 'imapuser',
      IMAP_PASSWORD: 'imappass',
    };

    const result = RawSmtpConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.imap).toBeDefined();
      expect(result.data.imap?.host).toBe('imap.example.com');
      expect(result.data.imap?.port).toBe(993);
      expect(result.data.imap?.user).toBe('imapuser');
      expect(result.data.imap?.pass).toBe('imappass');
      expect(result.data.imap?.secure).toBe(true); // default
    }
  });

  it('should not include IMAP when incomplete', () => {
    const config = {
      SMTP_HOST: 'smtp.example.com',
      SMTP_PORT: '587',
      SMTP_USERNAME: 'user',
      SMTP_PASSWORD: 'pass',
      IMAP_HOST: 'imap.example.com',
      // Missing IMAP_PORT, IMAP_USERNAME, IMAP_PASSWORD
    };

    const result = RawSmtpConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.imap).toBeUndefined();
    }
  });

  it('should reject missing required SMTP fields', () => {
    const invalidConfigs = [
      { SMTP_PORT: '587', SMTP_USERNAME: 'user', SMTP_PASSWORD: 'pass' }, // Missing HOST
      { SMTP_HOST: 'smtp.example.com', SMTP_USERNAME: 'user', SMTP_PASSWORD: 'pass' }, // Missing PORT
      { SMTP_HOST: 'smtp.example.com', SMTP_PORT: '587', SMTP_PASSWORD: 'pass' }, // Missing USERNAME
      { SMTP_HOST: 'smtp.example.com', SMTP_PORT: '587', SMTP_USERNAME: 'user' }, // Missing PASSWORD
    ];

    for (const config of invalidConfigs) {
      const result = RawSmtpConfigSchema.safeParse(config);
      expect(result.success).toBe(false);
    }
  });
});

describe('RawSesConfigSchema', () => {
  it('should validate complete SES configuration', () => {
    const config = {
      SES_ACCESS_KEY_ID: 'AKIAIOSFODNN7EXAMPLE',
      SES_SECRET_ACCESS_KEY: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
      SES_REGION: 'us-east-1',
    };

    const result = RawSesConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.accessKeyId).toBe('AKIAIOSFODNN7EXAMPLE');
      expect(result.data.secretAccessKey).toBe('wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY');
      expect(result.data.region).toBe('us-east-1');
    }
  });

  it('should reject missing required fields', () => {
    const invalidConfigs = [
      { SES_SECRET_ACCESS_KEY: 'secret', SES_REGION: 'us-east-1' }, // Missing ACCESS_KEY_ID
      { SES_ACCESS_KEY_ID: 'access', SES_REGION: 'us-east-1' }, // Missing SECRET
      { SES_ACCESS_KEY_ID: 'access', SES_SECRET_ACCESS_KEY: 'secret' }, // Missing REGION
    ];

    for (const config of invalidConfigs) {
      const result = RawSesConfigSchema.safeParse(config);
      expect(result.success).toBe(false);
    }
  });

  it('should allow various AWS regions', () => {
    const regions = ['us-east-1', 'us-west-2', 'eu-west-1', 'ap-southeast-1'];

    for (const region of regions) {
      const config = {
        SES_ACCESS_KEY_ID: 'AKIAIOSFODNN7EXAMPLE',
        SES_SECRET_ACCESS_KEY: 'secretkey',
        SES_REGION: region,
      };

      const result = RawSesConfigSchema.safeParse(config);
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.data.region).toBe(region);
      }
    }
  });
});

describe('RawSendgridConfigSchema', () => {
  it('should validate SendGrid API key', () => {
    const config = {
      SENDGRID_API_KEY: 'SG.xxxxxxxxxxxxxxxxxxxx.yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy',
    };

    const result = RawSendgridConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.sendgridApiKey).toBe(config.SENDGRID_API_KEY);
    }
  });

  it('should reject missing API key', () => {
    const result = RawSendgridConfigSchema.safeParse({});
    expect(result.success).toBe(false);
  });

  it('should accept empty API key (validation is done elsewhere)', () => {
    // Note: The raw schema accepts empty strings; validation for non-empty
    // is done at the form level via ProviderAccountFormSchema
    const result = RawSendgridConfigSchema.safeParse({ SENDGRID_API_KEY: '' });
    expect(result.success).toBe(true);
  });
});

describe('RawMailgunConfigSchema', () => {
  it('should validate Mailgun API key', () => {
    const config = {
      MAILGUN_API_KEY: 'key-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx',
    };

    const result = RawMailgunConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.mailgunApiKey).toBe(config.MAILGUN_API_KEY);
    }
  });

  it('should reject missing API key', () => {
    const result = RawMailgunConfigSchema.safeParse({});
    expect(result.success).toBe(false);
  });
});

describe('RawPostmarkConfigSchema', () => {
  it('should validate Postmark tokens', () => {
    const config = {
      POSTMARK_SERVER_TOKEN: 'xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx',
      POSTMARK_ACCOUNT_TOKEN: 'yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy',
    };

    const result = RawPostmarkConfigSchema.safeParse(config);
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.postmarkServerToken).toBe(config.POSTMARK_SERVER_TOKEN);
      expect(result.data.postmarkAccountToken).toBe(config.POSTMARK_ACCOUNT_TOKEN);
    }
  });

  it('should reject missing server token', () => {
    const config = {
      POSTMARK_ACCOUNT_TOKEN: 'account-token',
    };

    const result = RawPostmarkConfigSchema.safeParse(config);
    expect(result.success).toBe(false);
  });

  it('should reject missing account token', () => {
    const config = {
      POSTMARK_SERVER_TOKEN: 'server-token',
    };

    const result = RawPostmarkConfigSchema.safeParse(config);
    expect(result.success).toBe(false);
  });
});
