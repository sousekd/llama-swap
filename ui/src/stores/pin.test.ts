import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

describe("Activity capture PIN", () => {
  let stored: Map<string, string>;

  beforeEach(() => {
    vi.resetModules();
    stored = new Map();
    vi.stubGlobal("sessionStorage", {
      getItem: (key: string) => stored.get(key) ?? null,
      setItem: (key: string, value: string) => stored.set(key, value),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it.each([
    ["/ui/", "/api/verify-pin"],
    ["/prefix/ui/", "/prefix/api/verify-pin"],
  ])("verifies under the %s mount and unlocks the session", async (pathname, url) => {
    vi.stubGlobal("window", { location: { pathname } });
    const fetchMock = vi.fn().mockResolvedValue(new Response('{"ok":true}', { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    const pin = await import("./pin");
    pin.pinRequired.set(true);
    expect(get(pin.isLocked)).toBe(true);
    expect(await pin.verifyPin("1234")).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: '{"pin":"1234"}',
    });
    expect(get(pin.isLocked)).toBe(false);
    expect(stored.get("pinVerified")).toBe("true");
  });

  it("retains a session unlock but relocks after rejected verification", async () => {
    vi.stubGlobal("window", { location: { pathname: "/prefix/ui/" } });
    stored.set("pinVerified", "true");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"ok":false}', { status: 401 })));
    const pin = await import("./pin");
    pin.pinRequired.set(true);
    expect(get(pin.isLocked)).toBe(false);
    expect(await pin.verifyPin("0000")).toBe(false);
    expect(get(pin.isLocked)).toBe(true);
    expect(stored.get("pinVerified")).toBe("false");
    pin.pinRequired.set(false);
    expect(get(pin.isLocked)).toBe(false);
  });
});
