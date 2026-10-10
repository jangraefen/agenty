"use client";

import { PanelLeftIcon } from "lucide-react";
import { usePathname } from "next/navigation";
import { type ReactNode, Suspense, useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { useSidebar } from "@/components/ui/sidebar";

/**
 * Small screens only: opens the sidebar as a fly-in, and closes it after a navigation (Escape and
 * outside clicks are handled by the sheet). Part of the static shell; `children` is the app name.
 */
export function MobileTopBar({ children }: { children: ReactNode }) {
  const { setOpenMobile } = useSidebar();

  return (
    <header className="sticky top-0 z-10 flex items-center gap-2 border-b bg-background px-2 py-2 md:hidden">
      <Button
        aria-label="Open sidebar"
        onClick={() => setOpenMobile(true)}
        size="icon"
        variant="ghost"
      >
        <PanelLeftIcon aria-hidden="true" />
      </Button>
      {children}
      {/* usePathname() suspends while prerendering a page with an unknown param (/w/[id]). */}
      <Suspense>
        <CloseOnNavigation />
      </Suspense>
    </header>
  );
}

function CloseOnNavigation() {
  const { setOpenMobile } = useSidebar();
  const pathname = usePathname();
  const shownPathname = useRef(pathname);
  useEffect(() => {
    if (shownPathname.current === pathname) return;
    shownPathname.current = pathname;
    setOpenMobile(false);
  }, [pathname, setOpenMobile]);
  return null;
}
