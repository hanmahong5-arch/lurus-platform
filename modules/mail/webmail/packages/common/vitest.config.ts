import { defineProject } from 'vitest/config';

export default defineProject({
  test: {
    name: 'common',
    environment: 'node',
    include: ['**/__tests__/**/*.test.ts'],
    exclude: ['node_modules', 'dist'],
    globals: true,
  },
});
