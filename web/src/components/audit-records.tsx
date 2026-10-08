import type { Schemas } from "@/api/client";
import { Json } from "@/components/json";
import { formatTime } from "@/lib/format";

const eventLabels: Record<Schemas["AuditEvent"], string> = {
  decision: "Policy decision",
  approval: "Approval",
  result: "Result",
};

// AuditRecords lists what the gateway recorded for a run's tool calls, in
// the order it recorded them. Arguments and results come from the model and
// tools, so they are shown as text only.
export function AuditRecords({ records }: { records: Schemas["AuditRecord"][] }) {
  if (records.length === 0) {
    return <p className="text-sm text-muted-foreground">No tool calls.</p>;
  }
  return (
    <ol aria-label="Audit records" className="grid gap-3">
      {records.map((record, index) => (
        // Records have no id of their own; their order never changes.
        // biome-ignore lint/suspicious/noArrayIndexKey: see above.
        <li key={index} className="grid gap-1 border-l-2 pl-3 text-sm">
          <div className="flex flex-wrap items-baseline gap-2">
            <time dateTime={record.recorded_at} className="text-muted-foreground">
              {formatTime(record.recorded_at)}
            </time>
            <span>{eventLabels[record.event]}</span>
            <code className="font-semibold">{record.tool}</code>
            <span className="rounded bg-muted px-1.5 text-xs">{record.decision}</span>
            {record.approver !== undefined && <span>by {record.approver}</span>}
          </div>
          {record.reason !== undefined && record.reason !== "" && <p>{record.reason}</p>}
          {record.event === "decision" && record.args !== undefined && <Json value={record.args} />}
          {record.result !== undefined && <Json value={record.result} />}
          {record.error !== undefined && record.error !== "" && (
            <p className="text-destructive">{record.error}</p>
          )}
        </li>
      ))}
    </ol>
  );
}
