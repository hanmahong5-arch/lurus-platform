import { defineProject } from 'vitest/config';
import { resolve } from 'node:path';

export default defineProject({
  test: {
    name: 'worker',
    environment: 'node',
    include: ['**/__tests__/**/*.test.ts'],
    exclude: ['node_modules', 'dist', '.nitro', '.output'],
    globals: true,
    // Worker tests may need longer timeout for async operations
    testTimeout: 30000,
  },
  resolve: {
    alias: {
      '~': resolve(__dirname, '.'),
    },
  },
});
