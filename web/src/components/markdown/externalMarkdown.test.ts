import { describe, expect, it, vi } from "vitest";
import {
  externalMarkdownLink,
  isExternalMarkdownImage,
  isRelativeMarkdownImage,
  isRelativeMarkdownReference,
  scrollExternalMarkdownFragment,
} from "./externalMarkdown";

describe("external markdown reference boundary", () => {
  it.each([
    "javascript:alert(1)", "JaVaScRiPt:alert(1)", "java%73cript%3Aalert(1)",
    "data:text/html,hello", "blob:https://example.test/id", "file:///etc/passwd", "mailto:reader@example.test",
    "track:delete", "http:example.test", "//example.test/pixel", "%2f%2fexample.test/pixel",
    "/api/notes", "%2Fapi/notes", "\\\\example.test\\share", "a\\b.md", "a%5Cb.md",
    "\njavascript:alert(1)", "%0ajavascript:alert(1)", "./a%00.md", "./a%7f.md", " ./a.md", "./bad%zz.md",
    "", "?query",
  ])("does not treat %j as a selected-file reference", (value) => {
    expect(isRelativeMarkdownReference(value)).toBe(false);
    expect(externalMarkdownLink(value)).toBeUndefined();
  });

  it.each(["guide.md", "./guide.md#intro", "../other/日本語.md", "images/photo%20one.png", "example.test/file.md"])(
    "preserves a relative reference without promoting it to a URL: %s", (href) => {
      expect(externalMarkdownLink(href)).toEqual({ kind: "file", href });
    },
  );

  it("permits only explicit HTTP(S) URLs and local fragments separately from files", () => {
    expect(externalMarkdownLink("https://example.test/doc#intro")).toEqual({ kind: "web", href: "https://example.test/doc#intro" });
    expect(externalMarkdownLink("http://example.test/doc")).toEqual({ kind: "web", href: "http://example.test/doc" });
    expect(externalMarkdownLink("#intro")).toEqual({ kind: "fragment", href: "#intro" });
    expect(externalMarkdownLink("https://")).toBeUndefined();
  });

  it("requires a parent-issued blob and explicit raster MIME", () => {
    expect(isRelativeMarkdownImage("./safe.png")).toBe(true);
    for (const ref of ["a.svg", "a.SVG#view", "a%2esvg", "a.svgz", "https://example.test/pixel.png", "/api/asset/image.png"]) {
      expect(isRelativeMarkdownImage(ref)).toBe(false);
    }
    expect(isExternalMarkdownImage({ src: "blob:http://localhost/owned", mimeType: "image/png" })).toBe(true);
    for (const src of ["https://example.test/pixel.png", "/api/asset/image.png", "data:image/png;base64,AAAA", "blob:"]) {
      expect(isExternalMarkdownImage({ src, mimeType: "image/png" })).toBe(false);
    }
    expect(isExternalMarkdownImage({ src: "blob:http://localhost/owned", mimeType: "image/svg+xml" })).toBe(false);
    expect(isExternalMarkdownImage({ src: "blob:http://localhost/owned", mimeType: "text/html" })).toBe(false);
  });

  it("scrolls only inside its own document and maps ordinary heading fragments", () => {
    const root = document.createElement("article");
    root.innerHTML = '<h2 id="h-installation">Installation</h2><h2 id="h-日本語">日本語</h2>';
    const scroll = vi.spyOn(root.children[0], "scrollIntoView");
    expect(scrollExternalMarkdownFragment(root, "#installation")).toBe(true);
    expect(scroll).toHaveBeenCalledWith({ block: "start" });
    expect(scrollExternalMarkdownFragment(root, "#%E6%97%A5%E6%9C%AC%E8%AA%9E")).toBe(true);
    const outside = document.createElement("div");
    outside.id = "outside-document";
    document.body.append(outside);
    expect(scrollExternalMarkdownFragment(root, "#outside-document")).toBe(false);
    expect(scrollExternalMarkdownFragment(root, "#bad%zz")).toBe(false);
    outside.remove();
  });
});
