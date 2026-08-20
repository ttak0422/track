import { Link } from "@tanstack/react-router";
import { type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { useResolveQuery } from "../../queries";
import { pointerCanHover, previewOpenDelay } from "../preview/stack";
import { NoteKindContext, NoteVaultContext } from "./context";
import { assetHref, noteCandidateFromHref, webHref } from "./urls";

interface ExternalLinkProps {
  href: string;
  children: ReactNode;
}

// ExternalLink renders a standard markdown [text](href). Track action links are flattened to plain text
// by the server before the body reaches the frontend, so they never appear here. A link first tries to
// resolve as a track note; otherwise http(s) and domain-like links open in a new tab.
export function ExternalLink({ href, children }: ExternalLinkProps) {
  const kind = useContext(NoteKindContext);
  const vault = useContext(NoteVaultContext);
  const asset = assetHref(href, kind, vault);
  const noteCandidate = asset ? "" : noteCandidateFromHref(href);
  const resolved = useResolveQuery(noteCandidate, vault);

  // A link into the vault's assets/ goes straight to the server endpoint that serves the file, rather
  // than being resolved against the current /notes/<id> route.
  if (asset) {
    return (
      <a className="md-link" href={asset} target="_blank" rel="noreferrer noopener">
        {children}
      </a>
    );
  }
  if (noteCandidate !== "" && resolved.data?.found) {
    return (
      <Link
        className="md-link"
        to="/notes/$noteId"
        params={{ noteId: String(resolved.data.note.note_id) }}
      >
        {children}
      </Link>
    );
  }
  const target = webHref(href);
  const external = /^https?:\/\//i.test(target);
  if (!external) {
    return <a className="md-link" href={target}>{children}</a>;
  }
  return (
    <ExternalURLPopup target={target}>
      <a className="md-link" href={target} target="_blank" rel="noreferrer noopener">
        {children}
      </a>
    </ExternalURLPopup>
  );
}

// A pointer crossing a column of links should not flash a destination under each one. The link remains
// the affordance and the popup simply answers where it leads.
function ExternalURLPopup({ target, children }: { target: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => {
    if (timer.current !== undefined) window.clearTimeout(timer.current);
  }, []);

  function close() {
    if (timer.current !== undefined) {
      window.clearTimeout(timer.current);
      timer.current = undefined;
    }
    setOpen(false);
  }

  function scheduleOpen() {
    if (!pointerCanHover() || open || timer.current !== undefined) return;
    timer.current = window.setTimeout(() => {
      timer.current = undefined;
      if (pointerCanHover()) setOpen(true);
    }, previewOpenDelay);
  }

  return (
    <span className="md-link-url-wrap" onMouseEnter={scheduleOpen} onMouseLeave={close}>
      {children}
      {open ? <span className="md-link-url-popup" role="tooltip">{target}</span> : null}
    </span>
  );
}
