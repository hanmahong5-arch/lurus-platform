"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatCurrency, formatDate } from "@/lib/utils";
import type { AccountDetail } from "@/lib/api/identity";
import { ArrowLeft, Wallet, Crown } from "lucide-react";

interface Props {
  detail: AccountDetail;
}

export function UserDetailClient({ detail }: Props) {
  const router = useRouter();
  const [adjustAmount, setAdjustAmount] = useState("");
  const [adjustDesc, setAdjustDesc] = useState("");
  const [adjusting, setAdjusting] = useState(false);
  const [message, setMessage] = useState("");

  const { account, vip, wallet, subscriptions } = detail;

  async function handleAdjust() {
    const amount = parseFloat(adjustAmount);
    if (isNaN(amount) || amount === 0) {
      setMessage("请输入有效金额");
      return;
    }
    if (!adjustDesc.trim()) {
      setMessage("请输入调整原因");
      return;
    }

    setAdjusting(true);
    setMessage("");
    try {
      const res = await fetch(
        `/api/proxy/identity/admin/v1/accounts/${account.id}/wallet/adjust`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            amount,
            description: adjustDesc.trim(),
          }),
        },
      );
      if (!res.ok) {
        const err = await res.text();
        setMessage(`调整失败: ${err}`);
      } else {
        setMessage(`调整成功！新余额: ${(await res.json()).balance}`);
        setAdjustAmount("");
        setAdjustDesc("");
        router.refresh();
      }
    } catch (err) {
      setMessage(`请求失败: ${err}`);
    } finally {
      setAdjusting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="icon" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h1 className="text-2xl font-bold">
          {account.display_name || account.email}
        </h1>
        <Badge>{account.lurus_id}</Badge>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center gap-2 pb-2">
            <CardTitle className="text-sm font-medium">账户信息</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">ID</span>
              <span className="font-mono">{account.id}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">邮箱</span>
              <span>{account.email}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Zitadel Sub</span>
              <span className="font-mono text-xs">{account.zitadel_sub}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">推荐码</span>
              <span className="font-mono">{account.aff_code || "-"}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">注册时间</span>
              <span>{formatDate(account.created_at)}</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center gap-2 pb-2">
            <Crown className="h-4 w-4 text-yellow-500" />
            <CardTitle className="text-sm font-medium">VIP</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">等级</span>
              <span>
                Lv.{vip.level} {vip.level_name}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">积分</span>
              <span>{vip.points}</span>
            </div>
            {vip.level_expires_at && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">到期</span>
                <span>{formatDate(vip.level_expires_at.time)}</span>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center gap-2 pb-2">
            <Wallet className="h-4 w-4 text-green-500" />
            <CardTitle className="text-sm font-medium">钱包</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">余额</span>
              <span className="font-semibold text-lg">
                {formatCurrency(wallet.balance)}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">冻结</span>
              <span>{formatCurrency(wallet.frozen)}</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Wallet Adjustment */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">鹿贝调整</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-end gap-3">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">
                金额（正数充值，负数扣减）
              </label>
              <Input
                type="number"
                step="0.01"
                placeholder="例如: 100 或 -50"
                value={adjustAmount}
                onChange={(e) => setAdjustAmount(e.target.value)}
                className="w-40"
              />
            </div>
            <div className="flex-1 space-y-1">
              <label className="text-xs text-muted-foreground">原因</label>
              <Input
                placeholder="调整原因"
                value={adjustDesc}
                onChange={(e) => setAdjustDesc(e.target.value)}
              />
            </div>
            <Button onClick={handleAdjust} disabled={adjusting}>
              {adjusting ? "处理中..." : "确认调整"}
            </Button>
          </div>
          {message && (
            <p className="mt-2 text-sm text-muted-foreground">{message}</p>
          )}
        </CardContent>
      </Card>

      {/* Subscriptions */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">活跃订阅</CardTitle>
        </CardHeader>
        <CardContent>
          {subscriptions && subscriptions.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>产品</TableHead>
                  <TableHead>计划</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>到期时间</TableHead>
                  <TableHead>自动续订</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {subscriptions.map((sub) => (
                  <TableRow key={sub.id}>
                    <TableCell>{sub.product_id}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{sub.plan_code}</Badge>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          sub.status === "active" ? "default" : "secondary"
                        }
                      >
                        {sub.status}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {sub.expires_at ? formatDate(sub.expires_at) : "-"}
                    </TableCell>
                    <TableCell>{sub.auto_renew ? "是" : "否"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">暂无活跃订阅</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
