import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { Button } from "./button";

test("a button marked aria-disabled ignores clicks but keeps the focus", async () => {
  const user = userEvent.setup();
  const onClick = vi.fn();
  render(
    <Button aria-disabled onClick={onClick}>
      Busy
    </Button>,
  );

  const button = screen.getByRole("button", { name: "Busy" });
  await user.click(button);

  expect(onClick).not.toHaveBeenCalled();
  expect(button).toHaveFocus();
});

test("a submit button marked aria-disabled does not submit its form", async () => {
  const user = userEvent.setup();
  const onSubmit = vi.fn((event: SubmitEvent) => event.preventDefault());
  render(
    <form onSubmit={(event) => onSubmit(event.nativeEvent as SubmitEvent)}>
      <input aria-label="Field" />
      <Button type="submit" aria-disabled>
        Send
      </Button>
    </form>,
  );

  await user.click(screen.getByRole("button", { name: "Send" }));
  await user.type(screen.getByLabelText("Field"), "x{Enter}");

  expect(onSubmit).not.toHaveBeenCalled();
});

test("a button not marked aria-disabled is clicked", async () => {
  const user = userEvent.setup();
  const onClick = vi.fn();
  render(<Button onClick={onClick}>Go</Button>);

  await user.click(screen.getByRole("button", { name: "Go" }));

  expect(onClick).toHaveBeenCalledOnce();
});
