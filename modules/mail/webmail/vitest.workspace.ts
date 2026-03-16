import { defineWorkspace } from 'vitest/config';

export default defineWorkspace([
  // Packages
  'packages/schema',
  'packages/db',
  'packages/providers',
  'packages/common',
  'packages/core',
  'packages/api-client',
  'packages/ui',

  // Apps
  'apps/web',
  'apps/worker',
]);
