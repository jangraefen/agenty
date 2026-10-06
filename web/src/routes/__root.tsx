import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import type { RouterContext } from "@/router";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
});

function Root() {
  return (
    <div className="min-h-screen bg-white text-slate-900">
      <header className="border-b border-slate-200 px-6 py-3">
        <span className="font-semibold">Agenty</span>
      </header>
      <main className="px-6 py-4">
        <Outlet />
      </main>
    </div>
  );
}
