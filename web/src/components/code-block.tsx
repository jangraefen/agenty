import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Code or data as preformatted text, wrapped to the width it has. */
export function CodeBlock({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <pre
      className={cn(
        "rounded-md bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap wrap-anywhere",
        className,
      )}
    >
      {children}
    </pre>
  );
}
