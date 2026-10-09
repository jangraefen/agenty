import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agenty",
  description: "Self-hosted AI agents for your organization.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="flex min-h-full flex-col">{children}</body>
    </html>
  );
}
