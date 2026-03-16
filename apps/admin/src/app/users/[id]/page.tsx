import { auth } from "@/lib/auth";
import { getAccount } from "@/lib/api/identity";
import { UserDetailClient } from "./client";

interface Props {
  params: Promise<{ id: string }>;
}

export default async function UserDetailPage({ params }: Props) {
  const session = await auth();
  const { id } = await params;
  const detail = await getAccount(session!.accessToken!, Number(id));

  return <UserDetailClient detail={detail} />;
}
