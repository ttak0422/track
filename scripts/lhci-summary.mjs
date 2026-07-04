// Turn the newest Lighthouse result (.lighthouseci/lhr-*.json, written by `lhci collect`) into a
// Markdown score table and append it to the GitHub Actions run summary ($GITHUB_STEP_SUMMARY), so the
// help site's scores are visible on the run page without digging through logs or downloading the report.
import { appendFileSync, readdirSync, readFileSync } from "node:fs";

const dir = ".lighthouseci";

const files = readdirSync(dir)
  .filter((f) => /^lhr-.*\.json$/.test(f))
  .sort();
if (files.length === 0) {
  console.error(`no Lighthouse result found in ${dir}/`);
  process.exit(1);
}

const lhr = JSON.parse(readFileSync(`${dir}/${files[files.length - 1]}`, "utf8"));
const categories = lhr.categories ?? {};
const order = ["performance", "accessibility", "best-practices", "seo"];

const rows = order
  .filter((id) => categories[id])
  .map((id) => `| ${categories[id].title} | ${Math.round((categories[id].score ?? 0) * 100)} |`);

const table = [
  "## Lighthouse — help site",
  "",
  `Tested: \`${lhr.finalDisplayedUrl ?? lhr.finalUrl ?? lhr.requestedUrl ?? "?"}\``,
  "",
  "| Category | Score |",
  "| --- | ---: |",
  ...rows,
  "",
].join("\n");

const summary = process.env.GITHUB_STEP_SUMMARY;
if (summary) {
  appendFileSync(summary, `${table}\n`);
}
console.log(table);
