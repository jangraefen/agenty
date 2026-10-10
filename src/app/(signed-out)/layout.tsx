/** Pages without a sidebar: the landing page (`/`, which sends signed-in users on) and sign-in. */
export default function SignedOutLayout({ children }: LayoutProps<"/">) {
  return (
    <div className="flex min-h-svh flex-col">
      <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-4 md:p-8">
        {children}
      </main>
    </div>
  );
}
