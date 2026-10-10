import { LoaderCircle } from "lucide-react";

/**
 * Loading UI for every `loading.tsx`: a centred spinner that only appears after 300 ms
 * (`animate-delayed-fade-in`, CSS only), so fast navigations show nothing.
 */
export default function PageLoading() {
  return (
    <div
      className="flex flex-1 animate-delayed-fade-in items-center justify-center py-16"
      role="status"
    >
      <LoaderCircle aria-hidden="true" className="size-6 animate-spin text-muted-foreground" />
      <span className="sr-only">Loading…</span>
    </div>
  );
}
