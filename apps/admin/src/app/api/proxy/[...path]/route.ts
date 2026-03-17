import { NextRequest, NextResponse } from "next/server";
import { auth } from "@/lib/auth";

const SERVICE_MAP: Record<string, string> = {
  identity:
    process.env.IDENTITY_SERVICE_URL ||
    "http://platform-core.lurus-platform.svc.cluster.local:18104",
  notification:
    process.env.NOTIFICATION_SERVICE_URL ||
    "http://lurus-notification.lurus-system.svc.cluster.local:18900",
  gushen:
    process.env.GUSHEN_SERVICE_URL ||
    "http://gushen-web.ai-qtrd.svc.cluster.local:3000",
};

export async function GET(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, await params);
}

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, await params);
}

export async function PUT(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, await params);
}

export async function PATCH(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, await params);
}

export async function DELETE(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, await params);
}

async function proxy(req: NextRequest, params: { path: string[] }) {
  const session = await auth();
  if (!session?.accessToken) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const roles = session.user?.roles || [];
  if (!roles.includes("admin")) {
    return NextResponse.json({ error: "Forbidden" }, { status: 403 });
  }

  const [service, ...rest] = params.path;
  const baseUrl = SERVICE_MAP[service];
  if (!baseUrl) {
    return NextResponse.json(
      { error: `Unknown service: ${service}` },
      { status: 400 },
    );
  }

  const targetPath = "/" + rest.join("/");
  const url = new URL(targetPath, baseUrl);

  // Forward query params
  req.nextUrl.searchParams.forEach((value, key) => {
    url.searchParams.set(key, value);
  });

  const headers: Record<string, string> = {
    Authorization: `Bearer ${session.accessToken}`,
    "Content-Type": req.headers.get("content-type") || "application/json",
  };

  const fetchOpts: RequestInit = {
    method: req.method,
    headers,
  };

  if (req.method !== "GET" && req.method !== "HEAD") {
    fetchOpts.body = await req.text();
  }

  try {
    const upstream = await fetch(url.toString(), fetchOpts);
    const body = await upstream.text();

    return new NextResponse(body, {
      status: upstream.status,
      headers: {
        "Content-Type":
          upstream.headers.get("content-type") || "application/json",
      },
    });
  } catch (err) {
    return NextResponse.json(
      { error: `Proxy error: ${err}` },
      { status: 502 },
    );
  }
}
