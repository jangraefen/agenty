"use client";

import { PanelLeftIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { useSidebar } from "@/components/ui/sidebar";

/**
 * Small screens only: opens the sidebar as a fly-in, and closes it after a navigation (Escape and
 * outside clicks are handled by the sheet).
 */
export function MobileTopBar() {
  const { setOpenMobile } = useSidebar();
  const pathname = usePathname();
  const shownPathname = useRef(pathname);
  useEffect(() => {
    if (shownPathname.current === pathname) return;
    shownPathname.current = pathname;
    setOpenMobile(false);
  }, [pathname, setOpenMobile]);

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
      <Link className="font-semibold" href="/">
        Agenty
      </Link>
    </header>
  );
}
