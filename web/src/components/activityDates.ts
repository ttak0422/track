// Date math for the activity heatmap, kept framework-free so it can be tested directly.

// weekAlignedDates returns the day keys the heatmap renders: GitHub-style, rows are fixed
// weekdays (Sunday first) and columns are calendar weeks, so the range spans `weeks` columns
// ending with the current, partial week. The first date is therefore always a Sunday and the
// last is `today` — rendered into a 7-row column-flow grid, the final column simply has fewer
// cells, and a workless weekend always sits at the top/bottom rows instead of drifting.
export function weekAlignedDates(today: Date, weeks: number): string[] {
  const count = (weeks - 1) * 7 + today.getDay() + 1;
  const start = new Date(today);
  start.setDate(today.getDate() - (count - 1));
  return Array.from({ length: count }, (_, offset) => {
    const date = new Date(start);
    date.setDate(start.getDate() + offset);
    return dateKey(date);
  });
}

export function dateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}
