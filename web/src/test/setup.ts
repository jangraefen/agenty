import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { server } from "./server";

// findBy queries and waitFor wait up to 5 s, not 1 s: on a loaded machine a
// render that is only slow would otherwise fail a test.
configure({ asyncUtilTimeout: 5000 });

// jsdom does not implement scrolling, which the router does on navigation.
window.scrollTo = () => undefined;

beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
  localStorage.clear();
  delete document.documentElement.dataset.theme;
});

afterAll(() => {
  server.close();
});
