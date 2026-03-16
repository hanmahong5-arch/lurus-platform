import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // Global test configuration
    globals: true,
    environment: 'node',

    // Coverage configuration
    coverage: {
      provider: 'v8',
      reporter: ['text', 'text-summary', 'json', 'html', 'lcov'],
      reportsDirectory: './coverage',
      exclude: [
        'node_modules/**',
        'dist/**',
        '.next/**',
        '.nitro/**',
        '.output/**',
        '**/*.d.ts',
        '**/*.config.*',
        '**/coverage/**',
        '**/test/**',
        '**/__tests__/**',
        '**/*.test.*',
        '**/*.spec.*',
        'apps/docs/**',
      ],
      thresholds: {
        global: {
          statements: 80,
          branches: 70,
          functions: 80,
          lines: 80,
        },
      },
    },

    // Test file patterns
    include: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
    exclude: ['node_modules', 'dist', '.next', '.nitro', '.output'],

    // Reporter configuration
    reporters: ['default', 'html'],

    // Timeout settings
    testTimeout: 10000,
    hookTimeout: 10000,

    // Pool settings for parallel execution
    pool: 'threads',
    isolate: true,
  },
});
