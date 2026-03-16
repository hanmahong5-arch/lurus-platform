import { auth } from "@/lib/auth";
import { getFinancialReport } from "@/lib/api/identity";
import { FinanceClient } from "./client";
import { format, subDays } from "date-fns";

interface Props {
  searchParams: Promise<{ from?: string; to?: string; group_by?: string }>;
}

export default async function FinancePage({ searchParams }: Props) {
  const session = await auth();
  const params = await searchParams;
  const to = params.to || format(new Date(), "yyyy-MM-dd");
  const from = params.from || format(subDays(new Date(), 30), "yyyy-MM-dd");
  const groupBy = (params.group_by as "day" | "month") || "day";

  const report = await getFinancialReport(
    session!.accessToken!,
    from,
    to,
    groupBy,
  );

  return (
    <FinanceClient report={report} from={from} to={to} groupBy={groupBy} />
  );
}
