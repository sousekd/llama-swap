import { writable } from "svelte/store";
import { apiUrl } from "../lib/apiPath";

interface FreezeState {
  frozen: boolean;
}

export const swapsFrozen = writable(false);

// Bumped by every freeze request, SSE freezeChanged event, and stream reset so
// an HTTP response that arrives after newer state cannot overwrite it.
let freezeRevision = 0;

export function applyFreezeEvent(frozen: boolean): void {
  freezeRevision++;
  swapsFrozen.set(frozen);
}

export function resetFreezeState(): void {
  freezeRevision++;
  swapsFrozen.set(false);
}

async function readFreezeResponse(response: Response): Promise<boolean> {
  if (!response.ok) {
    throw new Error(`Failed to update swap freeze: ${response.status}`);
  }
  const state = (await response.json()) as FreezeState;
  if (typeof state.frozen !== "boolean") {
    throw new Error("Invalid swap freeze response");
  }
  return state.frozen;
}

export async function fetchFreeze(): Promise<void> {
  const revision = freezeRevision;
  const frozen = await readFreezeResponse(await fetch(apiUrl("/api/freeze")));
  if (freezeRevision === revision) swapsFrozen.set(frozen);
}

export async function setFreeze(frozen: boolean): Promise<void> {
  const revision = ++freezeRevision;
  const response = await fetch(apiUrl("/api/freeze"), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ frozen }),
  });
  const next = await readFreezeResponse(response);
  if (freezeRevision === revision) swapsFrozen.set(next);
}