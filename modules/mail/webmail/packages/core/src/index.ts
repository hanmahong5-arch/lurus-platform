/**
 * @lurus/webmail-core
 *
 * Core business logic package for Lurus Webmail.
 * Provides domain models and business rules for mail, calendar, contacts, and rules.
 */

// Re-export all modules
export * from './mail';
export * from './calendar';
export * from './contacts';
export * from './rules';

// Export version
export const VERSION = '1.0.0';
