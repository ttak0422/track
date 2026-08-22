// Turn the newest Lighthouse results (.lighthouseci/lhr-*.json, written by `lhci collect`) into a
// Markdown report appended to the GitHub Actions run summary ($GITHUB_STEP_SUMMARY), so the help
// site's scores are visible on the run page without digging through logs or downloading reports.
// One row per measured URL — a single overall number hides which page carries the cost.
import { appendFileSync, readdirSync, readFileSync } from "node:fs";

const dir = ".lighthouseci";

const files = readdirSync(dir)
  .filter((f) => /^lhr-.*\.json$/.test(f))
  .sort();
if (files.length === 0) {
  console.error(`no Lighthouse result found in ${dir}/`);
  process.exit(1);
}

const metrics = [
  ["FCP", "first-contentful-paint"],
  ["LCP", "largest-contentful-paint"],
  ["TBT", "total-blocking-time"],
  ["CLS", "cumulative-layout-shift"],
  ["SI", "speed-index"],
];

const rows = files.map((f) => {
  const lhr = JSON.parse(readFileSync(`${dir}/${f}`, "utf8"));
  const url = String(lhr.finalDisplayedUrl ?? lhr.finalUrl ?? lhr.requestedUrl ?? "?").replace(
    /^https?:\/\/[^/]+/,
    "",
  );
  const cells = [`| ${url || "/"} | ${Math.round((lhr.categories?.performance?.score ?? 0) * 100)} |`];
  for (const [, id] of metrics) {
    const value = lhr.audits?.[id]?.displayValue ?? "–";
    cells.push(` ${value} |`);
  }
  return cells.join("");
});

const head = [
  "| URL | Performance | " + metrics.map(([label]) => label).join(" | ") + " |",
  `| --- | ---: | ${metrics.map(() => "---:").join(" | ")} |`,
].join("\n");

const table = ["## Lighthouse — help site", "", head, ...rows, ""].join("\n");

const summary = process.env.GITHUB_STEP_SUMMARY;
if (summary) {
  appendFileSync(summary, `${table}\n`);
}
console.log(table);
