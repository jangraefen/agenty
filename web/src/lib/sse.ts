import { EventSourceParserStream } from "eventsource-parser/stream";

export interface ServerSentEvent {
  event: string;
  data: string;
}

// parseEventStream reads server-sent events from a response body. Browsers'
// EventSource cannot send an Authorization header, so the frontend reads
// streams with fetch and this. It ignores ids and retry times: a run's
// stream replays from its start, so a client reconnects by reading it again.
// Stopping the iteration cancels the body. It reads the stream itself, as
// not every browser iterates a ReadableStream.
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
