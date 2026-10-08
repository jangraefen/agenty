# Chat app — design

Working document, not committed: its decisions go into IDEA.md and the PR descriptions.

## Intent

What the maintainer said:

- The chat should feel like other AI assistants: a sidebar with the user's recent chats; New chat picks the harness to chat with; the other menu items on the same sidebar; icons (react-icons).
- Only the user's own chats are listed; the product shifts towards personal chats.
- Chats are independent of the workspace; the workspace becomes a management UI for harnesses and, later, connectors.
- Any harness of any workspace the user is a member of can be chatted with.
- Removing a user from a workspace hides their chats there.
- Runs private to their starter, with audit in a separate view, is wanted — in a dedicated later session. This work keeps today's access model: any member may open and follow up any run of the workspace.
- Lean and clean code.

Assumptions:

- Storage and access control are unchanged: a harness lives in a workspace, every run is that workspace's. "Independent of the workspace" is a property of the UI and of two read endpoints at user level.
- Runs and Approvals stay with management, next to Harnesses.

Success: a signed-in user lands on New chat, picks a harness from any of their workspaces, sends a message, and finds the conversation at the top of the sidebar's recent chats, at a URL without a workspace. Management is in the same sidebar.

## API

`ConversationSummary {id, workspace, harness, title}` — `id` the first run's, `title` the first run's input cut to 100 characters.

- `GET /v1/conversations` (`listConversations`): the conversations the caller started (started their first run), in the workspaces they are a member of now, latest activity first (the latest run's `created_at`). Paged: `limit` 1–200, default 50; `before` an opaque cursor that `ConversationList.next` gives, encoding the last item's latest-activity time and ID, so a follow-up between pages cannot repeat a page. A malformed cursor is a 400.
- `GET /v1/conversations/{id}` (`getConversation`): the summary of one conversation, found when it is in one of the caller's workspaces, else 404. It lets a chat URL omit the workspace; nothing else needs it.

Both compute the caller's workspaces as `GetMe` does; routes outside `/v1/workspaces/` pass the membership middleware, so the handlers enforce it. Every write stays on the workspace endpoints.

Store: `Conversations(ctx, ConversationFilter)` and `FindConversation(ctx, workspaces, id)`, one sqlc query each. A partial index `runs (started_by, created_at) WHERE id = conversation_id` serves the list's filter.

Tests: store — grouping, latest-first ordering, title cut, paging stable when the cursor's conversation gets a follow-up, others' and other workspaces' conversations excluded. Server — `TestInvariant_WorkspacesAreSeparate` gains a conversation alice started in `work`, where she is not a member: not listed, not found by ID; carol's lookups are not found.

## Frontend

One shell for every signed-in page: a sidebar beside the page; below `md` the sidebar is a non-modal disclosure toggled by a menu button and closed on navigation.

Sidebar, top to bottom:

- **New chat** (`/`)
- **Recent chats**: the caller's conversations by title, the current one marked; Show more pages on.
- **Manage**: a workspace switcher when the user has more than one workspace, then Harnesses, Runs, Approvals (with its waiting count) of the managed workspace — the one in the URL under `/w/$workspace`, else the first. Hidden without workspaces.
- Footer: user, theme, Sign out.

Pages:

- `/` — New chat: a harness picker over every harness of the user's workspaces (grouped by workspace when there is more than one), preselected from `?harness=workspace/name`, else the harness of the latest recent chat, else the first; then the empty chat and its message box. Sending starts a run through the workspace's endpoint and opens the chat. No workspaces or no harnesses: a short note with a link to management.
- `/c/$conversationId` — the chat: the run page's body, lifted into a shared component, against the workspace from `getConversation`. Unknown: Conversation not found.
- `/w/$workspace/...` — management pages, unchanged but for the header, which goes. The runs list links to `/c/$conversation_id`. `/w/$workspace/runs/$runId` redirects to its conversation at that run, as approvals still link to runs. The harness page's New conversation links to `/?harness=…`; `harnesses/$name/new` is deleted. `/w/$workspace` redirects to its harnesses.

Icons: `react-icons/lu`, `aria-hidden`, beside visible labels.

Tests (Vitest, MSW): recent chats listed and marked, Show more, drawer toggle, Manage links follow the URL's workspace, picker across workspaces and its preselection, sending starts in the right workspace and opens `/c/…`, unknown conversation, run redirect, no-harness note. The Playwright smoke test starts its chat from New chat.

## Documents

IDEA.md: personal chats first and the workspace as management; the Workspaces bullet of the web frontend (no longer one workspace at a time); the screens; the two endpoints; react-icons in Technology; the deferred decision on private runs and a separate audit view.

## Delivery

1. `feat: list a user's conversations` — store, API, tests, IDEA.md's API and direction.
2. `feat: chat app with a sidebar` — frontend, react-icons, IDEA.md's screens.

Out of scope: renaming or deleting chats, search, connectors, private runs, conversations as their own table.
