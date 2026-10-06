import { cn } from "@/lib/utils";

/** A JSON value from the API, indented, as text. */
export function Json({ value, className }: { value: unknown; className?: string }) {
  return (
    <pre
      className={cn(
        "overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-words",
        className,
      )}
    >
      {JSON.stringify(value, null, 2) ?? "undefined"}
    </pre>
  );
}
