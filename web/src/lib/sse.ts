export interface ServerSentEvent {
  event: string;
  data: string;
}

// parseEventStream reads server-sent events from a response body, as the
// HTML standard's event stream format defines them. Browsers' EventSource
// cannot send an Authorization header, so the frontend reads streams with
// fetch and this. It ignores ids and retry times: a run's stream replays
// from its start, so a client reconnects by reading it again. Stopping the
// iteration cancels the body.
export async function* parseEventStream(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ServerSentEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const parser = new Parser();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      const text = done ? decoder.decode() : decoder.decode(value, { stream: true });
      yield* parser.push(text, done);
      if (done) {
        return;
      }
    }
  } finally {
    await reader.cancel();
    reader.releaseLock();
  }
}

class Parser {
  #buffer = "";
  #event = "";
  #data: string[] = [];

  *push(text: string, end: boolean): Generator<ServerSentEvent> {
    this.#buffer += text;
    for (;;) {
      const lineEnd = /\r\n|\r|\n/.exec(this.#buffer);
      if (lineEnd === null) {
        return;
      }
      // A CR that ends the text so far may be the first half of a CRLF.
      if (lineEnd[0] === "\r" && lineEnd.index === this.#buffer.length - 1 && !end) {
        return;
      }
      const line = this.#buffer.slice(0, lineEnd.index);
      this.#buffer = this.#buffer.slice(lineEnd.index + lineEnd[0].length);
      const event = this.#line(line);
      if (event !== null) {
        yield event;
      }
    }
  }

  #line(line: string): ServerSentEvent | null {
    if (line === "") {
      const event =
        this.#data.length === 0
          ? null
          : { event: this.#event === "" ? "message" : this.#event, data: this.#data.join("\n") };
      this.#event = "";
      this.#data = [];
      return event;
    }
    const colon = line.indexOf(":");
    if (colon === 0) {
      return null; // a comment
    }
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }
    if (field === "event") {
      this.#event = value;
    } else if (field === "data") {
      this.#data.push(value);
    }
    return null;
  }
}
