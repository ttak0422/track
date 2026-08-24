import { createElement } from "react";
import { wikiPattern } from "./plugins";

// Block-level include directive (ADR 0031): `![[...]]` starting a line, with optional trailing babel
// options (`:only-contents`, `:lines ...`). Mirrors the engine's includeLine grammar.
const includePattern = /^([ \t]*)!\[\[([^[\]]+)\]\][ \t]*.*$/gm;

// toPortableMarkdown strips track-specific link constructs so the body pastes cleanly elsewhere:
//   [[key]] / [[key|alias]]      -> the alias, else the key (heading anchor dropped)
//   ![[Note##h:only-contents]]   -> the referenced title as plain text (include NOT expanded)
// Ordinary Markdown (headings, lists, code, [text](url) links, images) passes through untouched. Action
// links are already flattened upstream by the server's /api/render, so only wiki links are handled here.
// ponytail: regex-level flattening, so a literal [[x]] inside a fenced code block is also flattened;
// skipping code fences would need a full parse — accept it until someone actually pastes that.
export function toPortableMarkdown(body: string): string {
  // Reduce each include line to its bare [[...]] link (drop the `!` marker and trailing options), then
  // let the wiki-link pass below flatten it to plain text like any other link.
  const withoutIncludes = body.replace(includePattern, (_, indent: string, inner: string) => `${indent}[[${inner}]]`);
  wikiPattern.lastIndex = 0;
  return withoutIncludes.replace(wikiPattern, (_, target: string, alias?: string) => {
    const label = alias?.trim();
    // No alias: use the resolution key (the text before the first heading anchor `#`).
    return label && label.length > 0 ? label : (target.split("#", 1)[0] ?? target).trim();
  });
}

// portableToHtml renders portable Markdown to a static HTML string for the rich (Confluence) copy. It
// reuses the app's react-markdown pipeline (default HTML tags + GFM tables/strikethrough/task lists), so
// the output is semantic HTML a rich editor can paste. react-dom/server and the markdown deps are
// dynamically imported so they load only when a rich-copy action is used, staying out of the main chunk.
export async function portableToHtml(portable: string): Promise<string> {
  const [{ renderToStaticMarkup }, { default: Markdown }, { default: remarkGfm }] = await Promise.all([
    import("react-dom/server"),
    import("react-markdown"),
    import("remark-gfm"),
  ]);
  return renderToStaticMarkup(createElement(Markdown, { remarkPlugins: [remarkGfm] }, portable));
}
