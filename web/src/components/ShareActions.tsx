import { useEffect, useState } from "react";
import type { NoteID } from "../types";
import { copyText } from "./markdown/clipboard";
import { IconBrandX, IconLink, RailIcon } from "./icons";

// publishedNoteURL is derived from the export's configured public base so the prerendered HTML already
// contains a useful share target. The browser fallback keeps a manually opened local static export
// usable when its site descriptor has no base URL.
export function publishedNoteURL(noteID: NoteID, baseURL?: string): string {
  const base = baseURL?.replace(/\/+$/, "");
  if (base) return `${base}/notes/${encodeURIComponent(String(noteID))}/`;
  return typeof window === "undefined" ? "" : window.location.href;
}

function xIntentURL(url: string, title: string): string {
  // The URL rides in text so a blank line separates it from the title; a
  // separate url param would make X append the link a second time.
  const params = new URLSearchParams({
    text: `${title}\n\n${url}`,
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
          <RailIcon Icon={IconBrandX} className="share-action-icon" />
          <span className="sr-only">Share on X</span>
        </a>
        <button
          className="share-action share-action-copy"
          type="button"
          onClick={() => void copyLink()}
          data-tooltip={copied ? "Copied" : "Copy link"}
          aria-label={copied ? "Copied" : "Copy link"}
        >
          <RailIcon Icon={IconLink} className="share-action-icon" />
          <span className="sr-only">{copied ? "Copied" : "Copy link"}</span>
        </button>
      </div>
    </section>
  );
}


