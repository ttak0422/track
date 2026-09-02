import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The published chart options the static export generates are locked (ADR 0069), so the reader fetches
// "<name>.echarts.bin" and opens it, while the reference in the note body keeps naming the kind
// (".echarts.json"). A live workspace fetches its assets unchanged. Both fixtures come from
// internal/track/site/lock.go — see lock.test.ts for the same key.
const KEY = "LG4/Bc9q+UaWD+sEC9s/LpmC3x1KBuBGmFh/NsbBdAA=";
const BLOB =
  "1a2npt5isSpeSoXgXTbocf/S/NLInRT/FleFYxGj1ckLCZAwtp3vBN/eA82UQeCt8CytLWpeo9rEznAFlv/eY3UkLCHtMvGXUgGHrBTAldexOM30k5PgrLFtOHU=";

const fetched: string[] = [];

beforeEach(() => {
  window.__trackLock = KEY;
  fetched.length = 0;
  vi.stubGlobal("fetch", async (url: string) => {
    fetched.push(url);
    return url.endsWith(".bin")
      ? new Response(Uint8Array.from(atob(BLOB), (c) => c.charCodeAt(0)))
      : new Response("plain text");
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

// STATIC_MODE is baked in at import time, so each case imports api.ts after setting the build flag.
async function fetchAssetText(staticMode: boolean, href: string): Promise<string> {
  vi.stubEnv("VITE_TRACK_STATIC", staticMode ? "1" : "");
  vi.resetModules();
  return (await import("./api")).fetchAssetText(href);
}

describe("fetchAssetText", () => {
  it("opens the locked chart option on a published site", async () => {
    const text = await fetchAssetText(true, "assets/abc.echarts.json");
    expect(fetched).toEqual(["assets/abc.echarts.bin"]);
    expect(JSON.parse(text)).toEqual({ notes: [{ note_id: "abc", title: "Home" }] });
  });

  it("leaves an author's own attachment alone", async () => {
    expect(await fetchAssetText(true, "assets/abc.mermaid")).toBe("plain text");
    expect(fetched).toEqual(["assets/abc.mermaid"]);
  });

  it("fetches assets unchanged in the live workspace", async () => {
    expect(await fetchAssetText(false, "/api/asset?path=x.echarts.json")).toBe("plain text");
    expect(fetched).toEqual(["/api/asset?path=x.echarts.json"]);
  });
});

// The published site has no /api/ogp to call, so the reader's browser fetches the linked page itself.
async function getOgp(url: string) {
  vi.stubEnv("VITE_TRACK_STATIC", "1");
  vi.resetModules();
  return (await import("./api")).getOgp(url);
}

describe("parseOgp", () => {
  it("reads the Open Graph tags and resolves a relative image", async () => {
    const { parseOgp } = await import("./api");
    const html = `<html><head>
      <title>ignored when og:title is present</title>
      <meta property="og:title" content="A linked page">
      <meta property="og:description" content="What the page says about itself.">
      <meta property="og:image" content="/images/card.png">
      <meta property="og:site_name" content="Example">
    </head><body><meta property="og:title" content="body tags are not metadata"></body></html>`;
    expect(parseOgp(html, "https://example.com/post", "https://example.com/post")).toEqual({
      url: "https://example.com/post",
      title: "A linked page",
      description: "What the page says about itself.",
      image: "https://example.com/images/card.png",
      site_name: "Example",
    });
  });

  it("falls back to <title> and the host, and drops an unsafe image", async () => {
    const { parseOgp } = await import("./api");
    const html = `<html><head>
      <title>  Untagged page  </title>
      <meta name="description" content="Only a plain description tag.">
      <meta property="og:image" content="javascript:alert(1)">
    </head></html>`;
    expect(parseOgp(html, "https://example.org/page", "https://example.org/page")).toEqual({
      url: "https://example.org/page",
      title: "Untagged page",
      description: "Only a plain description tag.",
      site_name: "example.org",
    });
  });
});

describe("getOgp on a published site", () => {
  it("degrades to the bare card when the host refuses the cross-origin read", async () => {
    vi.stubGlobal("fetch", async () => {
      throw new TypeError("Failed to fetch");
    });
    expect(await getOgp("https://example.net/strict")).toEqual({ url: "https://example.net/strict" });
  });

  it("degrades to the bare card when the response is not HTML", async () => {
    vi.stubGlobal("fetch", async () => new Response("{}", { headers: { "content-type": "application/json" } }));
    expect(await getOgp("https://example.com/data.json")).toEqual({ url: "https://example.com/data.json" });
  });
});
