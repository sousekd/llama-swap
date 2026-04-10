import { writable, derived } from "svelte/store";

export const pinRequired = writable(false);

// Use sessionStorage so lock resets when tab closes
const stored =
  typeof window !== "undefined" ? sessionStorage.getItem("pinVerified") === "true" : false;
export const pinVerified = writable(stored);
pinVerified.subscribe((v) => {
  if (typeof window !== "undefined") {
    sessionStorage.setItem("pinVerified", String(v));
  }
});

export const isLocked = derived([pinRequired, pinVerified], ([$req, $ver]) => $req && !$ver);

export async function verifyPin(pin: string): Promise<boolean> {
  try {
    const res = await fetch("/api/verify-pin", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ pin }),
    });
    if (res.ok) {
      pinVerified.set(true);
      return true;
    }
    pinVerified.set(false);
    return false;
  } catch {
    pinVerified.set(false);
    return false;
  }
}
