import { useInfiniteQuery, useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useEffect, useId, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import { cacheStartedRun, harnessesQuery, recentChatsQuery } from "@/api/queries";
import { MessageBox } from "@/components/message-box";

/**
 * The new chat page, at `/`: the landing page of a signed-in user and the
 * sidebar's New chat.
 *
 * It is an empty chat with a picker of every harness in every workspace of
 * the user's. The first message starts the conversation's first run, of the
 * harness's latest version (`POST /v1/workspaces/{ws}/runs`), and opens the
 * new conversation at `/c/{conversation}`, where the chat goes on.
 *
 * The harness offered first is `?harness=<workspace>/<name>`, as a harness's
 * page links it, else that of the user's latest chat, else the first, until
 * the user picks one. To know them, the page loads the harness list of each
 * of the user's workspaces (harnessesQuery, one query each) and the recent
 * chats (recentChatsQuery, which the sidebar shares from the same cache).
 */
export const Route = createFileRoute("/_authed/")({
  validateSearch: (search: Record<string, unknown>): { harness?: string } =>
    typeof search.harness === "string" ? { harness: search.harness } : {},
  component: NewChatPage,
});

/** A harness the user can start a chat with. */
interface Choice {
  // ref is <workspace>/<name>; neither name holds a slash.
  ref: string;
  workspace: string;
  name: string;
}

/**
 * The page: works out the harnesses to offer and which comes first, then
 * hands them to Start. A workspace whose list fails leaves the others
 * offered, with an alert naming the failure.
 */
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
  // The recent chats come latest first, so the first is the latest.
  const latest = recent.data?.pages[0]?.conversations[0];
  const [picked, setPicked] = useState<string | null>(null);
  // The first of these that is still a harness the user can use: one that
  // was removed, or is in a workspace they left, is skipped.
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

/** A muted line in the middle of the page, for what there is to say instead of a chat. */
function Note({ children }: { children: ReactNode }) {
  return <p className="py-12 text-center text-sm text-muted-foreground">{children}</p>;
}

/**
 * Start picks the harness and starts the chat with the first message. The
 * picker groups the harnesses by workspace when the user has more than one,
 * as two workspaces may each have a harness of the same name.
 *
 * Once the run starts, cacheStartedRun puts it, in a new conversation, into
 * the query cache, so the chat page shows it at once without waiting for its
 * loader's request, and refreshes the recent chats.
 */
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
