/**
 * The browser entry point, which index.html loads as a module script after
 * public/theme.js. It makes the two browser-bound objects, the Theme and
 * the Session, on the real localStorage and window, and renders App with
 * them into #root. Everything else is made inside App, so tests start from
 * App with their own.
 */
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { Session } from "./auth/session";
import "./styles.css";
import { Theme } from "./theme/theme";

// public/theme.js has applied the stored theme before the first paint;
// Theme re-applies the same choice and keeps it current from here on.
const theme = new Theme(localStorage, window);

const root = document.getElementById("root");
if (root === null) {
  throw new Error("index.html has no #root element");
}
createRoot(root).render(
  <StrictMode>
    <App session={new Session(localStorage)} theme={theme} />
  </StrictMode>,
);
