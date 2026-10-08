import { useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import {
  conversationQuery,
  conversationsKey,
  harnessQuery,
  runQuery,
  runsKey,
} from "@/api/queries";
import { MessageBox } from "@/components/message-box";

// A new conversation with a harness: an empty chat whose first message starts
// the conversation's first run, of the harness's latest version, and opens
// it.
export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name/new")({
  component: NewConversationPage,
});

function NewConversationPage() {
  const { workspace, name } = Route.useParams();
  const { api } = Route.useRouteContext();
  const { data: stored } = useSuspenseQuery(harnessQuery(api, workspace, name));
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [text, setText] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const start = useMutation({
    mutationFn: (input: string) =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs", {
          params: { path: { workspace } },
          body: { harness: name, input },
        }),
      ),
    onSuccess: async (run) => {
      queryClient.setQueryData(runQuery(api, workspace, run.id).queryKey, run);
      // The new run's page shows the first message at once.
      queryClient.setQueryData(conversationQuery(api, workspace, run.id).queryKey, [run]);
      void queryClient.invalidateQueries({ queryKey: conversationsKey(workspace) });
      void queryClient.invalidateQueries({ queryKey: runsKey(workspace) });
      await navigate({ to: "/w/$workspace/runs/$runId", params: { workspace, runId: run.id } });
    },
  });

  // The page is here to write the first message.
  useEffect(() => {
    box.current?.focus();
  }, []);

  return (
    <article className="mx-auto grid max-w-3xl gap-4">
      <header className="flex flex-wrap items-center gap-3 border-b pb-3">
        <h1 className="text-xl font-semibold">New conversation with {stored.harness.name}</h1>
        <span className="text-sm text-muted-foreground">Version {stored.version}</span>
        <Link
          to="/w/$workspace/harnesses/$name"
          params={{ workspace, name }}
          className="text-sm text-muted-foreground underline"
        >
          Harness
        </Link>
      </header>
      <p className="py-12 text-center text-sm text-muted-foreground">
        Write the first message to start the conversation.
      </p>
      <MessageBox
        ref={box}
        value={text}
        onChange={setText}
        onSend={(message) => start.mutate(message)}
        blocked={start.isPending}
        invalid={start.isError}
        placeholder="Write a message…"
        notes={
          start.isError
            ? [
                {
                  id: "error",
                  note: (
                    <p role="alert" className="text-sm text-destructive">
                      The conversation could not be started: {start.error.message}
                    </p>
                  ),
                },
              ]
            : []
        }
      />
    </article>
  );
}
