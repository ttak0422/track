import { describe, expect, it } from "vitest";
import { applyListCaps, baseListCap, listCaps } from "./asideSpace";

describe("listCaps", () => {
  // The rail's own scroll is the safety net it has always been: a screen with nothing spare renders
  // exactly as before, four 320px lists deep.
  it("leaves every list at the base cap when there is nothing spare", () => {
    const lists = [
      { items: 40, content: 1040 },
      { items: 18, content: 468 },
    ];

    expect(listCaps(0, lists)).toEqual([baseListCap, baseListCap]);
    expect(listCaps(-400, lists)).toEqual([baseListCap, baseListCap]);
  });

  // A list shorter than the cap never held its whole frame to begin with — the room it does not use
  // is spare room, and it takes none of the share.
  it("gives nothing to a list that already shows all it has", () => {
    const caps = listCaps(300, [
      { items: 4, content: 104 },
      { items: 40, content: 1040 },
    ]);

    expect(caps[0]).toBe(104);
    expect(caps[1]).toBe(baseListCap + 300);
  });

  // The weighting is the point: twice the rows, twice the share.
  it("shares spare room by item count", () => {
    const caps = listCaps(300, [
      { items: 40, content: 2000 },
      { items: 20, content: 2000 },
    ]);

    expect(caps[0] - baseListCap).toBeCloseTo(200);
    expect(caps[1] - baseListCap).toBeCloseTo(100);
  });

  // By item count alone the short list would hold a share it can only render as white space. What it
  // cannot fill goes back to the list still cut off.
  it("passes on what a list cannot fill", () => {
    const caps = listCaps(400, [
      { items: 40, content: 400 },
      { items: 40, content: 2000 },
    ]);

    expect(caps[0]).toBe(400);
    expect(caps[1]).toBe(baseListCap + 320);
  });

  // Spare room beyond what every list can use simply goes unspent — no list is padded past its rows.
  it("never gives a list more than its content", () => {
    const caps = listCaps(5000, [
      { items: 40, content: 900 },
      { items: 4, content: 104 },
    ]);

    expect(caps).toEqual([900, 104]);
  });
});

// jsdom lays nothing out, so the rail is built carrying the measurements a browser would have taken.
function railWith(limit: string, lists: { items: number; shown: number; content: number }[]): HTMLElement {
  const rail = document.createElement("div");
  rail.style.maxHeight = limit;
  let listTotal = 0;
  for (const spec of lists) {
    const list = document.createElement("div");
    list.className = "backlink-list";
    for (let i = 0; i < spec.items; i += 1) list.appendChild(document.createElement("a"));
    Object.defineProperty(list, "clientHeight", { value: spec.shown, configurable: true });
    Object.defineProperty(list, "scrollHeight", { value: spec.content, configurable: true });
    rail.appendChild(list);
    listTotal += spec.shown;
  }
  // The rail as rendered: its lists, plus the 420px of chrome the measurement has to infer — the
  // headings, the tags, the graph's fixed height, the padding.
  Object.defineProperty(rail, "scrollHeight", { value: listTotal + 420, configurable: true });
  return rail;
}

describe("applyListCaps", () => {
  // The heart of it: room the rail is not using reaches the lists that are cut off.
  it("spends the rail's spare room on the lists still cut off", () => {
    const rail = railWith("1200px", [
      { items: 40, shown: baseListCap, content: 1040 },
      { items: 4, shown: 104, content: 104 },
    ]);

    applyListCaps(rail);

    const [backlinks, short] = [...rail.querySelectorAll<HTMLElement>(".backlink-list")];
    // 1200 less 420 of chrome and (320 + 104) of base leaves 356, and only the capped list can use it.
    expect(backlinks.style.maxHeight).toBe("676px");
    expect(short.style.maxHeight).toBe("");
  });

  // A rail with nothing spare is the rail as it always was: the stylesheet's cap, and the rail's own
  // scroll to reach the rest.
  it("writes no cap of its own when the rail is already full", () => {
    const rail = railWith("600px", [{ items: 40, shown: baseListCap, content: 1040 }]);

    applyListCaps(rail);

    expect(rail.querySelector<HTMLElement>(".backlink-list")!.style.maxHeight).toBe("");
  });

  // Stacked under the note there is no bound to share out, and a cap left over from the docked rail
  // has to go — the same aside is re-rendered across that breakpoint.
  it("clears the caps when the rail has no height bound", () => {
    const rail = railWith("", [{ items: 40, shown: baseListCap, content: 1040 }]);
    const list = rail.querySelector<HTMLElement>(".backlink-list")!;
    list.style.maxHeight = "676px";

    applyListCaps(rail);

    expect(list.style.maxHeight).toBe("");
  });
});
