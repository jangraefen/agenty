import { createFileRoute, Link, useLocation } from "@tanstack/react-router";
import { conversationQuery } from "@/api/queries";
import { ConversationChat } from "@/components/chat";
import { NotFoundPage } from "@/components/route-states";
import { orNotFound } from "@/lib/not-found";

/**
 * A conversation's chat, at `/c/{conversation}`, where the conversation is
 * named by the ID of its first run.
 *
 * The URL names no workspace: chats are the user's across all their
 * workspaces, and `GET /v1/conversations/{id}` finds one of theirs in any of
 * them, a workspace they left included, and tells which workspace it is in.
 * The loader fetches it whole with its runs (conversationQuery) before the
 * page shows, so the run that a `#run-<id>` hash names is on the page to
 * scroll to; a 404, a conversation that does not exist or that someone else
 * started, shows ConversationNotFound.
 *
 * The page itself is components/chat.tsx's ConversationChat, which follows
 * the latest run live and replies to it.
 */
export const Route = createFileRoute("/_authed/c/$conversationId")({
  loader: async ({ context: { queryClient, api }, params }) => {
    await orNotFound(queryClient.ensureQueryData(conversationQuery(api, params.conversationId)));
  },
  component: ChatPage,
  notFoundComponent: ConversationNotFound,
});

/**
 * The chat page: passes the conversation and the run its hash names, if any,
 * to ConversationChat.
 */
function ChatPage() {
  const { conversationId } = Route.useParams();
  const hash = useLocation({ select: (location) => location.hash });
  return (
    <ConversationChat
      id={conversationId}
      focus={hash.startsWith("run-") ? hash.slice("run-".length) : undefined}
      // Keyed by the conversation, so moving to another chat starts afresh:
      // its unsent reply, answer notices and mutation states belong to the
      // chat they were made in.
      key={conversationId}
    />
  );
}

/** Shown for a conversation the API does not find for this user. */
function ConversationNotFound() {
  return (
    <NotFoundPage
      title="Conversation not found"
      detail="It does not exist, or someone else started it."
      className="mx-auto max-w-3xl"
    >
      <Link to="/" className="underline">
        Start a new chat
      </Link>
    </NotFoundPage>
  );
}
