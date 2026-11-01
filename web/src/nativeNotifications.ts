import type { NoteID } from "./types";

declare global {
  interface Window {
    __trackNativeNotifications?: { postMessage(message: Record<string, string>): Promise<boolean> };
  }
}

// Only external activity takes the system route. User-action outcomes stay in context.
export async function postNativeActivity(noteID: NoteID, day: string): Promise<boolean> {
  try {
    return await window.__trackNativeNotifications?.postMessage({ action: "activity", noteID, day }) === true;
  } catch {
    return false;
  }
}

export function connectNativeNotifications(open: (noteID: NoteID) => void): () => void {
  const receive = (event: Event) => {
    const noteID = (event as CustomEvent).detail?.noteID;
    if (typeof noteID === "string" && /^([a-z0-9-]+~)?[A-Za-z0-9]+$/.test(noteID)) open(noteID);
  };
  window.addEventListener("track-native-notification-open", receive);
  // Ready is a handshake after the router's listener exists; native clicks can arrive during reload.
  try { void window.__trackNativeNotifications?.postMessage({ action: "ready" }).catch(() => {}); }
  catch { /* Missing or failed bridges leave the ordinary web workspace usable. */ }
  return () => window.removeEventListener("track-native-notification-open", receive);
}
