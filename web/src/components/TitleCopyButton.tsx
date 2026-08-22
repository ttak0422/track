import { useEffect, useRef, useState } from "react";
import { copyText } from "./markdown/clipboard";
import { IconCheck, IconCopy, RailIcon } from "./icons";

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
      {copied ? <RailIcon Icon={IconCheck} /> : <RailIcon Icon={IconCopy} />}
    </button>
  );
}


