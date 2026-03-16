import { z } from 'zod';

/**
 * Phone number schema
 */
export const PhoneSchema = z.object({
  type: z.enum(['home', 'work', 'mobile', 'fax', 'other']),
  value: z.string(),
  isPrimary: z.boolean().optional(),
});

export type Phone = z.infer<typeof PhoneSchema>;

/**
 * Email address schema
 */
export const EmailSchema = z.object({
  type: z.enum(['home', 'work', 'other']),
  value: z.string().email(),
  isPrimary: z.boolean().optional(),
});

export type Email = z.infer<typeof EmailSchema>;

/**
 * Address schema
 */
export const AddressSchema = z.object({
  type: z.enum(['home', 'work', 'other']),
  street: z.string().optional(),
  city: z.string().optional(),
  state: z.string().optional(),
  postalCode: z.string().optional(),
  country: z.string().optional(),
  formatted: z.string().optional(),
  isPrimary: z.boolean().optional(),
});

export type Address = z.infer<typeof AddressSchema>;

/**
 * Contact schema
 */
export const ContactSchema = z.object({
  id: z.string(),
  ownerId: z.string(),
  uid: z.string(), // vCard UID
  prefix: z.string().optional(),
  firstName: z.string().optional(),
  middleName: z.string().optional(),
  lastName: z.string().optional(),
  suffix: z.string().optional(),
  nickname: z.string().optional(),
  displayName: z.string(),
  company: z.string().optional(),
  jobTitle: z.string().optional(),
  department: z.string().optional(),
  emails: z.array(EmailSchema),
  phones: z.array(PhoneSchema),
  addresses: z.array(AddressSchema),
  birthday: z.date().optional(),
  anniversary: z.date().optional(),
  website: z.string().url().optional(),
  notes: z.string().optional(),
  photoUrl: z.string().optional(),
  labels: z.array(z.string()),
  isFavorite: z.boolean(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type Contact = z.infer<typeof ContactSchema>;

/**
 * Contact group/label schema
 */
export const ContactGroupSchema = z.object({
  id: z.string(),
  ownerId: z.string(),
  name: z.string(),
  color: z.string().optional(),
  contactCount: z.number(),
  isSystem: z.boolean(),
  createdAt: z.date(),
  updatedAt: z.date(),
});

export type ContactGroup = z.infer<typeof ContactGroupSchema>;

/**
 * Contact create/update request
 */
export const ContactRequestSchema = z.object({
  prefix: z.string().optional(),
  firstName: z.string().optional(),
  middleName: z.string().optional(),
  lastName: z.string().optional(),
  suffix: z.string().optional(),
  nickname: z.string().optional(),
  company: z.string().optional(),
  jobTitle: z.string().optional(),
  department: z.string().optional(),
  emails: z.array(z.object({ type: z.string(), value: z.string() })).optional(),
  phones: z.array(z.object({ type: z.string(), value: z.string() })).optional(),
  addresses: z
    .array(
      z.object({
        type: z.string(),
        street: z.string().optional(),
        city: z.string().optional(),
        state: z.string().optional(),
        postalCode: z.string().optional(),
        country: z.string().optional(),
      })
    )
    .optional(),
  birthday: z.date().optional(),
  anniversary: z.date().optional(),
  website: z.string().optional(),
  notes: z.string().optional(),
  labels: z.array(z.string()).optional(),
});

export type ContactRequest = z.infer<typeof ContactRequestSchema>;
