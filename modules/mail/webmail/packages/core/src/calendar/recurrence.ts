import dayjs from 'dayjs';
import type { RecurrenceRule, Event } from './types';

/**
 * Day of week mapping
 */
export const DAY_MAP: Record<string, number> = {
  SU: 0,
  MO: 1,
  TU: 2,
  WE: 3,
  TH: 4,
  FR: 5,
  SA: 6,
};

export const REVERSE_DAY_MAP: Record<number, string> = {
  0: 'SU',
  1: 'MO',
  2: 'TU',
  3: 'WE',
  4: 'TH',
  5: 'FR',
  6: 'SA',
};

/**
 * Parse RRULE string to RecurrenceRule object
 */
export function parseRRule(rrule: string): RecurrenceRule | null {
  try {
    const parts = rrule.replace('RRULE:', '').split(';');
    const rule: Partial<RecurrenceRule> = { interval: 1, wkst: 'MO' };

    for (const part of parts) {
      const [key, value] = part.split('=');

      switch (key) {
        case 'FREQ':
          rule.freq = value.toLowerCase() as RecurrenceRule['freq'];
          break;
        case 'INTERVAL':
          rule.interval = parseInt(value, 10);
          break;
        case 'COUNT':
          rule.count = parseInt(value, 10);
          break;
        case 'UNTIL':
          rule.until = parseICalDate(value);
          break;
        case 'BYDAY':
          rule.byDay = value.split(',');
          break;
        case 'BYMONTHDAY':
          rule.byMonthDay = value.split(',').map((v) => parseInt(v, 10));
          break;
        case 'BYMONTH':
          rule.byMonth = value.split(',').map((v) => parseInt(v, 10));
          break;
        case 'BYSETPOS':
          rule.bySetPos = value.split(',').map((v) => parseInt(v, 10));
          break;
        case 'WKST':
          rule.wkst = value;
          break;
      }
    }

    if (!rule.freq) return null;
    return rule as RecurrenceRule;
  } catch {
    return null;
  }
}

/**
 * Convert RecurrenceRule to RRULE string
 */
export function toRRuleString(rule: RecurrenceRule): string {
  const parts: string[] = [`FREQ=${rule.freq.toUpperCase()}`];

  if (rule.interval && rule.interval !== 1) {
    parts.push(`INTERVAL=${rule.interval}`);
  }

  if (rule.count) {
    parts.push(`COUNT=${rule.count}`);
  }

  if (rule.until) {
    parts.push(`UNTIL=${formatICalDate(rule.until)}`);
  }

  if (rule.byDay?.length) {
    parts.push(`BYDAY=${rule.byDay.join(',')}`);
  }

  if (rule.byMonthDay?.length) {
    parts.push(`BYMONTHDAY=${rule.byMonthDay.join(',')}`);
  }

  if (rule.byMonth?.length) {
    parts.push(`BYMONTH=${rule.byMonth.join(',')}`);
  }

  if (rule.bySetPos?.length) {
    parts.push(`BYSETPOS=${rule.bySetPos.join(',')}`);
  }

  if (rule.wkst && rule.wkst !== 'MO') {
    parts.push(`WKST=${rule.wkst}`);
  }

  return parts.join(';');
}

/**
 * Parse iCalendar date string
 */
export function parseICalDate(value: string): Date {
  // Format: YYYYMMDD or YYYYMMDDTHHmmssZ
  if (value.length === 8) {
    return dayjs(value, 'YYYYMMDD').toDate();
  }
  return dayjs(value.replace('Z', ''), 'YYYYMMDDTHHmmss').toDate();
}

/**
 * Format date to iCalendar format
 */
export function formatICalDate(date: Date): string {
  return dayjs(date).utc().format('YYYYMMDDTHHmmss') + 'Z';
}

/**
 * Generate recurrence occurrences (simplified implementation)
 */
export function generateOccurrences(
  event: Event,
  rangeStart: Date,
  rangeEnd: Date,
  maxOccurrences = 100
): Date[] {
  if (!event.recurrenceRule) return [event.start];

  const rule = event.recurrenceRule;
  const occurrences: Date[] = [];
  let current = dayjs(event.start);
  let count = 0;

  while (count < maxOccurrences) {
    const date = current.toDate();

    // Check if beyond range end
    if (date > rangeEnd) break;

    // Check if count limit reached
    if (rule.count && count >= rule.count) break;

    // Check if beyond until date
    if (rule.until && date > rule.until) break;

    // Add if within range
    if (date >= rangeStart) {
      // Check byDay filter
      if (rule.byDay?.length) {
        const dayCode = REVERSE_DAY_MAP[current.day()];
        if (!rule.byDay.some((d) => d.includes(dayCode))) {
          current = advanceByFrequency(current, rule);
          continue;
        }
      }

      // Check byMonthDay filter
      if (rule.byMonthDay?.length && !rule.byMonthDay.includes(current.date())) {
        current = advanceByFrequency(current, rule);
        continue;
      }

      // Check byMonth filter
      if (rule.byMonth?.length && !rule.byMonth.includes(current.month() + 1)) {
        current = advanceByFrequency(current, rule);
        continue;
      }

      occurrences.push(date);
    }

    count++;
    current = advanceByFrequency(current, rule);
  }

  return occurrences;
}

/**
 * Advance date by recurrence frequency
 */
function advanceByFrequency(date: dayjs.Dayjs, rule: RecurrenceRule): dayjs.Dayjs {
  const interval = rule.interval || 1;

  switch (rule.freq) {
    case 'daily':
      return date.add(interval, 'day');
    case 'weekly':
      return date.add(interval, 'week');
    case 'monthly':
      return date.add(interval, 'month');
    case 'yearly':
      return date.add(interval, 'year');
    default:
      return date.add(1, 'day');
  }
}

/**
 * Get human-readable recurrence description
 */
export function describeRecurrence(rule: RecurrenceRule): string {
  const interval = rule.interval || 1;
  const freq = rule.freq;

  let desc = '';

  if (interval === 1) {
    desc = freq === 'daily' ? 'Daily' : freq === 'weekly' ? 'Weekly' : freq === 'monthly' ? 'Monthly' : 'Yearly';
  } else {
    const unit = freq === 'daily' ? 'days' : freq === 'weekly' ? 'weeks' : freq === 'monthly' ? 'months' : 'years';
    desc = `Every ${interval} ${unit}`;
  }

  if (rule.byDay?.length) {
    const days = rule.byDay.map((d) => {
      const dayNames: Record<string, string> = {
        SU: 'Sun',
        MO: 'Mon',
        TU: 'Tue',
        WE: 'Wed',
        TH: 'Thu',
        FR: 'Fri',
        SA: 'Sat',
      };
      return dayNames[d.replace(/[-\d]/g, '')] || d;
    });
    desc += ` on ${days.join(', ')}`;
  }

  if (rule.count) {
    desc += `, ${rule.count} times`;
  } else if (rule.until) {
    desc += `, until ${dayjs(rule.until).format('MMM D, YYYY')}`;
  }

  return desc;
}
