/**
 * A clock for components whose text depends on the current time, such as
 * an approval request's time left (components/approval-card.tsx). The
 * hooks/ directory holds the app's own React hooks, which tie the api/ and
 * lib/ modules to component state.
 */
import { useEffect, useState } from "react";

/**
 * The current time in milliseconds, updated every intervalMs. Each caller
 * has its own interval, cleared on unmount or when intervalMs changes, so
 * only the components that show a time re-render as it passes.
 */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => {
      clearInterval(timer);
    };
  }, [intervalMs]);
  return now;
}
