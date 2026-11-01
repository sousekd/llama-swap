import { get, writable } from "svelte/store";
import { persistentStore } from "./persistent";

export type PlaygroundTab = "chat" | "images" | "speech" | "audio" | "realtime" | "rerank" | "concurrency";

export const playgroundTabs: { id: PlaygroundTab; label: string }[] = [
  { id: "chat", label: "Chat" },
  { id: "images", label: "Images" },
  { id: "speech", label: "Speech" },
  { id: "audio", label: "Transcription" },
  { id: "realtime", label: "Realtime" },
  { id: "rerank", label: "Rerank" },
  { id: "concurrency", label: "Load Test" },
];

export const selectedPlaygroundTab = persistentStore<PlaygroundTab>("playground-selected-tab", "chat");

export const selectedPlaygroundModel = persistentStore<string>("playground-selected-model", "");

/**
 * A model the Playground should show in a fresh conversation.
 *
 * Deliberately transient: the intent belongs to the navigation that set it,
 * and a reload afterwards starts an ordinary visit that restores history.
 */
const pendingModelIntent = writable<string | null>(null);

/** Subscribe to the pending intent. The Playground consumes it on arrival. */
export const playgroundIntent = { subscribe: pendingModelIntent.subscribe };

/** Reads and clears the pending intent. Chat consumes it once per navigation. */
export function takePlaygroundIntent(): string | null {
  let pending: string | null = null;
  pendingModelIntent.update((value) => {
    pending = value;
    return null;
  });
  return pending;
}

/**
 * Open the Playground on the Chat tab with `modelId` selected, replacing the
 * conversation on screen instead of restoring the last one. Opening a model is
 * an explicit choice, so the saved conversation's model must not win.
 *
 * The model travels in the intent rather than being written to
 * `selectedPlaygroundModel` here: Chat saves the conversation on screen while
 * starting the new one, and it must do that before the model changes, or the
 * saved history would record the new model on the old conversation.
 */
export function showModelInPlayground(modelId: string): void {
  selectedPlaygroundTab.set("chat");
  pendingModelIntent.set(modelId);
}

/**
 * Drop a stored tab that no longer exists.
 *
 * "docs" was a tab here until Help became its own page. A browser that last
 * left the Playground on it would select a tab with nothing behind it: every
 * panel hidden, and a header reading "Playground /".
 */
export function resetUnknownPlaygroundTab(): void {
  if (!playgroundTabs.some((tab) => tab.id === get(selectedPlaygroundTab))) {
    selectedPlaygroundTab.set(playgroundTabs[0].id);
  }
}

resetUnknownPlaygroundTab();
