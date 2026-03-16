import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';
import type { Event, EventRequest, Attendee } from './types';

dayjs.extend(utc);
dayjs.extend(timezone);

/**
 * Generate a unique iCalendar UID
 */
export function generateEventUid(domain = 'lurus.cn'): string {
  const timestamp = Date.now();
  const random = Math.random().toString(36).substring(2, 15);
  return `${timestamp}-${random}@${domain}`;
}

/**
 * Calculate event duration in minutes
 */
export function getEventDuration(event: Event): number {
  return Math.round((event.end.getTime() - event.start.getTime()) / (1000 * 60));
}

/**
 * Check if event spans multiple days
 */
export function isMultiDayEvent(event: Event): boolean {
  const start = dayjs(event.start);
  const end = dayjs(event.end);
  return !start.isSame(end, 'day');
}

/**
 * Check if event is happening now
 */
export function isEventNow(event: Event): boolean {
  const now = new Date();
  return event.start <= now && event.end > now;
}

/**
 * Check if event is in the past
 */
export function isEventPast(event: Event): boolean {
  return event.end < new Date();
}

/**
 * Check if event is in the future
 */
export function isEventFuture(event: Event): boolean {
  return event.start > new Date();
}

/**
 * Format event time range for display
 */
export function formatEventTimeRange(event: Event): string {
  const start = dayjs(event.start);
  const end = dayjs(event.end);

  if (event.allDay) {
    if (start.isSame(end.subtract(1, 'day'), 'day')) {
      return start.format('MMM D, YYYY');
    }
    return `${start.format('MMM D')} - ${end.subtract(1, 'day').format('MMM D, YYYY')}`;
  }

  if (start.isSame(end, 'day')) {
    return `${start.format('MMM D, YYYY')} ${start.format('h:mm A')} - ${end.format('h:mm A')}`;
  }

  return `${start.format('MMM D, h:mm A')} - ${end.format('MMM D, h:mm A YYYY')}`;
}

/**
 * Validate event request
 */
export function validateEventRequest(request: EventRequest): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  if (!request.summary?.trim()) {
    errors.push('Event title is required');
  }

  if (!request.start) {
    errors.push('Start time is required');
  }

  if (!request.end) {
    errors.push('End time is required');
  }

  if (request.start && request.end && request.end <= request.start) {
    errors.push('End time must be after start time');
  }

  // Validate attendees
  for (const attendee of request.attendees || []) {
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(attendee.email)) {
      errors.push(`Invalid attendee email: ${attendee.email}`);
    }
  }

  return { valid: errors.length === 0, errors };
}

/**
 * Get events for a date range
 */
export function filterEventsByDateRange(events: Event[], start: Date, end: Date): Event[] {
  return events.filter((event) => {
    // Event starts within range
    if (event.start >= start && event.start < end) return true;
    // Event ends within range
    if (event.end > start && event.end <= end) return true;
    // Event spans the entire range
    if (event.start <= start && event.end >= end) return true;
    return false;
  });
}

/**
 * Sort events by start time
 */
export function sortEventsByStartTime(events: Event[], ascending = true): Event[] {
  return [...events].sort((a, b) => {
    const diff = a.start.getTime() - b.start.getTime();
    return ascending ? diff : -diff;
  });
}

/**
 * Group events by date
 */
export function groupEventsByDate(events: Event[]): Map<string, Event[]> {
  const grouped = new Map<string, Event[]>();

  for (const event of events) {
    const dateKey = dayjs(event.start).format('YYYY-MM-DD');
    const existing = grouped.get(dateKey) || [];
    existing.push(event);
    grouped.set(dateKey, existing);
  }

  return grouped;
}

/**
 * Check for event conflicts
 */
export function findEventConflicts(event: Event, existingEvents: Event[]): Event[] {
  return existingEvents.filter((existing) => {
    // Skip same event
    if (existing.id === event.id) return false;
    // Skip if either is marked as free
    if (!event.busy || !existing.busy) return false;
    // Check for overlap
    return event.start < existing.end && event.end > existing.start;
  });
}

/**
 * Get attendee response summary
 */
export function getAttendeeResponseSummary(attendees: Attendee[]): {
  accepted: number;
  declined: number;
  tentative: number;
  pending: number;
  total: number;
} {
  const summary = {
    accepted: 0,
    declined: 0,
    tentative: 0,
    pending: 0,
    total: attendees.length,
  };

  for (const attendee of attendees) {
    if (attendee.isOrganizer) continue;
    switch (attendee.status) {
      case 'accepted':
        summary.accepted++;
        break;
      case 'declined':
        summary.declined++;
        break;
      case 'tentative':
        summary.tentative++;
        break;
      default:
        summary.pending++;
    }
  }

  return summary;
}
