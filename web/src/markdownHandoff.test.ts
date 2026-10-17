import { afterEach, describe, expect, it, vi } from "vitest";
import { consumeMarkdownHandoff } from "./markdownHandoff";
import { ExternalMarkdownSource } from "./externalMarkdown";

afterEach(() => { vi.restoreAllMocks(); });

describe("Markdown handoff transport", () => {
  it("posts only the capability and restores scoped Unicode files and image bytes", async () => {
    const image = [0x89, 80, 78, 71, 13, 10, 26, 10];
    const fetch = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      active: "日本語 file.md", files: [
        { path: "日本語 file.md", data: btoa("# Saved") },
        { path: "docs/next.md", data: btoa("# Next") },
        { path: "images/pic.png", data: btoa(String.fromCharCode(...image)) },
      ],
    })));
    const result = await consumeMarkdownHandoff("a".repeat(64));
    expect(fetch).toHaveBeenCalledWith("/api/markdown/consume", expect.objectContaining({
      method: "POST", cache: "no-store", body: JSON.stringify({ token: "a".repeat(64) }),
    }));
    const source = new ExternalMarkdownSource(result.files);
    const current = source.documents.find((doc) => doc.path === result.active)!;
    expect(source.linkedDocument(current.path, "docs/next.md#next")?.document.path).toBe("docs/next.md");
    expect(source.linkedDocument(current.path, "../private.md")).toBeUndefined();
    const picture = result.files.find((file) => file.webkitRelativePath === "images/pic.png")!;
    // jsdom's File has no arrayBuffer implementation, so read its bytes through FileReader.
    const bytes = await new Promise<ArrayBuffer>((resolve) => {
      const reader = new FileReader(); reader.onload = () => resolve(reader.result as ArrayBuffer); reader.readAsArrayBuffer(picture);
    });
    expect([...new Uint8Array(bytes)]).toEqual(image);
    source.dispose();
  });

  it("rejects an expired or already consumed capability", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("", { status: 410 }));
    await expect(consumeMarkdownHandoff("a".repeat(64))).rejects.toThrow(/expired/);
  });
});
