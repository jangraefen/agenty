import { describe, expect, test, vi } from "vitest";
import { StoredValue } from "./stored-value";

const KEY = "agenty.test";

// Reads a stored value as upper case, or "NONE" for none.
const upper = (stored: string | null) => stored?.toUpperCase() ?? "NONE";

describe("StoredValue", () => {
  test("starts with the stored value, parsed", () => {
    localStorage.setItem(KEY, "kept");

    expect(new StoredValue(localStorage, KEY, upper).value).toBe("KEPT");
    expect(new StoredValue(localStorage, "agenty.other", upper).value).toBe("NONE");
  });

  test("set stores the value, null removing it, and tells subscribers", () => {
    const value = new StoredValue<string | null>(localStorage, KEY, (stored) => stored);
    const listener = vi.fn();
    value.subscribe(listener);

    value.set("fresh");
    expect(value.value).toBe("fresh");
    expect(localStorage.getItem(KEY)).toBe("fresh");

    value.set(null);
    expect(value.value).toBeNull();
    expect(localStorage.getItem(KEY)).toBeNull();
    expect(listener).toHaveBeenCalledTimes(2);
  });

  test("an unsubscribed listener is not told", () => {
    const value = new StoredValue(localStorage, KEY, upper);
    const listener = vi.fn();
    // Bound, so it can be passed on as it is.
    const { subscribe } = value;
    const unsubscribe = subscribe(listener);

    unsubscribe();
    value.set("FRESH");

    expect(listener).not.toHaveBeenCalled();
  });

  test("follows the changes of other tabs to its key alone", () => {
    const value = new StoredValue(localStorage, KEY, upper);
    const listener = vi.fn();
    value.subscribe(listener);
    const stop = value.followOtherTabs(window);

    localStorage.setItem(KEY, "other");
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "other" }));
    expect(value.value).toBe("OTHER");
    // The same value again, and another key, change nothing.
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "other" }));
    window.dispatchEvent(new StorageEvent("storage", { key: "unrelated", newValue: "x" }));
    expect(listener).toHaveBeenCalledOnce();
    // A null key means another tab cleared the storage.
    localStorage.clear();
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
    expect(value.value).toBe("NONE");
    expect(listener).toHaveBeenCalledTimes(2);

    stop();
    localStorage.setItem(KEY, "later");
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "later" }));
    expect(value.value).toBe("NONE");
  });

  test("lives in memory when the browser refuses storage", () => {
    const refuse = () => {
      throw new DOMException("denied", "SecurityError");
    };
    const refusing: Storage = {
      length: 0,
      clear: () => undefined,
      key: () => null,
      getItem: refuse,
      setItem: refuse,
      removeItem: refuse,
    };

    const value = new StoredValue<string | null>(refusing, KEY, (stored) => stored);
    expect(value.value).toBeNull();
    value.set("fresh");
    expect(value.value).toBe("fresh");
    value.set(null);
    expect(value.value).toBeNull();
  });
});
