import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "@/App";
import { Session } from "@/auth/session";

// Renders the whole app at path, signed in with token unless it is null.
export function renderApp(path: string, token: string | null) {
  const session = new Session(localStorage);
  if (token !== null) {
    session.signIn(token);
  }
  const history = createMemoryHistory({ initialEntries: [path] });
  const user = userEvent.setup();
  return { ...render(<App session={session} history={history} />), history, session, user };
}
