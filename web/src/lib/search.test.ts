import { describe, expect, test } from "vitest";
import { parseSearch, stringifySearch } from "./search";

describe("parseSearch", () => {
  test("keeps every value a string", () => {
    expect(parseSearch("?harness=123&a=true&b=null&c=1e3&d=%7B%7D")).toEqual({
      harness: "123",
      a: "true",
      b: "null",
      c: "1e3",
      d: "{}",
    });
  });

  test("reads an empty search as no parameters", () => {
    expect(parseSearch("")).toEqual({});
    expect(parseSearch("?")).toEqual({});
  });
});

describe("stringifySearch", () => {
  test("writes strings as they are, encoded", () => {
    expect(stringifySearch({ harness: "true", status: "running", q: "a b&c" })).toBe(
      "?harness=true&status=running&q=a+b%26c",
    );
  });

  test("leaves out undefined values, and writes nothing for none", () => {
    expect(stringifySearch({ harness: undefined })).toBe("");
  });

  test("round-trips what it writes", () => {
    const search = { harness: "123", status: "failed" };
    expect(parseSearch(stringifySearch(search))).toEqual(search);
  });

  test("refuses a value that is not a string", () => {
    expect(() => stringifySearch({ page: 2 })).toThrow(/page/);
  });
});
