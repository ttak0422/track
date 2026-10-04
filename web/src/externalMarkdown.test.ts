import { afterEach, describe, expect, it, vi } from "vitest";
import { ExternalMarkdownSource, MAX_IMAGE_BYTES, MAX_MARKDOWN_BYTES, resolveLocalTarget } from "./externalMarkdown";

function file(path: string, text: string | Uint8Array = "# Hello"): File {
  const bytes = typeof text === "string" ? new TextEncoder().encode(text) : text;
  const item = new File([bytes as BlobPart], path.split("/").at(-1)!);
  Object.defineProperty(item, "webkitRelativePath", { value: path.includes("/") ? path : "" });
  Object.defineProperty(item, "arrayBuffer", { value: vi.fn(async () => bytes.buffer) });
  return item;
}

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("selected external Markdown sources", () => {
  it("opens the nearest README and only exposes Markdown documents", async () => {
    const readme = file("repo/README.md");
    const secret = file("repo/.env", "SECRET");
    const source = new ExternalMarkdownSource([file("repo/a.md"), file("repo/docs/README.md"), readme, secret]);
    expect(source.initialDocument().path).toBe("repo/README.md");
    expect(source.documents).toHaveLength(3);
    expect(source.label).toBe("Folder: repo");
    expect(await source.read(source.initialDocument())).toBe("# Hello");
    expect(secret.arrayBuffer).not.toHaveBeenCalled();
    expect(source.linkedDocument("repo/README.md", ".env")).toBeUndefined();
  });

  it("does not require secure-context randomUUID for local-file reading", async () => {
    vi.stubGlobal("crypto", {});
    const source = new ExternalMarkdownSource([file("README.md")]);
    expect(source.initialDocument().id).not.toBe("");
    expect(await source.read(source.initialDocument())).toBe("# Hello");
  });

  it("resolves only selected files with UTF-8, spaces, fragments and parent references", () => {
    const source = new ExternalMarkdownSource([file("repo/README.md"), file("repo/docs/日本 語.MARKDOWN")]);
    expect(source.linkedDocument("repo/README.md", "docs/%E6%97%A5%E6%9C%AC%20%E8%AA%9E.MARKDOWN#setup")?.document.path).toBe("repo/docs/日本 語.MARKDOWN");
    expect(source.linkedDocument("repo/docs/日本 語.MARKDOWN", "../README.md#top")?.fragment).toBe("top");
    expect(source.linkedDocument("repo/README.md", "../outside.md")).toBeUndefined();
    expect(source.linkedDocument("repo/README.md", "other.md")).toBeUndefined();
  });

  it.each(["/etc/passwd", "//host/private.md", "file:///tmp/note.md", "https://host/readme.md", "C:\\private.md", "..\\secret.md", "%2Fetc/passwd", "docs%2fsecret.md", "%5csecret.md", "%00secret.md", "javascript%3Aalert.md", "../../secret.md", "doc.md?token=x", "bad%ZZ.md"])("rejects unsafe target %s", (href) => {
    expect(resolveLocalTarget("repo/README.md", href)).toBeUndefined();
  });

  it("rejects invalid selections and oversize/unreadable/non-UTF-8 files", async () => {
    expect(() => new ExternalMarkdownSource([file("x.txt")])).toThrow(/markdown/i);
    expect(() => new ExternalMarkdownSource([file("README.md"), file("README.md")])).toThrow(/same path/);
    expect(() => new ExternalMarkdownSource([file("../README.md")])).toThrow(/path/);
    expect(() => new ExternalMarkdownSource(Array.from({length: 5001}, () => file("a.txt")))).toThrow(/5,000/);
    const tooBig = file("large.md");
    Object.defineProperty(tooBig, "size", { value: MAX_MARKDOWN_BYTES + 1 });
    let source = new ExternalMarkdownSource([tooBig]);
    await expect(source.read(source.initialDocument())).rejects.toThrow(/2 MiB/);
    expect(tooBig.arrayBuffer).not.toHaveBeenCalled();
    source = new ExternalMarkdownSource([file("invalid.md", new Uint8Array([0xff]))]);
    await expect(source.read(source.initialDocument())).rejects.toThrow(/UTF-8/);
    const missing = file("missing.md");
    vi.mocked(missing.arrayBuffer).mockRejectedValue(new Error("NotReadableError"));
    source = new ExternalMarkdownSource([missing]);
    await expect(source.read(source.initialDocument())).rejects.toThrow(/no longer readable/);
  });

  it("validates raster signatures, caches selected images and revokes URLs on close", async () => {
    const create = vi.fn(() => "blob:local-image");
    const revoke = vi.fn();
    vi.stubGlobal("URL", class extends URL { static createObjectURL = create; static revokeObjectURL = revoke; });
    const png = file("repo/p.png", new Uint8Array([137,80,78,71,13,10,26,10]));
    const spoof = file("repo/evil.png", "<svg onload='fetch(\"/api/note\")' />");
    const huge = file("repo/huge.png");
    Object.defineProperty(huge, "size", { value: MAX_IMAGE_BYTES + 1 });
    const source = new ExternalMarkdownSource([file("repo/README.md"), png, spoof, huge, file("repo/evil.svg")]);
    expect(await source.image("repo/README.md", "p.png")).toEqual({ src: "blob:local-image", mimeType: "image/png" });
    expect(await source.image("repo/README.md", "p.png")).toEqual({ src: "blob:local-image", mimeType: "image/png" });
    expect(png.arrayBuffer).toHaveBeenCalledOnce();
    for (const target of ["evil.png", "evil.svg", "huge.png", "missing.png", "https://tracker/p.png", "/api/note"]) {
      expect(await source.image("repo/README.md", target)).toBeUndefined();
    }
    expect(huge.arrayBuffer).not.toHaveBeenCalled();
    expect(create).toHaveBeenCalledOnce();
    source.dispose();
    expect(revoke).toHaveBeenCalledWith("blob:local-image");
    expect(await source.image("repo/README.md", "p.png")).toBeUndefined();
    await expect(source.read(source.initialDocument())).rejects.toThrow(/closed/);
    vi.unstubAllGlobals();
  });

  it("does not create a URL for an image still loading when its source closes", async () => {
    const create = vi.fn();
    vi.stubGlobal("URL", class extends URL { static createObjectURL = create; static revokeObjectURL = vi.fn(); });
    const png = file("p.png");
    let resolve!: (bytes: ArrayBuffer) => void;
    vi.mocked(png.arrayBuffer).mockImplementation(() => new Promise((done) => { resolve = done; }));
    const source = new ExternalMarkdownSource([file("README.md"), png]);
    const pending = source.image("README.md", "p.png");
    source.dispose();
    resolve(new Uint8Array([137,80,78,71,13,10,26,10]).buffer);
    expect(await pending).toBeUndefined();
    expect(create).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
