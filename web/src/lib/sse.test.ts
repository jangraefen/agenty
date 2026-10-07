import { describe, expect, test } from "vitest";
import { parseEventStream, type ServerSentEvent } from "./sse";

function streamOf(...chunks: string[]): ReadableStream<Uint8Array<ArrayBuffer>> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
}

async function collect(
  stream: ReadableStream<Uint8Array<ArrayBuffer>>,
): Promise<ServerSentEvent[]> {
  const events: ServerSentEvent[] = [];
  for await (const event of parseEventStream(stream)) {
    events.push(event);
  }
  return events;
}

describe("parseEventStream", () => {
  test.each<[string, string[], ServerSentEvent[]]>([
    [
      "gin's format, without a space",
      ["event:audit\ndata:{}\n\n"],
      [{ event: "audit", data: "{}" }],
    ],
    ["one space after the colon", ["event: audit\ndata: {}\n\n"], [{ event: "audit", data: "{}" }]],
    ["only the first space is dropped", ["data:  x\n\n"], [{ event: "message", data: " x" }]],
    ["no event name", ["data:x\n\n"], [{ event: "message", data: "x" }]],
    ["data lines joined", ["data:a\ndata:b\n\n"], [{ event: "message", data: "a\nb" }]],
    ["CRLF line ends", ["event:a\r\ndata:1\r\n\r\n"], [{ event: "a", data: "1" }]],
    ["CR line ends", ["event:a\rdata:1\r\r"], [{ event: "a", data: "1" }]],
    ["CR at a chunk's end, then CR", ["data:x\r", "\r"], [{ event: "message", data: "x" }]],
    ["CR at the stream's end", ["data:x\r\r"], [{ event: "message", data: "x" }]],
    ["comments ignored", [": keep-alive\n\ndata:x\n\n"], [{ event: "message", data: "x" }]],
    ["unknown fields ignored", ["id:7\nretry:10\ndata:x\n\n"], [{ event: "message", data: "x" }]],
    ["a field without a colon", ["data\n\n"], [{ event: "message", data: "" }]],
    ["no data, no event", ["event:a\n\n"], []],
    ["an unfinished event is dropped", ["data:x\n\ndata:y\n"], [{ event: "message", data: "x" }]],
    [
      "split across chunks anywhere",
      ["ev", "ent:au", "dit\nda", 'ta:{"a"', ":1}\n", "\nevent:b\r", "\ndata:2\r\n\r\n"],
      [
        { event: "audit", data: '{"a":1}' },
        { event: "b", data: "2" },
      ],
    ],
    [
      "the event name resets",
      ["event:a\ndata:1\n\ndata:2\n\n"],
      [
        { event: "a", data: "1" },
        { event: "message", data: "2" },
      ],
    ],
  ])("%s", async (_, chunks, want) => {
    expect(await collect(streamOf(...chunks))).toEqual(want);
  });

  test("decodes UTF-8 split across chunks", async () => {
    const bytes = new TextEncoder().encode("data:é\n\n");
    const stream = new ReadableStream<Uint8Array<ArrayBuffer>>({
      start(controller) {
        controller.enqueue(bytes.slice(0, 6));
        controller.enqueue(bytes.slice(6));
        controller.close();
      },
    });

    expect(await collect(stream)).toEqual([{ event: "message", data: "é" }]);
  });

  test("cancels the stream when the consumer stops early", async () => {
    let cancelled = false;
    const stream = new ReadableStream<Uint8Array<ArrayBuffer>>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("data:1\n\n"));
      },
      cancel() {
        cancelled = true;
      },
    });
    for await (const _ of parseEventStream(stream)) {
      break;
    }

    expect(cancelled).toBe(true);
  });

  test("passes on the stream's error", async () => {
    const stream = new ReadableStream<Uint8Array<ArrayBuffer>>({
      start(controller) {
        controller.error(new TypeError("network error"));
      },
    });

    await expect(collect(stream)).rejects.toThrow("network error");
  });
});
