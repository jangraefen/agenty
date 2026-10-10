import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agenty",
  description: "Self-hosted AI agents for your organization.",
};

/**
 * Document only. The route groups add the page frame: (signed-in) with the sidebar, (signed-out)
 * without.
 */
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">{children}</body>
    </html>
  );
}
