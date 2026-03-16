"use client";

import { useState, useEffect, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { NotificationTemplate } from "@/lib/api/notification";
import { Bell, Plus, Pencil, Trash2 } from "lucide-react";

export default function NotificationsPage() {
  const [templates, setTemplates] = useState<NotificationTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Partial<NotificationTemplate> | null>(
    null,
  );
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/proxy/notification/admin/v1/templates");
      if (res.ok) {
        setTemplates(await res.json());
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function handleSave() {
    if (!editing) return;
    setSaving(true);
    try {
      const res = await fetch("/api/proxy/notification/admin/v1/templates", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(editing),
      });
      if (res.ok) {
        setEditing(null);
        load();
      }
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete(id: number) {
    if (!confirm("确认删除此模板？")) return;
    await fetch(`/api/proxy/notification/admin/v1/templates/${id}`, {
      method: "DELETE",
    });
    load();
  }

  const channelBadge = (ch: string) => {
    switch (ch) {
      case "email":
        return <Badge>邮件</Badge>;
      case "fcm":
        return <Badge variant="secondary">推送</Badge>;
      case "in_app":
        return <Badge variant="outline">站内</Badge>;
      default:
        return <Badge variant="outline">{ch}</Badge>;
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">通知模板</h1>
        <Button
          onClick={() =>
            setEditing({
              event_type: "",
              channel: "in_app",
              title: "",
              body: "",
              priority: "normal",
              enabled: true,
            })
          }
        >
          <Plus className="mr-1 h-4 w-4" />
          新建模板
        </Button>
      </div>

      {editing && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              {editing.id ? "编辑模板" : "新建模板"}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid gap-3 md:grid-cols-3">
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  事件类型
                </label>
                <Input
                  value={editing.event_type || ""}
                  onChange={(e) =>
                    setEditing({ ...editing, event_type: e.target.value })
                  }
                  placeholder="account.created"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">通道</label>
                <select
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                  value={editing.channel || "in_app"}
                  onChange={(e) =>
                    setEditing({
                      ...editing,
                      channel: e.target.value as "in_app" | "email" | "fcm",
                    })
                  }
                >
                  <option value="in_app">站内信</option>
                  <option value="email">邮件</option>
                  <option value="fcm">FCM 推送</option>
                </select>
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">优先级</label>
                <select
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                  value={editing.priority || "normal"}
                  onChange={(e) =>
                    setEditing({
                      ...editing,
                      priority: e.target.value as
                        | "low"
                        | "normal"
                        | "high"
                        | "urgent",
                    })
                  }
                >
                  <option value="low">Low</option>
                  <option value="normal">Normal</option>
                  <option value="high">High</option>
                  <option value="urgent">Urgent</option>
                </select>
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">标题</label>
              <Input
                value={editing.title || ""}
                onChange={(e) =>
                  setEditing({ ...editing, title: e.target.value })
                }
                placeholder="通知标题（支持 {{var}} 变量）"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">内容</label>
              <textarea
                className="flex min-h-[80px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                value={editing.body || ""}
                onChange={(e) =>
                  setEditing({ ...editing, body: e.target.value })
                }
                placeholder="通知内容（支持 {{var}} 变量）"
              />
            </div>
            <div className="flex gap-2">
              <Button onClick={handleSave} disabled={saving}>
                {saving ? "保存中..." : "保存"}
              </Button>
              <Button variant="outline" onClick={() => setEditing(null)}>
                取消
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm font-medium">
            <Bell className="h-4 w-4" />
            模板列表
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground py-4 text-center">
              加载中...
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>事件类型</TableHead>
                  <TableHead>通道</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead>优先级</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {templates.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell className="font-mono text-xs">
                      {t.event_type}
                    </TableCell>
                    <TableCell>{channelBadge(t.channel)}</TableCell>
                    <TableCell>{t.title}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{t.priority}</Badge>
                    </TableCell>
                    <TableCell>
                      {t.enabled ? (
                        <Badge>启用</Badge>
                      ) : (
                        <Badge variant="secondary">禁用</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setEditing(t)}
                        >
                          <Pencil className="h-3 w-3" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => handleDelete(t.id)}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
                {templates.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                      暂无模板
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
