"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { formatDate } from "@/lib/utils";
import {
  Search,
  ChevronLeft,
  ChevronRight,
  CheckCircle,
  XCircle,
  Loader2,
} from "lucide-react";
import type {
  MarketplaceStrategy,
  StrategyStatus,
  StrategiesResponse,
} from "@/lib/api/lucrum";
import { fetchStrategies, updateStrategyStatus } from "@/lib/api/lucrum";

const PAGE_SIZE = 20;

function statusBadge(status: StrategyStatus) {
  switch (status) {
    case "active":
      return <Badge>Active</Badge>;
    case "pending":
      return <Badge variant="outline">Pending</Badge>;
    case "suspended":
      return <Badge variant="destructive">Suspended</Badge>;
    case "rejected":
      return <Badge variant="secondary">Rejected</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

function priceDisplay(strategy: MarketplaceStrategy): string {
  switch (strategy.price_type) {
    case "free":
      return "Free";
    case "per_run":
      return `${strategy.price_per_run} LB/run`;
    case "monthly":
      return `${strategy.price_monthly} LB/mo`;
    default:
      return strategy.price_type;
  }
}

export function StrategiesClient() {
  const [data, setData] = useState<StrategiesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [currentPage, setCurrentPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [actionLoading, setActionLoading] = useState<number | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchStrategies({
        page: currentPage,
        page_size: PAGE_SIZE,
        status: statusFilter || undefined,
        q: search || undefined,
      });
      setData(result);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to load strategies",
      );
    } finally {
      setLoading(false);
    }
  }, [currentPage, statusFilter, search]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    setCurrentPage(1);
    loadData();
  }

  async function handleStatusChange(id: number, newStatus: StrategyStatus) {
    setActionLoading(id);
    try {
      await updateStrategyStatus(id, newStatus);
      await loadData();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to update status",
      );
    } finally {
      setActionLoading(null);
    }
  }

  const strategies = data?.data ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">策略市场</h1>
        <span className="text-sm text-muted-foreground">共 {total} 个策略</span>
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <form onSubmit={handleSearch} className="flex gap-2">
              <Input
                placeholder="搜索策略名称..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="max-w-sm"
              />
              <Button type="submit" variant="outline" size="icon">
                <Search className="h-4 w-4" />
              </Button>
            </form>
            <div className="flex gap-1">
              {(["", "active", "pending", "suspended", "rejected"] as const).map(
                (s) => (
                  <Button
                    key={s || "all"}
                    variant={statusFilter === s ? "default" : "outline"}
                    size="sm"
                    onClick={() => {
                      setStatusFilter(s);
                      setCurrentPage(1);
                    }}
                  >
                    {s === "" ? "All" : s.charAt(0).toUpperCase() + s.slice(1)}
                  </Button>
                ),
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {loading && !data ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
              <span className="ml-2 text-muted-foreground">Loading...</span>
            </div>
          ) : error && !data ? (
            <div className="py-12 text-center">
              <p className="text-sm text-destructive">{error}</p>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={loadData}
              >
                Retry
              </Button>
            </div>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>策略名称</TableHead>
                    <TableHead>定价</TableHead>
                    <TableHead>评分</TableHead>
                    <TableHead>订阅数</TableHead>
                    <TableHead>运行次数</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>发布时间</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {strategies.map((strategy) => (
                    <TableRow key={strategy.id}>
                      <TableCell className="font-mono text-xs">
                        {strategy.id}
                      </TableCell>
                      <TableCell className="max-w-[200px]">
                        <div className="font-medium truncate">
                          {strategy.title}
                        </div>
                        {strategy.description && (
                          <div className="text-xs text-muted-foreground truncate">
                            {strategy.description}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        {priceDisplay(strategy)}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {strategy.grade_score ?? "-"}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {strategy.total_subscribers}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {strategy.total_runs}
                      </TableCell>
                      <TableCell>{statusBadge(strategy.status)}</TableCell>
                      <TableCell className="text-xs">
                        {formatDate(strategy.published_at)}
                      </TableCell>
                      <TableCell>
                        <div className="flex gap-1">
                          {strategy.status !== "active" && (
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={actionLoading === strategy.id}
                              onClick={() =>
                                handleStatusChange(strategy.id, "active")
                              }
                              title="Approve"
                            >
                              {actionLoading === strategy.id ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <CheckCircle className="h-4 w-4 text-green-600" />
                              )}
                            </Button>
                          )}
                          {strategy.status !== "suspended" && (
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={actionLoading === strategy.id}
                              onClick={() =>
                                handleStatusChange(strategy.id, "suspended")
                              }
                              title="Suspend"
                            >
                              {actionLoading === strategy.id ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <XCircle className="h-4 w-4 text-red-600" />
                              )}
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                  {strategies.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={9}
                        className="py-8 text-center text-muted-foreground"
                      >
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
                    onClick={() => setCurrentPage((p) => p - 1)}
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
                    onClick={() => setCurrentPage((p) => p + 1)}
                  >
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
              )}
            </>
          )}

          {error && data && (
            <p className="mt-2 text-sm text-destructive">{error}</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
