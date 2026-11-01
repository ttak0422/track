// Optional-feature diagnostics stay in the console without repeating on every render/poll.
const reported = new Set<string>();
export function reportRecoverable(feature: string, error: unknown): void {
  const key = `${feature}:${String(error).slice(0, 200)}`;
  if (reported.has(key)) return;
  if (reported.size >= 128) reported.delete(reported.values().next().value!);
  reported.add(key);
  console.warn(`Track: ${feature} unavailable`, error);
}

// Boot can fail before React exists. Keep prerendered content and offer one explicit action.
export function showBootFailure(error: unknown): void {
 reportRecoverable("application startup",error);
 if(document.getElementById("track-boot-recovery"))return;
 const notice=document.createElement("div");notice.id="track-boot-recovery";notice.setAttribute("role","status");
 const text=document.createElement("p");text.className="muted";text.textContent="Interactive features could not start. Existing content is kept.";
 const retry=document.createElement("button");retry.type="button";retry.textContent="Reload application";retry.className="note-recovery-action";retry.onclick=()=>window.location.reload();
 notice.append(text,retry);(document.getElementById("root")??document.body).append(notice);
}
