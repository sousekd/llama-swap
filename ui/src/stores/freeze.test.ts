import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";
import { connectionState } from "./theme";
import { fetchFreeze, setFreeze, swapsFrozen } from "./freeze";

afterEach(() => {
  connectionState.set("disconnected");
  swapsFrozen.set(false);
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

  it("refreshes after reconnecting", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ frozen: true }) }),
    );

    connectionState.set("connected");

    await vi.waitFor(() => expect(get(swapsFrozen)).toBe(true));
    expect(fetch).toHaveBeenCalledWith("/api/freeze");
  });
});