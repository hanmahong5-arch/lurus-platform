"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { formatCurrency } from "@/lib/utils";
import type { FinancialReportRow } from "@/lib/api/identity";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";

interface Props {
  report: FinancialReportRow[];
  from: string;
  to: string;
  groupBy: "day" | "month";
}

export function FinanceClient({ report, from, to, groupBy }: Props) {
  const router = useRouter();
  const [fromDate, setFromDate] = useState(from);
  const [toDate, setToDate] = useState(to);
  const [group, setGroup] = useState(groupBy);

  const totalGross = report.reduce((s, r) => s + r.gross_revenue, 0);
  const totalRefund = report.reduce((s, r) => s + r.refund_total, 0);
  const totalNet = report.reduce((s, r) => s + r.net_revenue, 0);
  const totalTx = report.reduce((s, r) => s + r.transaction_count, 0);

  function applyFilter() {
    const params = new URLSearchParams({
      from: fromDate,
      to: toDate,
      group_by: group,
    });
    router.push(`/finance?${params.toString()}`);
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">财务报表</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">筛选条件</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-end gap-3">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">开始日期</label>
              <Input
                type="date"
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">结束日期</label>
              <Input
                type="date"
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">分组</label>
              <select
                className="flex h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                value={group}
                onChange={(e) => setGroup(e.target.value as "day" | "month")}
              >
                <option value="day">按天</option>
                <option value="month">按月</option>
              </select>
            </div>
            <Button onClick={applyFilter}>查询</Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs text-muted-foreground">总收入</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{formatCurrency(totalGross)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs text-muted-foreground">退款总额</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-destructive">
              {formatCurrency(totalRefund)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs text-muted-foreground">净收入</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold text-green-600">
              {formatCurrency(totalNet)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs text-muted-foreground">交易笔数</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{totalTx}</p>
          </CardContent>
        </Card>
      </div>

      {report.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">收入趋势</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={report}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="period" fontSize={12} />
                <YAxis fontSize={12} />
                <Tooltip />
                <Legend />
                <Bar dataKey="gross_revenue" name="总收入" fill="#18181b" />
                <Bar dataKey="refund_total" name="退款" fill="#ef4444" />
                <Bar dataKey="net_revenue" name="净收入" fill="#22c55e" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">明细</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>周期</TableHead>
                <TableHead className="text-right">总收入</TableHead>
                <TableHead className="text-right">退款</TableHead>
                <TableHead className="text-right">净收入</TableHead>
                <TableHead className="text-right">笔数</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {report.map((row) => (
                <TableRow key={row.period}>
                  <TableCell>{row.period}</TableCell>
                  <TableCell className="text-right">
                    {formatCurrency(row.gross_revenue)}
                  </TableCell>
                  <TableCell className="text-right text-destructive">
                    {formatCurrency(row.refund_total)}
                  </TableCell>
                  <TableCell className="text-right text-green-600">
                    {formatCurrency(row.net_revenue)}
                  </TableCell>
                  <TableCell className="text-right">
                    {row.transaction_count}
                  </TableCell>
                </TableRow>
              ))}
              {report.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                    暂无数据
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
