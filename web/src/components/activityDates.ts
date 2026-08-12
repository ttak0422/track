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

// monthColumnLabels names the week columns where a month begins, so the heatmap can caption its
// columns the way GitHub's graph does. The first column is always named — a window narrow enough to
// sit inside one month would otherwise say nothing about when it is — and every later column is named
// only when its first day belongs to a new month. Numeric ("01".."12"), which needs no translation
// and fits the column pitch two characters wide.
export function monthColumnLabels(dates: string[]): { column: number; label: string }[] {
  const labels: { column: number; label: string }[] = [];
  let previous = "";
  for (let column = 0; column * 7 < dates.length; column++) {
    const month = dates[column * 7].slice(5, 7);
    if (column === 0 || month !== previous) {
      labels.push({ column, label: month });
    }
    previous = month;
  }
  return labels;
}

export function dateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}
