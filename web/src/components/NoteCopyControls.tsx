import { useEffect, useRef, useState } from "react";
import { copyRich, copyText } from "./markdown/clipboard";
import { toPortableMarkdown } from "./markdown/portable";

interface NoteCopyControlsProps {
  body: string;
}

// portableToHtml renders portable Markdown to a static HTML string for the rich (Confluence) copy. It
// reuses the app's react-markdown pipeline (default HTML tags + GFM tables/strikethrough/task lists), so
// the output is semantic HTML a rich editor can paste. react-dom/server and the markdown deps are
// dynamically imported so they load only when the button is used, staying out of the main chunk.
async function portableToHtml(portable: string): Promise<string> {
  const [{ renderToStaticMarkup }, { default: Markdown }, { default: remarkGfm }] = await Promise.all([
    import("react-dom/server"),
    import("react-markdown"),
    import("remark-gfm"),
  ]);
  return renderToStaticMarkup(<Markdown remarkPlugins={[remarkGfm]}>{portable}</Markdown>);
}

// NoteCopyControls exposes two flattened text buttons that copy the note with track-specific links
// neutralized: "Copy MD" writes portable Markdown as plain text; "Copy for Confluence" writes rich
// HTML (with the portable Markdown as the plain-text fallback) so pasting into Confluence keeps
// formatting. Each button briefly acknowledges with "Copied", matching the CodeBlock copy idiom.
export function NoteCopyControls({ body }: NoteCopyControlsProps) {
  const [copied, setCopied] = useState<"md" | "html" | null>(null);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    };
  }, []);

  function acknowledge(which: "md" | "html") {
    setCopied(which);
    if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setCopied(null), 1500);
  }

  async function copyMarkdown() {
    if (await copyText(toPortableMarkdown(body))) acknowledge("md");
  }

  async function copyConfluence() {
    const portable = toPortableMarkdown(body);
    const html = await portableToHtml(portable);
    if (await copyRich(html, portable)) acknowledge("html");
  }

  return (
    <>
      <button className="copy-toggle" type="button" onClick={copyMarkdown}>
        {copied === "md" ? "Copied" : "Copy MD"}
      </button>
      <button className="copy-toggle" type="button" onClick={copyConfluence}>
        {copied === "html" ? "Copied" : "Copy for Confluence"}
      </button>
    </>
  );
}
