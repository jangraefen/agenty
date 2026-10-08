import { useRouterState } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { LuMenu, LuX } from "react-icons/lu";
import { cn } from "@/lib/utils";

// Shell lays a page out beside the sidebar. Below md the sidebar is hidden
// behind a menu button, and leaving the page or Escape hides it again. It
// does not hold the focus: the page stays usable while it shows.
export function Shell({ sidebar, children }: { sidebar: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const href = useRouterState({ select: (state) => state.location.href });
  const [shownAt, setShownAt] = useState(href);
  if (shownAt !== href) {
    setShownAt(href);
    setOpen(false);
  }
  const toggle = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) {
      return;
    }
    const close = (event: KeyboardEvent) => {
      // A menu in the sidebar that Escape closes has handled it.
      if (event.key === "Escape" && !event.defaultPrevented) {
        setOpen(false);
        toggle.current?.focus();
      }
    };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [open]);
  const Icon = open ? LuX : LuMenu;

  return (
    <div className="flex min-h-dvh">
      <aside
        id="sidebar"
        className={cn(
          "h-dvh w-64 shrink-0 flex-col gap-4 overflow-x-hidden overflow-y-auto border-r bg-background p-3 pt-14 md:sticky md:top-0 md:flex md:pt-3",
          open ? "fixed inset-y-0 left-0 z-20 flex shadow-lg md:shadow-none" : "hidden",
        )}
      >
        {sidebar}
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center gap-2 border-b px-4 py-2 md:hidden">
          <button
            ref={toggle}
            type="button"
            className="relative z-30 rounded-md p-1.5 hover:bg-accent"
            aria-label={open ? "Close sidebar" : "Open sidebar"}
            aria-expanded={open}
            aria-controls="sidebar"
            onClick={() => setOpen(!open)}
          >
            <Icon aria-hidden="true" className="size-5" />
          </button>
          <span className="font-semibold">Agenty</span>
        </div>
        <main className="flex-1 px-6 py-4">{children}</main>
      </div>
    </div>
  );
}
