import { beforeAll, describe, expect, it } from "vitest";
import { lock, unlock, unlockText } from "./lock";
import { setStalePageHandler } from "./runtime";

// The key `track export-site` derives for one site's address, and a data file it locked with it —
// both produced by internal/track/site/lock.go. The two sides of the lock are written in different
// languages, so this fixture is what keeps them one mechanism.
const KEY = "LG4/Bc9q+UaWD+sEC9s/LpmC3x1KBuBGmFh/NsbBdAA=";
const BLOB =
  "1a2npt5isSpeSoXgXTbocf/S/NLInRT/FleFYxGj1ckLCZAwtp3vBN/eA82UQeCt8CytLWpeo9rEznAFlv/eY3UkLCHtMvGXUgGHrBTAldexOM30k5PgrLFtOHU=";

beforeAll(() => {
  // The page carries the site key; here a stand-in for it (the module reads it lazily).
  window.__trackLock = KEY;
});

describe("the site data lock", () => {
  it("opens a file the Go exporter locked", async () => {
    expect(JSON.parse(await unlockText(BLOB))).toEqual({ notes: [{ note_id: "abc", title: "Home" }] });
  });

  it("round-trips the state the prerender inlines", async () => {
    const state = JSON.stringify({ queries: [{ queryKey: ["note", "abc"], state: { data: "body" } }] });
    const locked = await lock(state);
    expect(locked).not.toContain("queryKey");
    expect(await unlockText(locked)).toBe(state);
  });

  it("refuses data this page's key does not open, and reports the page as stale", async () => {
    let stale = 0;
    setStalePageHandler(() => stale++);
    const bytes = Uint8Array.from(atob(BLOB), (c) => c.charCodeAt(0));
    bytes[bytes.length - 1] ^= 0xff;
    await expect(unlock(bytes.buffer)).rejects.toThrow();
    // The page cannot read what the site published, so the client's recovery (one reload) is signalled.
    expect(stale).toBe(1);
    setStalePageHandler(() => {});
  });
});
