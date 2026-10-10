import type { Metadata } from "next";
import { Suspense } from "react";
import { AppHeader } from "@/components/app-header";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agenty",
  description: "Self-hosted AI agents for your organization.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="flex min-h-full flex-col">
        <AppHeader />
        <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-8">
          {/* Every page reads the session, params or searchParams (request-time data), which
              Cache Components only allows inside a Suspense boundary: this one covers them all. */}
          <Suspense>{children}</Suspense>
        </main>
      </body>
    </html>
  );
}
