import { writable } from "svelte/store";
import { connectionState } from "./theme";

interface FreezeState {
  frozen: boolean;
}

export const swapsFrozen = writable(false);

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
  swapsFrozen.set(await readFreezeResponse(await fetch("/api/freeze")));
}

export async function setFreeze(frozen: boolean): Promise<void> {
  const response = await fetch("/api/freeze", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ frozen }),
  });
  swapsFrozen.set(await readFreezeResponse(response));
}

connectionState.subscribe((status) => {
  if (status === "connected") {
    void fetchFreeze().catch((error) => console.error(error));
  }
});