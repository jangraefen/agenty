import { describe, expect, test, vi } from "vitest";
import { Session } from "./session";

const KEY = "agenty.token";

describe("Session", () => {
  test("starts without a token when none is stored", () => {
    expect(new Session(localStorage).token).toBeNull();
  });

  test("starts with the stored token", () => {
    localStorage.setItem(KEY, "stored");

    expect(new Session(localStorage).token).toBe("stored");
  });

  test("signIn stores the token and tells subscribers", () => {
    const session = new Session(localStorage);
    const listener = vi.fn();
    session.subscribe(listener);

    session.signIn("fresh");

    expect(session.token).toBe("fresh");
    expect(localStorage.getItem(KEY)).toBe("fresh");
    expect(listener).toHaveBeenCalledOnce();
  });

  test("signOut forgets the token and tells subscribers", () => {
    localStorage.setItem(KEY, "stored");
    const session = new Session(localStorage);
    const listener = vi.fn();
    session.subscribe(listener);

    session.signOut();

    expect(session.token).toBeNull();
    expect(localStorage.getItem(KEY)).toBeNull();
    expect(listener).toHaveBeenCalledOnce();
  });

  test("an unsubscribed listener is not told", () => {
    const session = new Session(localStorage);
    const listener = vi.fn();
    const unsubscribe = session.subscribe(listener);

    unsubscribe();
    session.signIn("fresh");

    expect(listener).not.toHaveBeenCalled();
  });

  test("works in memory when the browser refuses storage", () => {
    const refusing: Storage = {
      length: 0,
      clear: () => undefined,
      key: () => null,
      getItem: () => {
        throw new DOMException("denied", "SecurityError");
      },
      setItem: () => {
        throw new DOMException("denied", "SecurityError");
      },
      removeItem: () => {
        throw new DOMException("denied", "SecurityError");
      },
    };

    const session = new Session(refusing);
    expect(session.token).toBeNull();
    session.signIn("fresh");
    expect(session.token).toBe("fresh");
    session.signOut();
    expect(session.token).toBeNull();
  });
});
