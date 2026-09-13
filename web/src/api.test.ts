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

// The live branch needs a JSON body to adopt read state from; the static branch reuses the default
// stub, whose .bin files hold the locked bundle.
async function searchNotes(staticMode: boolean, query: string, limit: number, vault: string) {
  if (!staticMode) {
    vi.stubGlobal("fetch", async (url: string) => {
      fetched.push(url);
      return new Response(JSON.stringify({ results: [] }));
    });
  }
  vi.stubEnv("VITE_TRACK_STATIC", staticMode ? "1" : "");
  vi.resetModules();
  return (await import("./api")).searchNotes(query, limit, vault);
}

describe("searchNotes", () => {
  it("keeps the search federated when no vault is named", async () => {
    await searchNotes(false, "alpha", 100, "");
    expect(fetched).toEqual(["/api/search?limit=100&q=alpha"]);
  });

  it("narrows the search to the named vault", async () => {
    await searchNotes(false, "alpha", 100, "work");
    expect(fetched).toEqual(["/api/search?limit=100&q=alpha&vault=work"]);
  });

  it("ignores the vault on the published site, whose search is one vault baked into the bundle", async () => {
    await searchNotes(true, "alpha", 100, "work");
    expect(fetched).toEqual(["/data/notes.bin", "/data/search.bin"]);
  });
});

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

// The whole-vault graph is scoped like the agenda: a request names the vault it wants (the empty
// name is the launch vault), and the response's "vault" label qualifies every id with it, so two
// vaults' same-numbered notes stay distinct in the query cache. The local graph is addressed by the
// note's own qualified id, which is how the client tells a foreign vault from the one it was
// launched in.
describe("graph requests and their vault", () => {
  function jsonGraph(vault: string) {
    return JSON.stringify({
      vault,
      graph: {
        center_id: 7,
        nodes: [{ note_id: 7, file_kind: "note", title: "Root" }],
        edges: [],
      },
    });
  }

  it("scopes the whole-vault graph to the requested vault and qualifies its ids", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "");
    vi.resetModules();
    const { getGraph } = await import("./api");
    vi.stubGlobal("fetch", async (url: string) => {
      fetched.push(url);
      return new Response(jsonGraph("work"));
    });

    const data = await getGraph("work");
    expect(fetched).toEqual(["/api/graph?vault=work"]);
    expect(data.graph.nodes[0].note_id).toBe("work~7");
  });

  it("leaves the whole-vault graph unscoped for the launch vault", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "");
    vi.resetModules();
    const { getGraph } = await import("./api");
    vi.stubGlobal("fetch", async (url: string) => {
      fetched.push(url);
      return new Response(jsonGraph(""));
    });

    const data = await getGraph("");
    expect(fetched).toEqual(["/api/graph"]);
    expect(data.graph.nodes[0].note_id).toBe("7");
  });

  it("addresses the local graph at the note's own vault, not the launch vault", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "");
    vi.resetModules();
    const { getLocalGraph } = await import("./api");
    vi.stubGlobal("fetch", async (url: string) => {
      fetched.push(url);
      return new Response(jsonGraph("work"));
    });

    await getLocalGraph("work~7");
    expect(fetched).toEqual(["/api/graph/local?id=7&vault=work"]);
  });
});

// The published site draws its graphs from one locked file (data/graph.bin) holding the whole
// vault's graph; the local graph is derived from it per note rather than fetched per note. A broken
// read here breaks the graph on every published page while leaving the live workspace untouched —
// the regression this pins (the SSG note-aside graph once stopped painting after the live server's
// graph endpoint changed shape).
describe("published graph data", () => {
  // Locks a graph payload with the page's key (see lock.test.ts) and stubs fetch to serve it as the
  // .bin file staticData asks for.
  async function serveGraph(graph: unknown) {
    const { lock } = await import("./lock");
    const locked = await lock(JSON.stringify({ graph }));
    vi.stubGlobal("fetch", async (url: string) => {
      fetched.push(String(url));
      return new Response(Uint8Array.from(atob(locked), (c) => c.charCodeAt(0)));
    });
  }

  const wholeGraph = {
    center_id: "",
    nodes: [
      { note_id: "1fBExGozUeXAXNB324DsDk", file_kind: "note", title: "Babel", size: 3 },
      { note_id: "5bsKWpzrLcdY0SkLtwsCr9", file_kind: "note", title: "Foreign" },
    ],
    edges: [{ source_id: "1fBExGozUeXAXNB324DsDk", target_id: "5bsKWpzrLcdY0SkLtwsCr9" }],
  };

  it("reads the whole-vault graph from the locked graph.bin", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "1");
    vi.resetModules();
    const { getGraph } = await import("./api");
    await serveGraph(wholeGraph);

    // The published bundle holds a single vault, so the scope a live caller would name is ignored.
    const data = await getGraph("work");
    expect(fetched).toEqual(["/data/graph.bin"]);
    expect(data.graph).toEqual(wholeGraph);
  });

  it("derives a note's local graph from the bundle and marks the centre", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "1");
    vi.resetModules();
    const { getLocalGraph } = await import("./api");
    await serveGraph(wholeGraph);

    const data = await getLocalGraph("1fBExGozUeXAXNB324DsDk");
    expect(fetched).toEqual(["/data/graph.bin"]);
    expect(data.graph.center_id).toBe("1fBExGozUeXAXNB324DsDk");
    expect(data.graph.nodes).toContainEqual({
      note_id: "1fBExGozUeXAXNB324DsDk",
      file_kind: "note",
      title: "Babel",
      size: 3,
      center: true,
    });
    // The 1-hop neighbourhood: the centre plus its direct neighbours, never the whole vault.
    expect(data.graph.nodes.map((node) => node.note_id).sort()).toEqual([
      "1fBExGozUeXAXNB324DsDk",
      "5bsKWpzrLcdY0SkLtwsCr9",
    ]);
    expect(data.graph.edges).toEqual(wholeGraph.edges);
  });
});

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

// The vault-scoped activity and new-notes requests ride the same ?vault=<name> param as every other
// scoped endpoint; the launch vault sends none, so a single-vault workspace's URLs are unchanged.
describe("vault-scoped activity and new-notes requests", () => {
  it("appends the vault to the activity request", async () => {
    const { getActivity } = await import("./api");
    await getActivity("2026-01-01", "2026-01-31", "work");
    expect(fetched).toEqual(["/api/activity?since=2026-01-01&until=2026-01-31&vault=work"]);
  });

  it("omits the vault param for the launch vault", async () => {
    const { getActivity } = await import("./api");
    await getActivity("2026-01-01", "2026-01-31");
    expect(fetched).toEqual(["/api/activity?since=2026-01-01&until=2026-01-31"]);
  });

  it("appends the vault to the new-notes listing", async () => {
    vi.stubGlobal(
      "fetch",
      async (url: string) => {
        fetched.push(String(url));
        return new Response(JSON.stringify({ notes: [] }), { headers: { "content-type": "application/json" } });
      },
    );
    const { listNewNotes } = await import("./api");
    await listNewNotes(10, "work");
    expect(fetched).toEqual(["/api/notes?sort=created&limit=10&vault=work"]);
  });

  it("keeps the static new-notes listing empty and vault-free", async () => {
    vi.stubEnv("VITE_TRACK_STATIC", "1");
    vi.resetModules();
    const { listNewNotes } = await import("./api");
    await expect(listNewNotes(10, "work")).resolves.toEqual({ notes: [] });
    expect(fetched).toEqual([]);
  });
});
