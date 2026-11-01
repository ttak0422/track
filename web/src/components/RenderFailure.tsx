// Source is escaped text, never active Markdown/task controls. It remains readable
// when sanitization fails without bypassing the server's rendering contract.
export function RenderFailure({ source, retry, retrying }: {
  source: string; retry: () => void; retrying: boolean;
}) {
  return <div>
    <p className="muted">Preview is unavailable. Showing the note source.</p>
    <button type="button" className="note-recovery-action" disabled={retrying} onClick={retry}>
      {retrying ? "Retrying…" : "Retry preview"}
    </button>
    <pre><code>{source}</code></pre>
  </div>;
}
