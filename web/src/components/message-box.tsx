import { type FormEvent, type KeyboardEvent, type ReactNode, type Ref, useId } from "react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

// The box a chat's messages are written in, at the bottom of the page: Enter
// sends, Shift+Enter starts a new line. Notes about the box, such as why it
// cannot send yet or why sending failed, go below it and describe it.
export function MessageBox({
  ref,
  value,
  onChange,
  onSend,
  blocked,
  invalid,
  placeholder,
  notes,
}: {
  ref?: Ref<HTMLTextAreaElement>;
  value: string;
  onChange: (value: string) => void;
  // onSend is called with a message that is not blank, unless blocked.
  onSend: (message: string) => void;
  blocked: boolean;
  invalid: boolean;
  placeholder: string;
  // notes are the notes below the box, by name; a false one is not shown.
  notes: Record<string, ReactNode | false>;
}) {
  const id = useId();

  function send() {
    if (!blocked && value.trim() !== "") {
      onSend(value);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    send();
  }

  function keyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      send();
    }
  }

  const shown = Object.entries(notes).filter(([, note]) => note !== false);
  const described = shown.map(([name]) => `${id}-${name}`).join(" ");

  return (
    <form onSubmit={submit} className="sticky bottom-0 grid gap-2 border-t bg-background pt-3 pb-4">
      <Label htmlFor={`${id}-message`} className="sr-only">
        Message
      </Label>
      <div className="flex items-end gap-2">
        <Textarea
          ref={ref}
          id={`${id}-message`}
          rows={2}
          placeholder={placeholder}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={keyDown}
          aria-invalid={invalid}
          aria-describedby={described === "" ? undefined : described}
          className="min-w-0 flex-1 resize-none rounded-xl"
        />
        <Button type="submit" className="shrink-0" aria-disabled={blocked}>
          Send
        </Button>
      </div>
      {shown.map(([name, note]) => (
        <div key={name} id={`${id}-${name}`}>
          {note}
        </div>
      ))}
    </form>
  );
}
