import { notificationAdmin } from "./client";

export interface NotificationTemplate {
  id: number;
  event_type: string;
  channel: "in_app" | "email" | "fcm";
  title: string;
  body: string;
  priority: "low" | "normal" | "high" | "urgent";
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export async function listTemplates(token: string) {
  return notificationAdmin<NotificationTemplate[]>("/templates", token);
}

export async function upsertTemplate(
  token: string,
  template: Partial<NotificationTemplate>,
) {
  return notificationAdmin<NotificationTemplate>("/templates", token, {
    method: "POST",
    body: JSON.stringify(template),
  });
}

export async function deleteTemplate(token: string, id: number) {
  return notificationAdmin<void>(`/templates/${id}`, token, {
    method: "DELETE",
  });
}
