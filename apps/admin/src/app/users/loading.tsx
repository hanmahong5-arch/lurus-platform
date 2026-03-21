import { Card, CardContent, CardHeader } from "@/components/ui/card";

function SkeletonRow({ cols }: { cols: number }) {
  return (
    <tr>
      {Array.from({ length: cols }).map((_, i) => (
        <td key={i} className="px-4 py-3">
          <div className="h-4 w-full max-w-[120px] animate-pulse rounded bg-muted" />
        </td>
      ))}
    </tr>
  );
}

export default function UsersLoading() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="h-8 w-32 animate-pulse rounded bg-muted" />
        <div className="h-5 w-24 animate-pulse rounded bg-muted" />
      </div>

      <Card>
        <CardHeader>
          <div className="flex gap-2">
            <div className="h-9 w-64 animate-pulse rounded bg-muted" />
            <div className="h-9 w-9 animate-pulse rounded bg-muted" />
          </div>
        </CardHeader>
        <CardContent>
          <table className="w-full">
            <thead>
              <tr>
                {["ID", "Lurus ID", "Username", "Email", "Status", "Created", "Action"].map(
                  (h) => (
                    <th key={h} className="px-4 py-2 text-left text-xs text-muted-foreground">
                      {h}
                    </th>
                  ),
                )}
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 8 }).map((_, i) => (
                <SkeletonRow key={i} cols={7} />
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
