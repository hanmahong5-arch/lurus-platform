"use client";

import { useRouter } from "next/navigation";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatCurrency, formatDate } from "@/lib/utils";
import type { Invoice, PaginatedResponse } from "@/lib/api/identity";
import { ChevronLeft, ChevronRight } from "lucide-react";

interface Props {
  data: PaginatedResponse<Invoice>;
  currentPage: number;
  accountId?: number;
}

export function SubscriptionsClient({ data, currentPage, accountId }: Props) {
  const router = useRouter();
  const totalPages = Math.ceil(data.total / (data.page_size || 20));

  function goToPage(page: number) {
    const params = new URLSearchParams({ page: String(page) });
    if (accountId) params.set("account_id", String(accountId));
    router.push(`/subscriptions?${params.toString()}`);
  }

  const statusBadge = (status: string) => {
    switch (status) {
      case "paid":
        return <Badge>已支付</Badge>;
      case "pending":
        return <Badge variant="secondary">待支付</Badge>;
      case "refunded":
        return <Badge variant="destructive">已退款</Badge>;
      default:
        return <Badge variant="outline">{status}</Badge>;
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">订阅管理</h1>
        <span className="text-sm text-muted-foreground">
          共 {data.total} 条记录
        </span>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">账单记录</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>账单号</TableHead>
                <TableHead>账户 ID</TableHead>
                <TableHead>金额</TableHead>
                <TableHead>支付方式</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.data?.map((invoice) => (
                <TableRow key={invoice.id}>
                  <TableCell className="font-mono text-xs">
                    {invoice.invoice_no}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="link"
                      size="sm"
                      className="h-auto p-0"
                      onClick={() =>
                        router.push(`/users/${invoice.account_id}`)
                      }
                    >
                      {invoice.account_id}
                    </Button>
                  </TableCell>
                  <TableCell>{formatCurrency(invoice.amount)}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{invoice.payment_method}</Badge>
                  </TableCell>
                  <TableCell>{statusBadge(invoice.status)}</TableCell>
                  <TableCell className="text-xs">
                    {formatDate(invoice.created_at)}
                  </TableCell>
                </TableRow>
              ))}
              {(!data.data || data.data.length === 0) && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                    暂无数据
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>

          {totalPages > 1 && (
            <div className="flex items-center justify-end gap-2 pt-4">
              <Button
                variant="outline"
                size="sm"
                disabled={currentPage <= 1}
                onClick={() => goToPage(currentPage - 1)}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <span className="text-sm text-muted-foreground">
                {currentPage} / {totalPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={currentPage >= totalPages}
                onClick={() => goToPage(currentPage + 1)}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
