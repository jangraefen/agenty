import { useInfiniteQuery, useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useEffect, useId, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import { cacheStartedRun, harnessesQuery, recentChatsQuery } from "@/api/queries";
import { MessageBox } from "@/components/message-box";

// A new chat: an empty chat with a harness of any of the user's workspaces,
// whose first message starts the conversation's first run, of the harness's
// latest version, and opens it. The harness is ?harness=<workspace>/<name>,
// else that of the latest chat, else the first, until the user picks one.
export const Route = createFileRoute("/_authed/")({
  validateSearch: (search: Record<string, unknown>): { harness?: string } =>
    typeof search.harness === "string" ? { harness: search.harness } : {},
  component: NewChatPage,
});

interface Choice {
  // ref is <workspace>/<name>; neither name holds a slash.
  ref: string;
  workspace: string;
  name: string;
}

function NewChatPage() {
  const { me, api } = Route.useRouteContext();
  const { harness: asked } = Route.useSearch();
  const lists = useQueries({
    queries: me.workspaces.map((workspace) => harnessesQuery(api, workspace)),
  });
  const recent = useInfiniteQuery(recentChatsQuery(api));
  const choices: Choice[] = me.workspaces.flatMap((workspace, i) =>
    (lists[i]?.data ?? []).map(({ harness: { name } }) => ({
      ref: `${workspace}/${name}`,
      workspace,
      name,
    })),
  );
  const latest = recent.data?.pages[0]?.conversations[0];
  const [picked, setPicked] = useState<string | null>(null);
  const chosen =
    [picked, asked, latest && `${latest.workspace}/${latest.harness}`]
      .map((ref) => choices.find((choice) => choice.ref === ref))
      .find((choice) => choice !== undefined) ?? choices[0];
  const failed = lists.find((list) => list.isError)?.error;
  // Until every list and the latest chat are known, the harness to offer
  // first is not.
  const loading = lists.some((list) => list.isPending) || recent.isPending;

  if (me.workspaces.length === 0) {
    return <Note>You are not a member of any workspace yet.</Note>;
  }
  return (
    <article className="mx-auto grid max-w-3xl gap-4">
      <h1 className="border-b pb-3 text-xl font-semibold">New chat</h1>
      {failed && (
        <p role="alert" className="text-sm text-destructive">
          Some harnesses could not be loaded: {failed.message}
        </p>
      )}
      {loading ? (
        <Note>Loading the harnesses…</Note>
      ) : chosen !== undefined ? (
        <Start
          choices={choices}
          chosen={chosen}
          onPick={setPicked}
          grouped={me.workspaces.length > 1}
        />
      ) : (
        !failed && (
          <Note>
            No harness to chat with yet.{" "}
            <Link
              to="/w/$workspace/harnesses"
              params={{ workspace: me.workspaces[0] ?? "" }}
              className="underline"
            >
              Manage harnesses
            </Link>
          </Note>
        )
      )}
    </article>
  );
}

function Note({ children }: { children: ReactNode }) {
  return <p className="py-12 text-center text-sm text-muted-foreground">{children}</p>;
}

// Start picks the harness and starts the chat with the first message.
function Start({
  choices,
  chosen,
  onPick,
  grouped,
}: {
  choices: Choice[];
  chosen: Choice;
  onPick: (ref: string) => void;
  grouped: boolean;
}) {
  const { api } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const id = useId();
  const [text, setText] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const start = useMutation({
    mutationFn: ({ workspace, name, input }: Choice & { input: string }) =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs", {
          params: { path: { workspace } },
          body: { harness: name, input },
        }),
      ),
    onSuccess: async (run, { workspace }) => {
      cacheStartedRun(queryClient, api, workspace, run);
      // The chat takes the empty one's place, so Back leads where the user
      // came from.
      await navigate({
        to: "/c/$conversationId",
        params: { conversationId: run.conversation_id },
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

  const options = (workspace: string) =>
    choices
      .filter((choice) => choice.workspace === workspace)
      .map((choice) => (
        <option key={choice.ref} value={choice.ref}>
          {choice.name}
        </option>
      ));
  const workspaces = [...new Set(choices.map((choice) => choice.workspace))];

  return (
    <>
      <div className="flex items-center gap-2">
        <label htmlFor={id} className="text-sm font-medium">
          Harness
        </label>
        <select
          id={id}
          value={chosen.ref}
          onChange={(event) => onPick(event.target.value)}
          className="h-9 rounded-md border bg-background px-2 text-sm"
        >
          {grouped
            ? workspaces.map((workspace) => (
                <optgroup key={workspace} label={workspace}>
                  {options(workspace)}
                </optgroup>
              ))
            : workspaces.flatMap(options)}
        </select>
      </div>
      <Note>Write the first message to start the conversation.</Note>
      <MessageBox
        ref={box}
        value={text}
        onChange={setText}
        onSend={(input) => start.mutate({ ...chosen, input })}
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
    </>
  );
}
