import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "@/App";
import { Session } from "@/auth/session";
import { Theme } from "@/theme/theme";

/**
 * Test helper that renders the real App, as main.tsx does, but on a memory
 * history, so tests start at any URL and can read where the app navigated.
 */

/**
 * Renders the whole app at path, signed in with token unless it is null, in
 * the theme stored in localStorage. Returns Testing Library's queries with
 * the history, session and theme the app runs on, and a user-event user to
 * drive it.
 */
export function renderApp(path: string, token: string | null) {
  const session = new Session(localStorage);
  if (token !== null) {
    session.signIn(token);
  }
  const history = createMemoryHistory({ initialEntries: [path] });
  const theme = new Theme(localStorage, window);
  const user = userEvent.setup();
  return {
    ...render(<App session={session} theme={theme} history={history} />),
    history,
    session,
    theme,
    user,
  };
}
