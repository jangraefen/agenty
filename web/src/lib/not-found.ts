/**
 * The bridge between API errors and the router's not-found handling: route
 * loaders wrap their ensureQueryData calls in orNotFound, so a missing
 * harness, conversation or run shows the route's not-found page rather than
 * an error. Another user's run or conversation answers 404 too, as the
 * server does not reveal that it exists.
 */
import { notFound } from "@tanstack/react-router";
import { ApiError } from "@/api/client";

/**
 * Awaits a route's data, turning the API's 404 into the route's not found;
 * every other error is rethrown, for the route's error component.
 */
export async function orNotFound<T>(load: Promise<T>): Promise<T> {
  try {
    return await load;
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      throw notFound();
    }
    throw error;
  }
}
