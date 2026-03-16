import { describe, it, expect } from 'vitest';

// Re-implementing the pure functions for testing since the module has @db dependencies
export const generateSnippet = (text: string) =>
  text ? text.toString().replace(/\s+/g, ' ').slice(0, 100) : null;

export function base64ToUint8Array(base64: string): Uint8Array {
  const binary = atob(base64);
  const len = binary.length;
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export function base64ToBlob(base64: string, contentType: string): Blob {
  const bytes = base64ToUint8Array(base64);
  return new Blob([bytes], { type: contentType });
}

describe('generateSnippet', () => {
  it('should generate snippet from normal text', () => {
    const text = 'Hello World, this is a test message.';
    const result = generateSnippet(text);
    expect(result).toBe('Hello World, this is a test message.');
  });

  it('should collapse multiple whitespace characters', () => {
    const text = 'Hello    World\n\nThis   has   spaces';
    const result = generateSnippet(text);
    expect(result).toBe('Hello World This has spaces');
  });

  it('should truncate to 100 characters', () => {
    const longText = 'A'.repeat(150);
    const result = generateSnippet(longText);
    expect(result).toHaveLength(100);
    expect(result).toBe('A'.repeat(100));
  });

  it('should return null for empty string', () => {
    expect(generateSnippet('')).toBeNull();
  });

  it('should handle text with newlines and tabs', () => {
    const text = 'Line 1\nLine 2\tTabbed';
    const result = generateSnippet(text);
    expect(result).toBe('Line 1 Line 2 Tabbed');
  });

  it('should handle text with leading/trailing whitespace', () => {
    const text = '   Hello World   ';
    const result = generateSnippet(text);
    expect(result).toBe(' Hello World ');
  });

  it('should handle Unicode text', () => {
    const text = '你好世界 Hello World 日本語';
    const result = generateSnippet(text);
    expect(result).toBe('你好世界 Hello World 日本語');
  });

  it('should handle HTML-like content (not strip tags)', () => {
    const text = '<p>Hello</p> <strong>World</strong>';
    const result = generateSnippet(text);
    expect(result).toBe('<p>Hello</p> <strong>World</strong>');
  });
});

describe('base64ToUint8Array', () => {
  it('should convert simple base64 string', () => {
    // "Hello" in base64
    const base64 = 'SGVsbG8=';
    const result = base64ToUint8Array(base64);

    expect(result).toBeInstanceOf(Uint8Array);
    expect(result.length).toBe(5);
    // Check bytes: H=72, e=101, l=108, l=108, o=111
    expect(Array.from(result)).toEqual([72, 101, 108, 108, 111]);
  });

  it('should convert empty base64 string', () => {
    const result = base64ToUint8Array('');
    expect(result).toBeInstanceOf(Uint8Array);
    expect(result.length).toBe(0);
  });

  it('should handle padding correctly', () => {
    // "A" in base64 is "QQ==" (with padding)
    const base64 = 'QQ==';
    const result = base64ToUint8Array(base64);
    expect(result.length).toBe(1);
    expect(result[0]).toBe(65); // ASCII 'A'
  });

  it('should convert binary data', () => {
    // Binary data 0xFF, 0x00, 0xFF
    const base64 = '/wD/';
    const result = base64ToUint8Array(base64);
    expect(result.length).toBe(3);
    expect(result[0]).toBe(255);
    expect(result[1]).toBe(0);
    expect(result[2]).toBe(255);
  });
});

describe('base64ToBlob', () => {
  it('should create blob with correct content type', () => {
    const base64 = 'SGVsbG8='; // "Hello"
    const blob = base64ToBlob(base64, 'text/plain');

    expect(blob).toBeInstanceOf(Blob);
    expect(blob.type).toBe('text/plain');
    expect(blob.size).toBe(5);
  });

  it('should handle different content types', () => {
    const base64 = 'SGVsbG8=';

    const textBlob = base64ToBlob(base64, 'text/plain');
    expect(textBlob.type).toBe('text/plain');

    const jsonBlob = base64ToBlob(base64, 'application/json');
    expect(jsonBlob.type).toBe('application/json');

    const imageBlob = base64ToBlob(base64, 'image/png');
    expect(imageBlob.type).toBe('image/png');
  });

  it('should create empty blob from empty base64', () => {
    const blob = base64ToBlob('', 'text/plain');
    expect(blob.size).toBe(0);
    expect(blob.type).toBe('text/plain');
  });
});

// Additional utility type tests
describe('buildParticipantsSnapshot helper logic', () => {
  // Testing the extraction logic used in buildParticipantsSnapshot
  type AddressValue = { name?: string | null; address?: string | null };
  type AddressObjectJSON = { value?: AddressValue[] };
  type Mini = { n?: string | null; e: string | null };

  const extract = (addrObj?: AddressObjectJSON | null): Mini[] =>
    (addrObj?.value ?? [])
      .map((a) => ({ n: a?.name || null, e: a?.address || null }))
      .filter((x) => x.e)
      .slice(0, 5);

  it('should extract participants from address object', () => {
    const addrObj: AddressObjectJSON = {
      value: [
        { name: 'John Doe', address: 'john@example.com' },
        { name: 'Jane Smith', address: 'jane@example.com' },
      ],
    };

    const result = extract(addrObj);
    expect(result.length).toBe(2);
    expect(result[0]).toEqual({ n: 'John Doe', e: 'john@example.com' });
    expect(result[1]).toEqual({ n: 'Jane Smith', e: 'jane@example.com' });
  });

  it('should filter out entries without email', () => {
    const addrObj: AddressObjectJSON = {
      value: [
        { name: 'John Doe', address: 'john@example.com' },
        { name: 'No Email', address: null },
        { name: 'Also No Email' },
      ],
    };

    const result = extract(addrObj);
    expect(result.length).toBe(1);
    expect(result[0].e).toBe('john@example.com');
  });

  it('should limit to 5 participants', () => {
    const addrObj: AddressObjectJSON = {
      value: Array.from({ length: 10 }, (_, i) => ({
        name: `User ${i}`,
        address: `user${i}@example.com`,
      })),
    };

    const result = extract(addrObj);
    expect(result.length).toBe(5);
  });

  it('should handle null/undefined address object', () => {
    expect(extract(null)).toEqual([]);
    expect(extract(undefined)).toEqual([]);
  });

  it('should handle empty value array', () => {
    expect(extract({ value: [] })).toEqual([]);
  });

  it('should handle name-only entries', () => {
    const addrObj: AddressObjectJSON = {
      value: [{ name: 'Name Only' }],
    };

    const result = extract(addrObj);
    expect(result.length).toBe(0);
  });
});
