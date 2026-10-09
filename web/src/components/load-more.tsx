import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";

/**
 * The button under every paged list: the recent chats, the audit log's runs
 * and every events table.
 */

/**
 * LoadMore loads the next page of query, while it has one, and is marked busy
 * while that page loads, which makes the button ignore further clicks.
 * onLoaded gets the data once a page has loaded, as a list may move the
 * focus to it; a page that fails leaves the focus on the button.
 *
 * It renders nothing once the query has no next page, which the query
 * knows from the last page's cursor.
 */
export function LoadMore<Page>({
  query,
  onLoaded,
  children = "Load more",
  ...props
}: Omit<ComponentProps<typeof Button>, "onClick" | "aria-disabled"> & {
  query: UseInfiniteQueryResult<InfiniteData<Page>>;
  onLoaded?: (data: InfiniteData<Page>) => void;
}) {
  if (!query.hasNextPage) {
    return null;
  }
  async function loadMore() {
    const result = await query.fetchNextPage();
    if (result.isSuccess) {
      onLoaded?.(result.data);
    }
  }
  return (
    <Button {...props} aria-disabled={query.isFetchingNextPage} onClick={() => void loadMore()}>
      {children}
    </Button>
  );
}
