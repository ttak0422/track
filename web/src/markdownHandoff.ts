// A handoff transfers already-read bytes. No browser-provided path is ever sent to a file-read API.
export async function consumeMarkdownHandoff(token: string): Promise<{ active: string; files: File[] }> {
  const response = await fetch("/api/markdown/consume", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }), cache: "no-store", credentials: "same-origin",
  });
  if (!response.ok) throw new Error("Markdown handoff expired or unavailable. Run :Track markdown again.");
  const snapshot = await response.json() as { active: string; files: { path: string; data: string | null }[] };
  if (typeof snapshot.active !== "string" || !Array.isArray(snapshot.files) || snapshot.files.length > 5000) {
    throw new Error("Invalid Markdown handoff.");
  }
  const files = snapshot.files.map((item) => {
    if (typeof item.path !== "string" || (item.data !== null && typeof item.data !== "string")) throw new Error("Invalid Markdown handoff.");
    const bytes = Uint8Array.from(atob(item.data ?? ""), (char) => char.charCodeAt(0));
    const file = new File([bytes], item.path.split("/").at(-1)!);
    Object.defineProperty(file, "webkitRelativePath", { value: item.path });
    return file;
  });
  return { active: snapshot.active, files };
}
