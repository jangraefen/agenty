import { notFound } from "@tanstack/react-router";
import { ApiError } from "@/api/client";

/** Awaits a route's data, turning the API's 404 into the route's not found. */
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
