import '@testing-library/jest-dom';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';

// Cleanup after each test
afterEach(() => {
  cleanup();
});

// Mock Next.js modules
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
    back: vi.fn(),
    forward: vi.fn(),
  }),
  useSearchParams: () => new URLSearchParams(),
  usePathname: () => '/',
  redirect: vi.fn(),
}));

// Mock Next.js headers
vi.mock('next/headers', () => ({
  cookies: () => ({
    get: vi.fn(),
    set: vi.fn(),
    delete: vi.fn(),
    getAll: vi.fn(() => []),
  }),
  headers: () => new Headers(),
}));

// Mock environment variables for testing
process.env.NEXT_PUBLIC_SUPABASE_URL = 'http://localhost:54321';
process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY = 'test-anon-key';
process.env.WORKER_URL = 'http://localhost:3001';

// Suppress console errors in tests (optional)
// vi.spyOn(console, 'error').mockImplementation(() => {});

// Global test utilities
declare global {
  // biome-ignore lint/style/noVar: Global test utilities
  var testUtils: {
    createMockUser: () => { id: string; email: string };
    createMockMailbox: () => { id: string; name: string; user_id: string };
  };
}

globalThis.testUtils = {
  createMockUser: () => ({
    id: 'test-user-id',
    email: 'test@example.com',
  }),
  createMockMailbox: () => ({
    id: 'test-mailbox-id',
    name: 'INBOX',
    user_id: 'test-user-id',
  }),
};
