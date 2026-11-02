// Source is escaped text, never active Markdown/task controls. It remains readable
// when sanitization fails without bypassing the server's rendering contract.
export function RenderFailure({ source, retry, retrying, retained = false }: {
  source: string; retry: () => void; retrying: boolean; retained?: boolean;
}) {
  return <div>
    <p className="muted">{retained ? "Could not refresh the preview. Showing the last rendered preview; it may be stale." : "Preview is unavailable. Showing the note source."}</p>
    <button type="button" className="note-recovery-action" disabled={retrying} onClick={retry}>
      {retrying ? "Retrying…" : "Retry preview"}
    </button>
    {!retained && <pre><code>{source}</code></pre>}
  </div>;
}
