import { defineProject } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

export default defineProject({
  plugins: [react()],
  test: {
    name: 'web',
    environment: 'jsdom',
    include: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
    exclude: ['node_modules', 'dist', '.next'],
    globals: true,
    setupFiles: ['./__tests__/setup.ts'],
    // React component tests may need longer timeout
    testTimeout: 15000,
    // CSS handling
    css: false,
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, '.'),
    },
  },
});
