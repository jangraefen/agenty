import type { Metadata } from "next";
import { Suspense } from "react";
import { AppHeader } from "@/components/app-header";
import "./globals.css";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: "Agenty",
  description: "Self-hosted AI agents for your organization.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="flex min-h-full flex-col">
        <Providers>
          <Suspense>
            <AppHeader />
          </Suspense>
          <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-8">
            {children}
          </main>
        </Providers>
      </body>
    </html>
  );
}
