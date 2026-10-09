/**
 * A server-sent event parser over a fetch response body. api/run-events.ts
 * is its one user, reading a run's event stream; the parsing itself is the
 * eventsource-parser package's, this module adapts it to an async iterator.
 */
import { EventSourceParserStream } from "eventsource-parser/stream";

/** One event of a stream: its name, "message" when it gave none, and its data. */
export interface ServerSentEvent {
  event: string;
  data: string;
}

/**
 * parseEventStream reads server-sent events from a response body. Browsers'
 * EventSource cannot send an Authorization header, so the frontend reads
 * streams with fetch and this. It ignores ids and retry times: a run's
 * stream replays from its start, so a client reconnects by reading it again.
 * Stopping the iteration cancels the body. It reads the stream itself, as
 * not every browser iterates a ReadableStream.
 *
 * The body passes through a text decoder, then the parser, which splits it
 * into events however the bytes arrived in chunks.
 */
export async function* parseEventStream(
  body: ReadableStream<Uint8Array<ArrayBuffer>>,
): AsyncGenerator<ServerSentEvent> {
  const reader = body
    .pipeThrough(new TextDecoderStream())
    .pipeThrough(new EventSourceParserStream())
    .getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        return;
      }
      yield { event: value.event ?? "message", data: value.data };
    }
  } finally {
    // Ends the body if the consumer stopped early; a no-op if it ended.
    await reader.cancel();
  }
}
