import { auth } from "@/lib/auth";
import { listInvoices } from "@/lib/api/identity";
import { SubscriptionsClient } from "./client";

interface Props {
  searchParams: Promise<{ page?: string; account_id?: string }>;
}

export default async function SubscriptionsPage({ searchParams }: Props) {
  const session = await auth();
  const params = await searchParams;
  const page = Number(params.page) || 1;
  const accountId = params.account_id ? Number(params.account_id) : undefined;

  const data = await listInvoices(session!.accessToken!, {
    account_id: accountId,
    page,
    page_size: 20,
  });

  return (
    <SubscriptionsClient
      data={data}
      currentPage={page}
      accountId={accountId}
    />
  );
}
