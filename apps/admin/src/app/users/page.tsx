import { auth } from "@/lib/auth";
import { listAccounts } from "@/lib/api/identity";
import { UsersClient } from "./client";

interface Props {
  searchParams: Promise<{ q?: string; page?: string }>;
}

export default async function UsersPage({ searchParams }: Props) {
  const session = await auth();
  const params = await searchParams;
  const page = Number(params.page) || 1;
  const q = params.q || "";

  const data = await listAccounts(session!.accessToken!, {
    q: q || undefined,
    page,
    page_size: 20,
  });

  return <UsersClient data={data} query={q} currentPage={page} />;
}
