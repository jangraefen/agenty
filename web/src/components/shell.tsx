/**
 * The page layout of a signed-in user: the sidebar beside the page's main
 * content. The authed layout (routes/_authed.tsx) wraps every signed-in page
 * in it, with AppSidebar as the sidebar.
 *
 * On a wide screen (md and up) the sidebar is always shown, sticky beside
 * the page. On a narrow one a bar with a menu button shows and hides it over
 * the page.
 */
import { useRouterState } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { LuMenu, LuX } from "react-icons/lu";
import { cn } from "@/lib/utils";

/**
 * Shell lays a page out beside the sidebar. Below md the sidebar is hidden
 * behind a menu button, and leaving the page or Escape hides it again. It
 * does not hold the focus: the page stays usable while it shows.
 *
 * Whether it shows is one piece of state, open, which the CSS reads at
 * narrow widths only, so a wide screen ignores it.
 */
export function Shell({ sidebar, children }: { sidebar: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const href = useRouterState({ select: (state) => state.location.href });
  // Following a link, such as a chat in the sidebar, closes the sidebar, so
  // the page it opened shows. Reset while rendering, rather than in an
  // effect, so the page never shows a frame with the sidebar still open.
  const [shownAt, setShownAt] = useState(href);
  if (shownAt !== href) {
    setShownAt(href);
    setOpen(false);
  }
  const toggle = useRef<HTMLButtonElement>(null);
  // Escape closes the open sidebar and returns the focus to the button that
  // opened it; the listener is there only while the sidebar is open.
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
