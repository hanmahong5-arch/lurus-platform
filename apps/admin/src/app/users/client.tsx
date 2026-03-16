"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDate } from "@/lib/utils";
import type { Account, PaginatedResponse } from "@/lib/api/identity";
import { Search, ChevronLeft, ChevronRight } from "lucide-react";

interface Props {
  data: PaginatedResponse<Account>;
  query: string;
  currentPage: number;
}

export function UsersClient({ data, query, currentPage }: Props) {
  const router = useRouter();
  const [search, setSearch] = useState(query);
  const totalPages = Math.ceil(data.total / (data.page_size || 20));

  function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    const params = new URLSearchParams();
    if (search) params.set("q", search);
    router.push(`/users?${params.toString()}`);
  }

  function goToPage(page: number) {
    const params = new URLSearchParams();
    if (search) params.set("q", search);
    params.set("page", String(page));
    router.push(`/users?${params.toString()}`);
  }

  const statusLabel = (s: number) => {
    switch (s) {
      case 1:
        return <Badge>Active</Badge>;
      case 0:
        return <Badge variant="secondary">Inactive</Badge>;
      case -1:
        return <Badge variant="destructive">Banned</Badge>;
      default:
        return <Badge variant="outline">{s}</Badge>;
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">用户管理</h1>
        <span className="text-sm text-muted-foreground">
          共 {data.total} 个用户
        </span>
      </div>

      <Card>
        <CardHeader>
          <form onSubmit={handleSearch} className="flex gap-2">
            <Input
              placeholder="搜索用户名、邮箱、Lurus ID..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="max-w-sm"
            />
            <Button type="submit" variant="outline" size="icon">
              <Search className="h-4 w-4" />
            </Button>
          </form>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Lurus ID</TableHead>
                <TableHead>用户名</TableHead>
                <TableHead>邮箱</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>注册时间</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.data?.map((account) => (
                <TableRow key={account.id}>
                  <TableCell className="font-mono text-xs">
                    {account.id}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {account.lurus_id}
                  </TableCell>
                  <TableCell>{account.display_name}</TableCell>
                  <TableCell>{account.email}</TableCell>
                  <TableCell>{statusLabel(account.status)}</TableCell>
                  <TableCell className="text-xs">
                    {formatDate(account.created_at)}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => router.push(`/users/${account.id}`)}
                    >
                      详情
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {(!data.data || data.data.length === 0) && (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-muted-foreground py-8">
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
