import { CodeBlock } from "@/components/code-block";

/**
 * A JSON value from the API, indented, as text: tool arguments, results and
 * audit details, which come from models and tools. JSON.stringify answers
 * undefined for undefined itself, shown as the word.
 */
export function Json({ value }: { value: unknown }) {
  return <CodeBlock>{JSON.stringify(value, null, 2) ?? "undefined"}</CodeBlock>;
}
