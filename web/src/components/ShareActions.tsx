import { useEffect, useState } from "react";
import type { NoteID } from "../types";
import { copyText } from "./markdown/clipboard";

// publishedNoteURL is derived from the export's configured public base so the prerendered HTML already
// contains a useful share target. The browser fallback keeps a manually opened local static export
// usable when its site descriptor has no base URL.
export function publishedNoteURL(noteID: NoteID, baseURL?: string): string {
  const base = baseURL?.replace(/\/+$/, "");
  if (base) return `${base}/notes/${encodeURIComponent(String(noteID))}/`;
  return typeof window === "undefined" ? "" : window.location.href;
}

function xIntentURL(url: string, title: string): string {
  const params = new URLSearchParams({
    url,
    text: title,
  });
  return `https://x.com/intent/tweet?${params.toString()}`;
}

export function ShareActions({
  noteID,
  title,
  baseURL,
}: {
  noteID: NoteID;
  title: string;
  baseURL?: string;
}) {
  const [copied, setCopied] = useState(false);
  const url = publishedNoteURL(noteID, baseURL);

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1800);
    return () => window.clearTimeout(timer);
  }, [copied]);

  if (!url) return null;

  async function copyLink() {
    setCopied(await copyText(url));
  }

  return (
    <section className="share-actions" aria-label="Share this note">
      <span className="share-actions-label">Share</span>
      <div className="share-actions-list">
        <a
          className="secondary-button share-action"
          href={xIntentURL(url, title)}
          target="_blank"
          rel="noopener noreferrer"
        >
          Share on X
        </a>
        <button className="secondary-button share-action" type="button" onClick={() => void copyLink()}>
          {copied ? "Copied" : "Copy link"}
        </button>
      </div>
    </section>
  );
}
