import { defineProject } from 'vitest/config';
import { resolve } from 'node:path';

export default defineProject({
  test: {
    name: 'api-client',
    environment: 'node',
    include: ['**/__tests__/**/*.test.ts'],
    exclude: ['node_modules', 'dist'],
    globals: true,
    testTimeout: 10000,
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
});
