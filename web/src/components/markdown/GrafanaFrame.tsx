import { useContext, useState } from "react";
import { CodeBlock } from "./CodeBlock";
import { NoteIdentityContext } from "./context";

// A note cannot authorize a network connection. Each mounted frame starts stopped; the reader
// explicitly admits its exact origin. Same-origin and loopback frames never receive script+origin.
export function grafanaURL(value: unknown, appOrigin = window.location.origin): URL | null {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value);
    const host = url.hostname.toLowerCase().replace(/\.$/, "");
    if (url.protocol !== "https:" || url.username || url.password || url.hash || url.origin === appOrigin ||
      host === "localhost" || host.endsWith(".localhost") || host === "[::1]" || host === "[::]" || host.startsWith("[::ffff:7f") || host === "[::ffff:0:0]" || /^127\./.test(host) || host === "0.0.0.0" ||
      !/\/(?:d|d-solo)\/[^/]+\/[^/]+\/?$/.test(url.pathname)) return null;
    const params = new Set(["from", "to", "orgId", "panelId", "theme", "kiosk", "fullscreen", "refresh"]);
    for (const key of url.searchParams.keys()) if (!params.has(key) && !key.startsWith("var-")) return null;
    return url;
  } catch { return null; }
}

export function GrafanaFrame({ text }: { text: string }) {
  const documentIdentity = useContext(NoteIdentityContext);
  return <GrafanaSession key={JSON.stringify([documentIdentity, text])} text={text} />;
}

function GrafanaSession({ text }: { text: string }) {
  let config: { url?: unknown; title?: unknown } = {};
  try { config = JSON.parse(text) ?? {}; } catch { /* source fallback below */ }
  const url = grafanaURL(config.url);
  const [admittedURL, setAdmittedURL] = useState("");
  const [attempt, setAttempt] = useState(0);
  if (!url) return <div><p>Grafana embed needs an external HTTPS /d/ or /d-solo/ URL without credentials or token parameters.</p><CodeBlock lang="json" text={text} /></div>;
  const title = typeof config.title === "string" ? config.title : "Grafana dashboard";
  const active = admittedURL === url.href;
  return <section className="grafana-frame" aria-label={title}>
    <p>{title} · {url.origin}</p>
    <div className="metrics-dashboard-controls">
      <button type="button" onClick={() => setAdmittedURL(active ? "" : url.href)}>{active ? "Disconnect Grafana" : `Load Grafana from ${url.origin}`}</button>
      {active ? <button type="button" onClick={() => setAttempt((n) => n + 1)}>Reconnect</button> : null}
      <a className="md-link" href={url.href} target="_blank" rel="noopener noreferrer">Open Grafana externally</a>
    </div>
    {active ? <>
      <iframe key={`${url.href}:${attempt}`} src={url.href} title={title} sandbox="allow-scripts allow-same-origin" referrerPolicy="no-referrer" />
      <p className="metrics-dashboard-meta">If the frame stays blank or sign-in fails, open Grafana externally. Grafana permissions, framing policy and browser cookie restrictions still apply.</p>
    </> : null}
  </section>;
}
