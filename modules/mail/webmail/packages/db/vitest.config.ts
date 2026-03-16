import { defineProject } from 'vitest/config';

export default defineProject({
  test: {
    name: 'db',
    environment: 'node',
    include: ['**/__tests__/**/*.test.ts'],
    exclude: ['node_modules', 'dist'],
    globals: true,
    // Database tests may need longer timeout
    testTimeout: 30000,
  },
});
