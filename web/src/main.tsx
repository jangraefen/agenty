import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { Session } from "./auth/session";
import "./styles.css";

const root = document.getElementById("root");
if (root === null) {
  throw new Error("index.html has no #root element");
}
createRoot(root).render(
  <StrictMode>
    <App session={new Session(localStorage)} />
  </StrictMode>,
);
