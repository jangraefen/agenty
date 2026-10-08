import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useLocation } from "@tanstack/react-router";
import { conversationQuery, conversationSummaryQuery, runQuery } from "@/api/queries";
import { ConversationChat } from "@/components/chat";
import { orNotFound } from "@/lib/not-found";

// A conversation's chat, in whichever of the user's workspaces it is. A
// hash run-<id> scrolls to that run.
export const Route = createFileRoute("/_authed/c/$conversationId")({
  loader: async ({ context: { queryClient, api }, params }) => {
    const summary = await orNotFound(
      queryClient.ensureQueryData(conversationSummaryQuery(api, params.conversationId)),
    );
    // The whole conversation, so the run the hash names is there to scroll
    // to; the page says so itself if it cannot be loaded.
    await Promise.all([
      queryClient.ensureQueryData(runQuery(api, summary.workspace, summary.id)),
      queryClient.prefetchQuery(conversationQuery(api, summary.workspace, summary.id)),
    ]);
  },
  component: ChatPage,
  notFoundComponent: ConversationNotFound,
});

function ChatPage() {
  const { conversationId } = Route.useParams();
  const { api } = Route.useRouteContext();
  const { data: conversation } = useSuspenseQuery(conversationSummaryQuery(api, conversationId));
  const hash = useLocation({ select: (location) => location.hash });
  return (
    <ConversationChat
      conversation={conversation}
      focus={hash.startsWith("run-") ? hash.slice("run-".length) : undefined}
      key={conversationId}
    />
  );
}

function ConversationNotFound() {
  return (
    <section className="mx-auto max-w-3xl">
      <h1 className="text-xl font-semibold">Conversation not found</h1>
      <p className="mt-2 text-muted-foreground">It does not exist, or someone else started it.</p>
      <Link to="/" className="mt-2 inline-block underline">
        Start a new chat
      </Link>
    </section>
  );
}
