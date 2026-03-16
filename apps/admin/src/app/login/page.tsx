"use client";

import { signIn } from "next-auth/react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted">
      <Card className="w-[380px]">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">Lurus Admin</CardTitle>
          <p className="text-sm text-muted-foreground">
            管理后台 — 需要管理员权限
          </p>
        </CardHeader>
        <CardContent>
          <Button
            className="w-full"
            onClick={() => signIn("zitadel", { callbackUrl: "/" })}
          >
            使用 Zitadel 登录
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
