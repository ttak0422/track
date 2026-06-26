import { Link } from "@tanstack/react-router";
import { type ReactNode, useContext } from "react";
import { useResolveQuery } from "../../queries";
import { NoteKindContext } from "./context";
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
  const asset = assetHref(href, kind);
  const noteCandidate = asset ? "" : noteCandidateFromHref(href);
  const resolved = useResolveQuery(noteCandidate);

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
  return (
    <a
      className="md-link"
      href={target}
      {...(external ? { target: "_blank", rel: "noreferrer noopener" } : {})}
    >
      {children}
    </a>
  );
}
