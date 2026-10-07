import { CodeBlock } from "@/components/code-block";

/** A JSON value from the API, indented, as text. */
export function Json({ value }: { value: unknown }) {
  return <CodeBlock>{JSON.stringify(value, null, 2) ?? "undefined"}</CodeBlock>;
}
