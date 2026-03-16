/**
 * @lurus/webmail-api-client
 *
 * Unified API client package for Lurus Webmail.
 * Provides type-safe tRPC client and React Query hooks.
 */

// Core client
export * from './client';
export * from './types';

// React integration
export { ApiProvider, useApiClient } from './react';

// Hooks
export * from './hooks';
