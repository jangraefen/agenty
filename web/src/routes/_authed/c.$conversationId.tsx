import { createFileRoute, Link, useLocation } from "@tanstack/react-router";
import { conversationQuery } from "@/api/queries";
import { ConversationChat } from "@/components/chat";
import { NotFoundPage } from "@/components/route-states";
import { orNotFound } from "@/lib/not-found";

// A conversation's chat, in whichever of the user's workspaces it is, loaded
// whole with its runs, so the run a hash run-<id> names is there to scroll to.
export const Route = createFileRoute("/_authed/c/$conversationId")({
  loader: async ({ context: { queryClient, api }, params }) => {
    await orNotFound(queryClient.ensureQueryData(conversationQuery(api, params.conversationId)));
  },
  component: ChatPage,
  notFoundComponent: ConversationNotFound,
});

function ChatPage() {
  const { conversationId } = Route.useParams();
  const hash = useLocation({ select: (location) => location.hash });
  return (
    <ConversationChat
      id={conversationId}
      focus={hash.startsWith("run-") ? hash.slice("run-".length) : undefined}
      key={conversationId}
    />
  );
}

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
