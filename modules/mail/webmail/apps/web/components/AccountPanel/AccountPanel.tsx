'use client';

/**
 * Account Panel Component (lurus-webmail)
 *
 * Displays Lurus identity overview inside the sidebar user dropdown:
 * LurusID, VIP badge, Lubell wallet balance, subscription status, and top-up link.
 * Fetches data from the local /api/identity/overview route.
 * Errors are silently ignored — non-critical enhancement.
 */

import { useEffect, useState } from 'react';

interface AccountOverview {
  account: { lurus_id: string; display_name: string };
  vip: { level: number; level_name: string };
  wallet: { balance: number };
  subscription: { status: string } | null;
  topup_url: string;
}

const VIP_COLORS: Record<number, string> = {
  0: 'text-muted-foreground',
  1: 'text-slate-400',
  2: 'text-amber-400',
  3: 'text-indigo-300',
  4: 'text-cyan-300',
};

const SUB_LABELS: Record<string, { cls: string; label: string }> = {
  active:  { cls: 'text-green-400', label: '订阅中' },
  grace:   { cls: 'text-orange-400', label: '宽限期' },
  expired: { cls: 'text-red-400', label: '已到期' },
};

export function AccountPanel() {
  const [overview, setOverview] = useState<AccountOverview | null>(null);

  useEffect(() => {
    fetch('/api/identity/overview')
      .then((r) => (r.ok ? r.json() : null))
      .then((d: AccountOverview | null) => setOverview(d))
      .catch(() => {/* silently degrade */});
  }, []);

  if (!overview) return null;

  const vipColor = VIP_COLORS[overview.vip.level] ?? VIP_COLORS[0]!;
  const subInfo = overview.subscription
    ? (SUB_LABELS[overview.subscription.status] ?? { cls: 'text-muted-foreground', label: overview.subscription.status })
    : { cls: 'text-muted-foreground', label: '免费' };

  const topupUrl = `${overview.topup_url}?redirect=${encodeURIComponent(window.location.href)}&from=lurus-webmail`;

  return (
    <div className="px-2 py-2 space-y-1.5 border-b border-border mb-1">
      {/* LurusID + VIP badge */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground font-mono">{overview.account.lurus_id}</span>
        <span className={`text-xs font-medium ${vipColor}`}>{overview.vip.level_name}</span>
      </div>

      {/* Lubell balance */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">鹿贝余额</span>
        <span className="text-sm font-mono tabular-nums text-amber-400">
          🦌 {overview.wallet.balance.toFixed(2)}{' '}
          <span className="text-xs text-muted-foreground">LB</span>
        </span>
      </div>

      {/* Subscription status */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">订阅</span>
        <span className={`text-xs font-medium ${subInfo.cls}`}>{subInfo.label}</span>
      </div>

      {/* Top-up link */}
      <a
        href={topupUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="block w-full text-center text-xs py-1 px-2 rounded bg-amber-500/10 text-amber-400 hover:bg-amber-500/20 transition-colors"
      >
        充值鹿贝
      </a>
    </div>
  );
}
