import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import { scrollExternalMarkdownFragment } from "./markdown/ExternalMarkdownView";
import { MarkdownView } from "./MarkdownView";
import { useExternalMarkdownSource } from "./ExternalMarkdownSource";
import { consumeMarkdownHandoff } from "../markdownHandoff";

export function MarkdownViewer({ documentID = "" }: { documentID?: string }) {
  const { source, select, clear } = useExternalMarkdownSource();
  const navigate = useNavigate();
  const hash = useRouterState({ select: (state) => state.location.hash });
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const viewer = useRef<HTMLDivElement>(null);
  const article = useRef<HTMLElement>(null);
  const handoff = useRef<{ token: string; promise: ReturnType<typeof consumeMarkdownHandoff> } | null>(null);
  const selectionEpoch = useRef(0);
  const [selectionError, setSelectionError] = useState("");
  const [linkError, setLinkError] = useState("");
  const [loaded, setLoaded] = useState<{ id: string; markdown: string; error: string } | null>(null);
  const document = source?.document(documentID || source.activeDocumentID);

  useEffect(() => {
    const token = /^handoff=([a-f0-9]{64})$/.exec(hash)?.[1];
    if (!token) { handoff.current = null; return; }
    // StrictMode replays effects: share the pending consume rather than spending a one-use token
    // twice. Navigation, manual selection and closing all invalidate a late response.
    if (handoff.current?.token !== token) handoff.current = { token, promise: consumeMarkdownHandoff(token) };
    let cancelled = false;
    const epoch = selectionEpoch.current;
    void handoff.current.promise.then(({ active, files }) => {
      if (cancelled || epoch !== selectionEpoch.current) return;
      const next = select(files, "Folder from editor");
      const document = next.documents.find((item) => item.path === active);
      if (!document) throw new Error("The handed-off Markdown file is missing.");
      setSelectionError(""); setLoaded(null);
      void navigate({ to: "/markdown", search: { doc: document.id }, hash: "", replace: true });
    }).catch((error: unknown) => {
      if (cancelled || epoch !== selectionEpoch.current) return;
      setSelectionError(error instanceof Error ? error.message : "Unable to open Markdown handoff.");
      void navigate({ to: "/markdown", search: {}, hash: "", replace: true });
    });
    return () => { cancelled = true; };
  }, [hash, select, navigate]);

  useEffect(() => {
    setLinkError("");
    if (!source || !document) return;
    source.activeDocumentID = document.id;
    let cancelled = false;
    void source.read(document).then(
      (markdown) => { if (!cancelled) setLoaded({ id: document.id, markdown, error: "" }); },
      (error: unknown) => { if (!cancelled) setLoaded({ id: document.id, markdown: "", error: error instanceof Error ? error.message : "Unable to read this file. Select it again." }); },
    );
    return () => { cancelled = true; };
  }, [source, document]);

  // Body reads finish after route navigation. Scroll only inside this document, never into vault
  // chrome. Both README-style hashes and Track's h- heading namespace are accepted.
  useEffect(() => {
    if (!document || loaded?.id !== document.id || !article.current) return;
    if (hash) scrollExternalMarkdownFragment(article.current, hash);
    else viewer.current?.scrollIntoView({ block: "start" });
  }, [hash, document, loaded]);

  function openDocument(id: string, fragment = "", replace = false) {
    setLinkError("");
    if (source) source.activeDocumentID = id;
    void navigate({ to: "/markdown", search: { doc: id }, hash: fragment, replace });
  }

  function choose(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = ""; // selecting the same file again is an explicit refresh
    if (!files.length) return; // cancel keeps the current source and route intact
    try {
      selectionEpoch.current++;
      const next = select(files);
      setSelectionError("");
      setLoaded(null);
      openDocument(next.initialDocument().id);
    } catch (error) {
      setSelectionError(error instanceof Error ? error.message : "Unable to open this selection.");
    }
  }

  const resolveImage = useCallback((href: string) => source && document ? source.image(document.path, href) : undefined, [source, document]);

  return (
    <div ref={viewer} className="external-markdown-viewer">
      <header className="day-head">
        <h1 className="day-title">Markdown viewer</h1>
        <p className="muted">Read local files without importing them into your vault. Files stay in this browser session after opening.</p>
        <div className="external-markdown-actions">
          <button type="button" className="text-button" onClick={() => filesInput.current?.click()}>Open Markdown files</button>
          <button type="button" className="text-button" onClick={() => folderInput.current?.click()}>Open folder</button>
          {source && <button type="button" className="text-button" onClick={() => {
            selectionEpoch.current++;
            clear(); setLoaded(null); setSelectionError(""); setLinkError("");
            void navigate({ to: "/markdown", search: {}, hash: "", replace: true });
          }}>Close source</button>}
        </div>
        <input ref={filesInput} type="file" accept=".md,.markdown" multiple hidden aria-label="Select Markdown files" onChange={choose} />
        <input ref={folderInput} type="file" multiple hidden aria-label="Select Markdown folder" {...{ webkitdirectory: "" }} onChange={choose} />
        {selectionError && <p role="alert" className="error">{selectionError}</p>}
      </header>
      {!source ? (
        <p className="muted">{documentID ? "This source is no longer open. Select the files again." : "Open a README or choose a folder to follow its relative Markdown links and view local images."}</p>
      ) : (
        <>
          <div className="external-markdown-source">
            <p className="muted">External source · Read-only · {source.label}</p>
            <label className="external-markdown-file">File
              <select value={document?.id ?? ""} onChange={(event) => openDocument(event.currentTarget.value)}>
                {!document && <option value="">Choose a Markdown file</option>}
                {source.documents.map((item) => <option key={item.id} value={item.id}>{item.path}</option>)}
              </select>
            </label>
            <p className="muted">Select the files again to refresh disk changes. Full reload closes the source.</p>
          </div>
          {!document ? <p role="status" className="muted">Choose a file above. An older source must be selected again.</p> : loaded?.id !== document.id ? (
            <p role="status" className="muted">Reading {document.path}…</p>
          ) : loaded.error ? <p role="alert" className="error">{loaded.error}</p> : (
            <article ref={article} aria-label={`External Markdown: ${document.path}`}>
              {linkError && <p role="alert" className="error">{linkError}</p>}
              <MarkdownView markdown={loaded.markdown} title={document.path} external={{
                onOpenLink: (href) => {
                  const linked = source.linkedDocument(document.path, href);
                  if (linked) openDocument(linked.document.id, linked.fragment);
                  else setLinkError("That Markdown file is outside the selected source or is unavailable. Open its folder to include it.");
                },
                resolveImage,
              }} />
            </article>
          )}
        </>
      )}
    </div>
  );
}
