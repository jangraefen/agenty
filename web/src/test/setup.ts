import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// jsdom does not implement scrolling, which the router does on navigation.
window.scrollTo = () => undefined;

afterEach(() => {
  cleanup();
});
