import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";
import { applyFreezeEvent, fetchFreeze, resetFreezeState, setFreeze, swapsFrozen } from "./freeze";

afterEach(() => {
  resetFreezeState();
  vi.unstubAllGlobals();
});

describe("swap freeze store", () => {
  it("fetches the server state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ frozen: true }) }),
    );

    await fetchFreeze();

    expect(fetch).toHaveBeenCalledWith("/api/freeze");
    expect(get(swapsFrozen)).toBe(true);
  });

  it("updates the server state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ frozen: true }) }),
    );

    await setFreeze(true);

    expect(fetch).toHaveBeenCalledWith("/api/freeze", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ frozen: true }),
    });
    expect(get(swapsFrozen)).toBe(true);
  });

  it("ignores a stale HTTP response that arrives after a newer SSE event", async () => {
    let finishFreeze: (value: unknown) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn().mockReturnValue(new Promise((resolve) => { finishFreeze = resolve; })),
    );

    const freezing = setFreeze(true);
    // Another client unfreezes while our PUT response is still in flight.
    applyFreezeEvent(false);
    expect(get(swapsFrozen)).toBe(false);

    finishFreeze({ ok: true, json: async () => ({ frozen: true }) });
    await freezing;
    expect(get(swapsFrozen)).toBe(false);
  });
});