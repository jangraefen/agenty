import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Code or data as preformatted text, wrapped to the width it has: harness
 * YAML, Rego, JSON and tool results. Its content is React text, so whatever a
 * tool or model put in it shows as written, never as HTML. Wrapping, even
 * inside long words, keeps a long line from widening the page on a phone.
 */
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
