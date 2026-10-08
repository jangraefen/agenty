import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "@/App";
import { Session } from "@/auth/session";
import { Theme } from "@/theme/theme";

// Renders the whole app at path, signed in with token unless it is null, in
// the theme stored in localStorage.
export function renderApp(path: string, token: string | null) {
  const session = new Session(localStorage);
  if (token !== null) {
    session.signIn(token);
  }
  const history = createMemoryHistory({ initialEntries: [path] });
  const theme = new Theme(window);
  const user = userEvent.setup();
  return {
    ...render(<App session={session} theme={theme} history={history} />),
    history,
    session,
    theme,
    user,
  };
}
