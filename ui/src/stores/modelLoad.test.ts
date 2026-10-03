import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";
import { pendingLoads, handleLoadModel, cancelLoad, onToggleLoad } from "./modelLoad";
import * as api from "./api";
import type { Model } from "../lib/types";

function model(id: string, state: Model["state"]): Model {
  return {
    id,
    name: id,
    description: "",
    state,
    unlisted: false,
    peerID: "",
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  pendingLoads.set({});
});

describe("model load failures", () => {
  it("propagates load failures after clearing pending state", async () => {
    vi.spyOn(api, "loadModel").mockRejectedValue(new Error("Failed to load model: 409"));

    await expect(handleLoadModel("b")).rejects.toThrow("Failed to load model: 409");
    expect(get(pendingLoads)["b"]).toBeUndefined();
  });

  it("does not treat a cancelled load as a failure", async () => {
    const deferred = Promise.withResolvers<void>();
    vi.spyOn(api, "loadModel").mockImplementation((_model, signal) => {
      signal?.addEventListener("abort", () => deferred.resolve());
      return deferred.promise;
    });

    const pending = handleLoadModel("b");
    cancelLoad("b");
    await expect(pending).resolves.toBeUndefined();
    expect(get(pendingLoads)["b"]).toBeUndefined();
  });

  it("rethrows unload failures to the caller", async () => {
    vi.spyOn(api, "unloadSingleModel").mockRejectedValue(new Error("Failed to unload model: 500"));

    await expect(onToggleLoad(model("a", "ready"))).rejects.toThrow(
      "Failed to unload model: 500",
    );
  });
});
