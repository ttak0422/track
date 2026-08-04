#!/usr/bin/env node
// Screenshot the design playground across candidate themes and write a side-by-side comparison
// page (ADR 0068). Needs a dev server — `make site-dev`, or `npm run dev` in web/ — because the
// playground and its candidates exist only there, plus a one-time browser download:
//   npx --yes playwright@1.62.1 install chromium
//
// Usage: node scripts/design-shots.mjs [path ...]     (default: /gallery)
// Env:   BASE=http://localhost:5173  OUT=_design-shots
//        VARIANTS=candidate-a,candidate-b  THEMES=light,dark

import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Exact pin, npx-only — like the lighthouse target, the tool is not a package.json dependency.
const PLAYWRIGHT = "playwright@1.62.1";

const BASE = process.env.BASE ?? "http://localhost:5173";
const OUT = process.env.OUT ?? "_design-shots";
// The default variant list comes from candidates.css itself — the one place a candidate is
// declared — so an added or renamed candidate cannot silently drop out of the sheet.
const candidatesCss = readFileSync(new URL("../web/src/dev/candidates.css", import.meta.url), "utf8");
const declared = [...new Set([...candidatesCss.matchAll(/\[data-theme-variant="([\w-]+)"\]/g)].map((m) => m[1]))];
// "base" = no variant param: the current styles.css tokens, the column to beat.
const variants = ["base", ...(process.env.VARIANTS?.split(",").filter(Boolean) ?? declared)];
const themes = (process.env.THEMES ?? "light,dark").split(",").filter(Boolean);
const pages = process.argv.length > 2 ? process.argv.slice(2) : ["/gallery"];

mkdirSync(OUT, { recursive: true });

const shots = [];
for (const page of pages) {
  for (const theme of themes) {
    for (const variant of variants) {
      const params = new URLSearchParams({ theme });
      if (variant !== "base") params.set("variant", variant);
      const url = `${BASE}${page}?${params}`;
      const file = `${slug(page)}--${theme}--${variant}.png`;
      console.log(`shot ${url} -> ${OUT}/${file}`);
      try {
        execFileSync(
          "npx",
          [
            "--yes",
            PLAYWRIGHT,
            "screenshot",
            "--viewport-size=1280,900",
            "--wait-for-timeout=1200",
            "--full-page",
            url,
            join(OUT, file),
          ],
          { stdio: "inherit" },
        );
      } catch {
        console.error(`\nscreenshot failed. Is the dev server running at ${BASE}? (make site-dev)`);
        console.error(`If chromium is missing: npx --yes ${PLAYWRIGHT} install chromium`);
        process.exit(1);
      }
      shots.push({ page, theme, variant, file });
    }
  }
}

// One row per page × theme, one column per variant — the "pick a number" page.
const rows = [];
for (const page of pages) {
  for (const theme of themes) {
    const cells = shots
      .filter((s) => s.page === page && s.theme === theme)
      .map(
        (s) =>
          `<figure><figcaption>${s.variant}</figcaption><a href="${s.file}"><img src="${s.file}" loading="lazy"></a></figure>`,
      )
      .join("");
    rows.push(`<section><h2>${page} · ${theme}</h2><div class="grid">${cells}</div></section>`);
  }
}
writeFileSync(
  join(OUT, "index.html"),
  `<!doctype html><meta charset="utf-8"><title>design shots</title>
<style>
  body { font: 14px system-ui; margin: 24px; background: #1c1c1c; color: #ddd; }
  h2 { font-size: 14px; font-weight: 500; }
  .grid { display: flex; gap: 16px; overflow-x: auto; padding-bottom: 8px; }
  figure { margin: 0; flex: 0 0 auto; }
  figcaption { font-family: ui-monospace, monospace; font-size: 11px; margin-bottom: 6px; color: #999; }
  img { width: 420px; display: block; border: 1px solid #444; }
</style>
${rows.join("\n")}`,
);
console.log(`\nwrote ${OUT}/index.html — open it and pick a candidate.`);

function slug(p) {
  return p.replace(/[^a-z0-9]+/gi, "-").replace(/^-|-$/g, "") || "home";
}
