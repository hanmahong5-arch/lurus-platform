import { z } from 'zod';

/**
 * Calendar schema
 */
export const CalendarSchema = z.object({
  id: z.string(),
  ownerId: z.string(),
  name: z.string(),
  description: z.string().optional(),
  color: z.string(),
  timezone: z.string(),
  isDefault: z.boolean(),
  isReadOnly: z.boolean(),
  syncEnabled: z.boolean(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Calendar = z.infer<typeof CalendarSchema>;

/**
 * Event attendee schema
 */
export const AttendeeSchema = z.object({
  email: z.string().email(),
  name: z.string().optional(),
  status: z.enum(['needs-action', 'accepted', 'declined', 'tentative']),
  role: z.enum(['required', 'optional', 'chair']),
  isOrganizer: z.boolean(),
});

export type Attendee = z.infer<typeof AttendeeSchema>;

/**
 * Recurrence rule schema (RFC 5545)
 */
export const RecurrenceRuleSchema = z.object({
  freq: z.enum(['daily', 'weekly', 'monthly', 'yearly']),
  interval: z.number().min(1).default(1),
  count: z.number().optional(),
  until: z.date().optional(),
  byDay: z.array(z.string()).optional(), // 'MO', 'TU', 'WE', etc.
  byMonthDay: z.array(z.number()).optional(),
  byMonth: z.array(z.number()).optional(),
  bySetPos: z.array(z.number()).optional(),
  wkst: z.string().default('MO'),
});

export type RecurrenceRule = z.infer<typeof RecurrenceRuleSchema>;

/**
 * Calendar event schema
 */
export const EventSchema = z.object({
  id: z.string(),
  calendarId: z.string(),
  ownerId: z.string(),
  uid: z.string(), // iCalendar UID
  summary: z.string(),
  description: z.string().optional(),
  location: z.string().optional(),
  start: z.date(),
  end: z.date(),
  allDay: z.boolean(),
  timezone: z.string(),
  recurrenceRule: RecurrenceRuleSchema.optional(),
  recurrenceId: z.date().optional(), // For exceptions
  attendees: z.array(AttendeeSchema),
  organizer: AttendeeSchema.optional(),
  status: z.enum(['confirmed', 'tentative', 'cancelled']),
  visibility: z.enum(['public', 'private', 'confidential']),
  busy: z.boolean(),
  reminders: z.array(
    z.object({
      method: z.enum(['email', 'popup']),
      minutes: z.number(),
    })
  ),
  color: z.string().optional(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Event = z.infer<typeof EventSchema>;

/**
 * Event create/update request
 */
export const EventRequestSchema = z.object({
  calendarId: z.string(),
  summary: z.string().min(1),
  description: z.string().optional(),
  location: z.string().optional(),
  start: z.date(),
  end: z.date(),
  allDay: z.boolean().optional(),
  timezone: z.string().optional(),
  recurrenceRule: RecurrenceRuleSchema.optional(),
  attendees: z.array(z.object({ email: z.string().email(), name: z.string().optional() })).optional(),
  reminders: z.array(z.object({ method: z.enum(['email', 'popup']), minutes: z.number() })).optional(),
  visibility: z.enum(['public', 'private', 'confidential']).optional(),
});

export type EventRequest = z.infer<typeof EventRequestSchema>;
