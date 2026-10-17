import { useEffect, useRef, useState } from "react";
import { DRAWIO_ORIGIN, DRAWIO_URL, MAX_DRAWIO_XML, validDrawioXML } from "./markdown/drawioEditing";

interface Props {
  xml: string;
  onApply: (xml: string) => boolean;
  onClose: () => void;
}

// Each mount owns a fresh browsing context. Source-window checks therefore also
// reject delayed messages from an earlier diagram, even on the same origin.
export function DrawioEditorDialog({ xml, onApply, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const [started, setStarted] = useState(false);
  const [ready, setReady] = useState(false);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");
  const settled = useRef(false);
  const initialized = useRef(false);
  const loaded = useRef(false);
  const request = useRef<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const callbacks = useRef({ onApply, onClose });
  callbacks.current = { onApply, onClose };

  function close() {
    // Cross-origin focus/IME and pending autosaves are opaque to Track. Protect
    // the entire online session rather than trust a possibly delayed dirty bit.
    if (!started || window.confirm("Discard unapplied draw.io changes and close the editor?")) {
      settled.current = true;
      callbacks.current.onClose();
    }
  }

  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement;
    element?.showModal();
    return () => {
      element?.close();
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);

  useEffect(() => {
    if (!started) return;
    timer.current = setTimeout(() => setError("The online editor did not respond. Check your connection, then cancel and reopen. Your note is unchanged."), 30000);
    const receive = (event: MessageEvent) => {
      if (settled.current || event.origin !== DRAWIO_ORIGIN || event.source !== frame.current?.contentWindow ||
          typeof event.data !== "string" || event.data.length > MAX_DRAWIO_XML * 8) return;
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (!message || typeof message !== "object" || Array.isArray(message)) return;
      if (message.event === "init" && !initialized.current) {
        initialized.current = true;
        frame.current?.contentWindow?.postMessage(JSON.stringify({ action: "load", xml, autosave: 1, title: "Track diagram — Save applies to draft" }), DRAWIO_ORIGIN);
      } else if (message.event === "load" && initialized.current && !loaded.current) {
        loaded.current = true;
        clearTimeout(timer.current);
        setReady(true);
        setError("");
      } else if (loaded.current && message.event === "save" && !request.current) {
        apply(message.xml);
      } else if (loaded.current && message.event === "export" && request.current &&
                 message.format === "xml" && message.message?.trackRequest === request.current) {
        clearTimeout(timer.current);
        request.current = null;
        setApplying(false);
        apply(message.xml);
      } else if (message.event === "exit") {
        // Exit can be sent by Escape inside the iframe too.
        if (window.confirm("Discard unapplied draw.io changes and close the editor?")) {
          settled.current = true;
          callbacks.current.onClose();
        }
      }
    };
    function apply(value: unknown) {
      if (!validDrawioXML(value)) {
        setError("The editor returned invalid or oversized diagram XML (maximum 4 MiB of text). The draft is unchanged.");
        return;
      }
      if (callbacks.current.onApply(value)) settled.current = true;
      else setError("The note draft changed while this diagram was open. Cancel and reopen the diagram to edit the current draft.");
    }
    window.addEventListener("message", receive);
    return () => {
      clearTimeout(timer.current);
      window.removeEventListener("message", receive);
    };
  }, [started, xml]);

  function applyCurrent() {
    if (!ready || request.current) return;
    const id = crypto.randomUUID();
    request.current = id;
    setApplying(true);
    setError("");
    // Request the current full document, not the last debounced autosave or a
    // first-page SVG. Correlation prevents late exports satisfying a later Apply.
    frame.current?.contentWindow?.postMessage(JSON.stringify({ action: "export", format: "xml", trackRequest: id }), DRAWIO_ORIGIN);
    timer.current = setTimeout(() => {
      request.current = null;
      setApplying(false);
      setError("The editor did not return XML. Try Apply again or cancel. The draft is unchanged.");
    }, 15000);
  }

  return <dialog ref={dialog} className="modal-card drawio-editor-dialog" aria-labelledby="drawio-editor-title"
    onCancel={(event) => { event.preventDefault(); close(); }}>
    <div className="drawio-editor-header">
      <h3 id="drawio-editor-title">Edit draw.io diagram</h3>
      <button className="drawio-editor-button" type="button" onClick={close}>Close</button>
    </div>
    <p className="muted">Online editor: https://embed.diagrams.net. Opening it shares this diagram’s XML with third-party code from diagrams.net. Other note text is not sent. Internet access is required.</p>
    <p className="muted">Apply to draft (or Save inside the editor), then use Track’s Save to save the note. Cancel discards unapplied changes.</p>
    {error ? <p className="error" role="alert">{error}</p> : null}
    {started ? <iframe ref={frame} title="Online draw.io editor" src={DRAWIO_URL}
      sandbox="allow-scripts allow-same-origin" referrerPolicy="no-referrer" /> :
      <button className="drawio-editor-button" type="button" disabled={!validDrawioXML(xml)} onClick={() => setStarted(true)}>Open online editor</button>}
    {!started && !validDrawioXML(xml) ? <p className="error">This diagram is invalid or exceeds the 4 MiB text limit. Edit its XML in the note source.</p> : null}
    {started && !ready && !error ? <p role="status">Loading online editor…</p> : null}
    <div className="modal-actions">
      <button type="button" onClick={close}>Cancel</button>
      <button type="button" disabled={!ready || applying} onClick={applyCurrent}>{applying ? "Applying…" : "Apply to draft"}</button>
    </div>
  </dialog>;
}
