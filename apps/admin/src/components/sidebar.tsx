"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Users,
  BarChart3,
  CreditCard,
  Bell,
  Settings,
  TrendingUp,
  LayoutDashboard,
} from "lucide-react";
import { cn } from "@/lib/utils";

const navItems = [
  { href: "/users", label: "用户管理", icon: Users },
  { href: "/finance", label: "财务报表", icon: BarChart3 },
  { href: "/subscriptions", label: "订阅管理", icon: CreditCard },
  { href: "/strategies", label: "策略市场", icon: TrendingUp },
  { href: "/notifications", label: "通知模板", icon: Bell },
  { href: "/system", label: "系统状态", icon: Settings },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="flex h-screen w-56 flex-col border-r bg-card">
      <div className="flex h-14 items-center border-b px-4">
        <Link href="/" className="flex items-center gap-2 font-semibold">
          <LayoutDashboard className="h-5 w-5" />
          <span>Lurus Admin</span>
        </Link>
      </div>
      <nav className="flex-1 space-y-1 p-2">
        {navItems.map((item) => {
          const active = pathname.startsWith(item.href);
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                active
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
              )}
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
