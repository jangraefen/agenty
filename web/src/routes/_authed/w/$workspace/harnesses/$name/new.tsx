import { useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import { cacheStartedRun, harnessQuery } from "@/api/queries";
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
      cacheStartedRun(queryClient, api, workspace, run);
      // The chat takes the empty one's place, so Back leads to the harness.
      await navigate({
        to: "/w/$workspace/runs/$runId",
        params: { workspace, runId: run.id },
        replace: true,
        // The conversation goes on in the chat's own box.
        state: { focusMessage: true },
      });
    },
  });

  // The page is here to write the first message.
  useEffect(() => {
    box.current?.focus();
  }, []);

  return (
    <article className="mx-auto grid max-w-3xl gap-4">
      <Link
        to="/w/$workspace/harnesses/$name"
        params={{ workspace, name }}
        className="text-sm underline"
      >
        Back to {name}
      </Link>
      <h1 className="border-b pb-3 text-xl font-semibold">
        New conversation with {stored.harness.name}
      </h1>
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
        notes={{
          error: start.isError && (
            <p role="alert" className="text-sm text-destructive">
              The conversation could not be started: {start.error.message}
            </p>
          ),
        }}
      />
    </article>
  );
}
