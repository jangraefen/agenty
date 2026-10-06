import type { RunStatus } from "@/api/queries";
import { cn } from "@/lib/utils";

const colors: Record<RunStatus, string> = {
  running: "bg-blue-100 text-blue-900 dark:bg-blue-950 dark:text-blue-200",
  succeeded: "bg-green-100 text-green-900 dark:bg-green-950 dark:text-green-200",
  failed: "bg-red-100 text-red-900 dark:bg-red-950 dark:text-red-200",
  cancelled: "bg-muted text-muted-foreground",
};

export function RunStatusBadge({ status }: { status: RunStatus }) {
  return (
    <span className={cn("rounded-full px-2 py-0.5 text-xs font-medium", colors[status])}>
      {status}
    </span>
  );
}
