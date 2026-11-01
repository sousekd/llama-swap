import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  pendingLoads,
  handleLoadModel,
  cancelLoad,
  onToggleLoad,
  loadingPercent,
  loadingMessage,
} from "./modelLoad";
import { models, handleAPIEventMessage } from "./api";
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

function progressModel(fields: Partial<Model>): Model {
  return { id: "m", state: "stopped", name: "", description: "", unlisted: false, peerID: "", ...fields };
}

afterEach(() => {
  vi.restoreAllMocks();
  pendingLoads.set({});
  models.set([]);
});

describe("loadingPercent", () => {
  it("rounds the reported fraction down to a whole percentage", () => {
    expect(loadingPercent(progressModel({ state: "starting", loadingProgress: 0.426 }))).toBe(42);
    expect(loadingPercent(progressModel({ state: "starting", loadingProgress: 0 }))).toBe(0);
    // kyojin caps progress at 0.999 until it is ready
    expect(loadingPercent(progressModel({ state: "starting", loadingProgress: 0.999 }))).toBe(99);
  });

  it("is undefined without progress or once the model is no longer starting", () => {
    expect(loadingPercent(progressModel({ state: "starting" }))).toBeUndefined();
    expect(loadingPercent(progressModel({ state: "ready", loadingProgress: 1 }))).toBeUndefined();
    expect(loadingPercent(undefined)).toBeUndefined();
  });
});

describe("loadingMessage", () => {
  it("is only shown while starting", () => {
    expect(loadingMessage(progressModel({ state: "starting", loadingMessage: "loading tensors" }))).toBe("loading tensors");
    expect(loadingMessage(progressModel({ state: "ready", loadingMessage: "loading tensors" }))).toBe("");
    expect(loadingMessage(progressModel({ state: "starting" }))).toBe("");
  });
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

  it("clears pending and stale progress when a load with progress fails", async () => {
    const deferred = Promise.withResolvers<void>();
    vi.spyOn(api, "loadModel").mockImplementation(() => deferred.promise);

    const pending = handleLoadModel("b");
    // The upstream reports loading progress through health checks while the
    // load request is still pending.
    handleAPIEventMessage(
      JSON.stringify({
        type: "modelStatus",
        data: JSON.stringify([{ id: "b", state: "starting", loadingProgress: 0.42, loadingMessage: "loading tensors" }]),
      }),
    );

    const loadError = new Error("Failed to load model: 502");
    deferred.reject(loadError);
    await expect(pending).rejects.toThrow("Failed to load model: 502");
    expect(get(pendingLoads)["b"]).toBeUndefined();
    // The stopped snapshot after the failure must not carry progress.
    handleAPIEventMessage(
      JSON.stringify({
        type: "modelStatus",
        data: JSON.stringify([{ id: "b", state: "stopped" }]),
      }),
    );
    expect(get(models)[0]).toMatchObject({ id: "b", state: "stopped" });
    expect(get(models)[0].loadingProgress).toBeUndefined();
    expect(get(models)[0].loadingMessage).toBeUndefined();
  });
});
