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
