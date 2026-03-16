import { defineProject } from 'vitest/config';

export default defineProject({
  test: {
    name: 'providers',
    environment: 'node',
    include: ['**/__tests__/**/*.test.ts'],
    exclude: ['node_modules', 'dist'],
    globals: true,
    // Provider tests may need longer timeout for network operations
    testTimeout: 30000,
  },
});
