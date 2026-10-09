/**
 * Vitest's setup file (vite.config.ts names it), run before every test file.
 *
 * The test/ directory holds the unit tests' shared helpers: this setup, the
 * MSW server that stands in for the API (server.ts), the response fixtures
 * shaped by the spec's types (fixtures.ts), the whole-app render
 * (render.tsx) and a fake colour-scheme query (media.ts). Tests render the
 * real App against the mocked API, so they exercise routing, loaders, the
 * query cache and the typed client together.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { server } from "./server";

// findBy queries and waitFor wait up to 5 s, not 1 s: on a loaded machine a
// render that is only slow would otherwise fail a test.
configure({ asyncUtilTimeout: 5000 });

// jsdom does not implement scrolling, which the router does on navigation.
window.scrollTo = () => undefined;

// An unhandled request fails the test, so no test passes by accident while
// asking for something the mock does not answer.
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});

// Each test starts from a clean page: no rendered app, only the default
// handlers, nothing stored (token or theme) and no theme applied.
afterEach(() => {
  cleanup();
  server.resetHandlers();
  localStorage.clear();
  delete document.documentElement.dataset.theme;
});

afterAll(() => {
  server.close();
});
