import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useId, useState } from "react";
import { unwrap } from "@/api/client";
import { harnessQuery } from "@/api/queries";
import { Button, buttonVariants } from "@/components/ui/button";
import { formatTime } from "@/lib/format";
import { harnessYaml } from "@/lib/harness-yaml";

export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name/")({
  component: HarnessPage,
});

function HarnessPage() {
  const { workspace, name } = Route.useParams();
  const { api } = Route.useRouteContext();
  const { data: stored } = useSuspenseQuery(harnessQuery(api, workspace, name));
  const { harness } = stored;
  const id = useId();

  return (
    <article className="grid max-w-4xl gap-6">
      <Link to="/w/$workspace/harnesses" params={{ workspace }} className="text-sm underline">
        All harnesses
      </Link>
      <header className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold">{harness.name}</h1>
        <span className="text-sm text-muted-foreground">Version {stored.version}</span>
        <span className="text-sm text-muted-foreground">
          changed <time dateTime={stored.created_at}>{formatTime(stored.created_at)}</time>
        </span>
        <Link
          to="/w/$workspace/runs"
          params={{ workspace }}
          search={{ harness: harness.name }}
          className="ml-auto text-sm underline"
        >
          Runs of this harness
        </Link>
        <Link
          to="/w/$workspace/harnesses/$name/edit"
          params={{ workspace, name: harness.name }}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          Edit
        </Link>
      </header>

      <StartRun name={harness.name} />

      <section>
        <h2 className="text-sm font-semibold">Instructions</h2>
        <p className="mt-1 whitespace-pre-wrap break-words">{harness.instructions}</p>
      </section>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Model</dt>
        <dd>
          {harness.model.provider} / {harness.model.name}
        </dd>
        <dt className="text-muted-foreground">Steps at most</dt>
        <dd>{harness.limits.max_steps}</dd>
        <dt className="text-muted-foreground">Tool calls at most</dt>
        <dd>{harness.limits.max_tool_calls}</dd>
      </dl>
      <section>
        <h2 id={`${id}-tools`} className="text-sm font-semibold">
          Granted tools
        </h2>
        {harness.tools === undefined || harness.tools.length === 0 ? (
          <p className="mt-1 text-sm text-muted-foreground">None: every tool call is denied.</p>
        ) : (
          <ul aria-labelledby={`${id}-tools`} className="mt-1 flex flex-wrap gap-2">
            {harness.tools.map((tool) => (
              <li key={tool} className="rounded bg-muted px-2 py-0.5 font-mono text-xs">
                {tool}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section>
        <h2 className="text-sm font-semibold">Policy</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Rules that tighten the central policy for this harness.
        </p>
        {harness.policy === undefined || harness.policy.length === 0 ? (
          <p className="mt-1 text-sm text-muted-foreground">None.</p>
        ) : (
          harness.policy.map((module) => (
            <figure key={module.name} className="mt-2">
              <figcaption className="text-xs text-muted-foreground">{module.name}</figcaption>
              <pre className="mt-1 rounded-md bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-words">
                {module.source}
              </pre>
            </figure>
          ))
        )}
      </section>
      <section>
        <h2 id={`${id}-yaml`} className="text-sm font-semibold">
          As YAML
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          The harness file's fields, read-only; its policy is shown above.
        </p>
        <figure aria-labelledby={`${id}-yaml`} className="mt-2">
          <pre className="rounded-md bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-words">
            {harnessYaml(harness)}
          </pre>
        </figure>
      </section>
    </article>
  );
}

function StartRun({ name }: { name: string }) {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const navigate = useNavigate();
  const [input, setInput] = useState("");
  const id = useId();
  const start = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs", {
          params: { path: { workspace } },
          body: { harness: name, input },
        }),
      ),
    onSuccess: (run) =>
      navigate({ to: "/w/$workspace/runs/$runId", params: { workspace, runId: run.id } }),
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!start.isPending) {
      start.mutate();
    }
  }

  return (
    <form onSubmit={submit} className="grid gap-2 rounded-md border p-4">
      <label htmlFor={`${id}-input`} className="text-sm font-semibold">
        Input
      </label>
      <textarea
        id={`${id}-input`}
        required
        rows={3}
        value={input}
        onChange={(event) => setInput(event.target.value)}
        aria-invalid={start.isError}
        aria-describedby={start.isError ? `${id}-error` : undefined}
        className="rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring"
      />
      {start.isError && (
        <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
          The run could not be started: {start.error.message}
        </p>
      )}
      {/* Not disabled, which would drop the focus. */}
      <Button type="submit" className="justify-self-start" aria-disabled={start.isPending}>
        Start run
      </Button>
    </form>
  );
}
