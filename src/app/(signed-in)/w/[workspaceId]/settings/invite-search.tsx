"use client";

import { useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { type UserSearchResponse, userSearchResponseSchema } from "@/lib/user-search";
import { inviteUserAction } from "./actions";

const DEBOUNCE_MS = 300;
const MIN_LENGTH = 2;
const MAX_LENGTH = 100;

type State =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "done"; users: UserSearchResponse["users"] }
  | { kind: "error" };

/**
 * Live user search for invitations: queries the search route as the admin types (debounced) and
 * invites through the regular server action. `revision` changes whenever members or invitations
 * change, so the shown statuses are fetched again after an invite.
 */
export function InviteSearch({
  workspaceId,
  initialQuery,
  revision,
}: {
  workspaceId: string;
  initialQuery: string;
  revision: string;
}) {
  const inputId = useId();
  const [query, setQuery] = useState(initialQuery);
  const [state, setState] = useState<State>({ kind: "idle" });
  const trimmed = query.trim();

  useEffect(() => {
    // `revision` is a dependency on purpose: a new value means the statuses may have changed.
    void revision;
    if (trimmed.length < MIN_LENGTH) {
      setState({ kind: "idle" });
      return;
    }
    const controller = new AbortController();
    const timer = setTimeout(async () => {
      // Earlier results stay visible while the next ones load.
      setState((previous) => (previous.kind === "done" ? previous : { kind: "loading" }));
      try {
        const url = `/api/workspaces/${encodeURIComponent(workspaceId)}/users?q=${encodeURIComponent(trimmed)}`;
        const response = await fetch(url, { signal: controller.signal, cache: "no-store" });
        if (!response.ok) throw new Error(`status ${response.status}`);
        const { users } = userSearchResponseSchema.parse(await response.json());
        setState({ kind: "done", users });
      } catch {
        if (!controller.signal.aborted) setState({ kind: "error" });
      }
    }, DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [workspaceId, trimmed, revision]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2">
        <Label htmlFor={inputId}>Search users</Label>
        <Input
          autoComplete="off"
          id={inputId}
          maxLength={MAX_LENGTH}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Name or email"
          type="search"
          value={query}
        />
      </div>
      <div aria-live="polite" className="flex flex-col gap-2">
        {trimmed.length > 0 && trimmed.length < MIN_LENGTH ? (
          <p className="text-muted-foreground text-sm">Type at least 2 characters.</p>
        ) : null}
        {state.kind === "loading" ? (
          <p className="text-muted-foreground text-sm">Searching…</p>
        ) : null}
        {state.kind === "error" ? (
          <p className="text-destructive text-sm" role="alert">
            Search failed. Please try again.
          </p>
        ) : null}
        {state.kind === "done" && state.users.length === 0 ? (
          <p className="text-muted-foreground text-sm">No matching users.</p>
        ) : null}
        {state.kind === "done" && state.users.length > 0 ? (
          <ul aria-label="Search results" className="flex flex-col gap-2">
            {state.users.map((found) => (
              <li className="flex min-h-8 items-center justify-between gap-3" key={found.id}>
                <span className="min-w-0 truncate">
                  <span className="font-medium">{found.name || found.email}</span>
                  <span className="text-muted-foreground"> · {found.email}</span>
                </span>
                {found.status === null ? (
                  <form action={inviteUserAction.bind(null, workspaceId)}>
                    <input name="userId" type="hidden" value={found.id} />
                    <input name="q" type="hidden" value={trimmed} />
                    <Button size="sm" type="submit">
                      Invite
                    </Button>
                  </form>
                ) : (
                  <span className="text-muted-foreground text-sm">
                    {found.status === "member" ? "Member" : "Invited"}
                  </span>
                )}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  );
}
