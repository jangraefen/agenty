import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { Session } from "./auth/session";
import "./styles.css";
import { Theme } from "./theme/theme";

// Applied before React renders, as the Content-Security-Policy allows no
// inline script in index.html to do it earlier.
const theme = new Theme(window);

const root = document.getElementById("root");
if (root === null) {
  throw new Error("index.html has no #root element");
}
createRoot(root).render(
  <StrictMode>
    <App session={new Session(localStorage)} theme={theme} />
  </StrictMode>,
);
