import { AppSidebar, AppTopBar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";

/**
 * Pages for signed-in users. The sidebar frame (and on small screens the top bar) is in the static
 * shell, so the content doesn't move when the session-dependent parts stream in. Every page still
 * checks the session itself.
 */
export default function SignedInLayout({ children }: LayoutProps<"/">) {
  return (
    <SidebarProvider>
      <AppSidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <AppTopBar />
        <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-4 md:p-8">
          {children}
        </main>
      </div>
    </SidebarProvider>
  );
}
