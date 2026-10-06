import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { App } from "./App";

test("the app shell renders the home page", async () => {
  render(<App />);

  expect(await screen.findByRole("heading", { name: "Runs" })).toBeInTheDocument();
  expect(screen.getByText("Agenty")).toBeInTheDocument();
});
