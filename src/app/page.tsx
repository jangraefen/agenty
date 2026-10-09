import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export default function HomePage() {
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col justify-center gap-6 p-8">
      <div className="flex flex-col gap-2">
        <h1 className="font-semibold text-4xl tracking-tight">Agenty</h1>
        <p className="text-lg text-muted-foreground">
          Self-hosted AI agents for your organization.
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Under construction</CardTitle>
          <CardDescription>Sign-in, agents and chat arrive in the next milestones.</CardDescription>
        </CardHeader>
        <CardContent>
          <a className={buttonVariants({ variant: "outline" })} href="/api/health">
            Check server health
          </a>
        </CardContent>
      </Card>
    </main>
  );
}
