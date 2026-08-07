import { useEffect, useRef, useState } from "react";
import { copyText } from "./markdown/clipboard";

interface TitleCopyButtonProps {
  title: string;
  // Site-specific class: the note page's h1 variant (note-title-copy) and the floating window
  // chrome's (wiki-preview-copy) each carry their own sizing and hover-reveal rules.
  className?: string;
}

// TitleCopyButton copies a note title to the clipboard, switching its glyph to a check for a moment
// when it succeeds. Shared by the full-page reader (beside the h1) and the floating preview chrome.
export function TitleCopyButton({ title, className = "note-title-copy" }: TitleCopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(
    () => () => {
      if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    },
    [],
  );

  async function copyTitle() {
    if (!(await copyText(title))) return;
    setCopied(true);
    if (resetTimer.current !== undefined) window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
  }

  return (
    <button
      type="button"
      className={className}
      onClick={() => void copyTitle()}
      // Inside the floating chrome the whole bar drags; stop the click from starting a move.
      onPointerDown={(event) => event.stopPropagation()}
      aria-label={copied ? "Title copied" : "Copy title"}
      title={copied ? "Title copied" : "Copy title"}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </button>
  );
}

function CopyIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <rect x="8" y="8" width="11" height="11" rx="1.5" />
      <path d="M16 8V5.5A1.5 1.5 0 0 0 14.5 4h-9A1.5 1.5 0 0 0 4 5.5v9A1.5 1.5 0 0 0 5.5 16H8" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="m5 12.5 4.2 4.2L19 7" />
    </svg>
  );
}
