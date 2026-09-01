// How the docked rail hands out its vertical room. Every capped list in the aside shows the same
// 320px however tall the screen is, so a 1440p display reads five stubs with four scrollbars while
// the room below them goes to waste. This is the rule that spends that room.
//
// The rail's own scroll stays the safety net: nothing here shrinks a list. Below the cap the aside
// behaves exactly as it always has, and only genuine spare room is handed out.

// The cap a list keeps when the rail has no room to spare. It is the height the aside has always
// given a list, so a screen with nothing spare renders exactly as before.
export const baseListCap = 320;

export interface AsideList {
  // Rows in the list. The share is weighted by this, so a list with twice the entries takes twice
  // the room — the reader's own measure of how much a section has to say.
  items: number;
  // What the list would take uncapped, in px.
  content: number;
}

// listCaps hands each list the height it may take, in the order it was given them.
//
// Spare room is shared out by item count, and no list takes more than it can fill: a section that
// runs out of rows stops taking, and what it did not use goes back to the ones still cut off. That
// second half is what keeps the weighting honest — by item count alone, a four-entry section would
// hold a share it can only render as white space.
export function listCaps(spare: number, lists: readonly AsideList[]): number[] {
  const caps = lists.map((list) => Math.min(list.content, baseListCap));
  let left = spare;
  // Only the lists still cut off can use anything; the weights stay each list's own item count.
  let cut = lists.map((_, index) => index).filter((index) => caps[index] < lists[index].content);
  // Each round either fills a list to its content (dropping it from the next round) or spends what
  // is left, so the loop is bounded by the number of lists.
  while (left > 0.5 && cut.length > 0) {
    const weight = cut.reduce((sum, index) => sum + lists[index].items, 0);
    if (weight <= 0) break;
    const round = left;
    for (const index of cut) {
      const share = (round * lists[index].items) / weight;
      const give = Math.min(share, lists[index].content - caps[index]);
      caps[index] += give;
      left -= give;
    }
    cut = cut.filter((index) => caps[index] < lists[index].content);
  }
  return caps;
}

// applyListCaps measures a rail and writes each list the height it may take.
//
// The sizes are read rather than computed: heading heights, the font scale, and the content width
// setting all move them, and a constant here would drift from the stylesheet. Both readings are the
// ones it ran with last time — scrollHeight is a list's full content whatever cap stands on it, and
// the chrome is the rail less the lists as rendered — so the result settles in a single pass.
export function applyListCaps(rail: HTMLElement): void {
  const lists = [...rail.querySelectorAll<HTMLElement>(".backlink-list")];
  // Stacked under the note the rail has no height bound, so there is no spare room to speak of.
  const limit = Number.parseFloat(getComputedStyle(rail).maxHeight);
  if (!Number.isFinite(limit)) {
    for (const list of lists) list.style.maxHeight = "";
    return;
  }
  const sizes = lists.map((list) => ({ items: list.childElementCount, content: list.scrollHeight }));
  // Everything in the rail that is not a list: the headings, the tags, the graph's fixed height, the
  // padding. Taken as it stands, so nothing here has to know the stylesheet's numbers.
  const chrome = rail.scrollHeight - lists.reduce((total, list) => total + list.clientHeight, 0);
  const base = sizes.reduce((total, size) => total + Math.min(size.content, baseListCap), 0);
  const caps = listCaps(limit - chrome - base, sizes);
  lists.forEach((list, index) => {
    // Back to the stylesheet's own cap when there is nothing to add, rather than restating it.
    list.style.maxHeight = caps[index] > baseListCap ? `${Math.round(caps[index])}px` : "";
  });
}
