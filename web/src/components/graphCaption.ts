// graphCountCaption is the line beside a whole-vault graph (GraphPanel, GraphFullView): how many
// notes the canvas is showing, and — since the overview draws only the linked part of the vault up
// to a cap (see overviewGraph) — how many it left out. One line, because both halves answer the
// same question and a reader glancing at the corner should not have to add them up.
export function graphCountCaption(nodeCount: number, hidden = 0): string {
  const drawn = `${nodeCount.toLocaleString()} ${nodeCount === 1 ? "note" : "notes"}`;
  return hidden > 0 ? `${drawn} · ${hidden.toLocaleString()} not drawn` : drawn;
}
