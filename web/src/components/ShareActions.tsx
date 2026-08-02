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
      <div className="share-actions-list">
        <a
          className="share-action share-action-x"
          href={xIntentURL(url, title)}
          target="_blank"
          rel="noopener noreferrer"
          data-tooltip="Share on X"
          aria-label="Share on X"
        >
          <XIcon />
          <span className="sr-only">Share on X</span>
        </a>
        <button
          className="share-action share-action-copy"
          type="button"
          onClick={() => void copyLink()}
          data-tooltip={copied ? "Copied" : "Copy link"}
          aria-label={copied ? "Copied" : "Copy link"}
        >
          <LinkIcon />
          <span className="sr-only">{copied ? "Copied" : "Copy link"}</span>
        </button>
      </div>
    </section>
  );
}

function XIcon() {
  return (
    <svg className="share-action-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817-5.963 6.817H1.684l7.73-8.835L1.254 2.25H8.08l4.713 6.231 5.45-6.231Zm-1.161 17.52h1.833L7.084 4.126H5.117L17.083 19.77Z" />
    </svg>
  );
}

function LinkIcon() {
  return (
    <svg className="share-action-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.71 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </svg>
  );
}
