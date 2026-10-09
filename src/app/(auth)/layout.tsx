import type { ReactNode } from "react";

/** Frame of the signed-out pages (sign-in): a narrow, centered column without the app header. */
export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto flex w-full max-w-md flex-1 flex-col justify-center p-8">
      {children}
    </main>
  );
}
