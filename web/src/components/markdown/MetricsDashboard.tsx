import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useContext, useState } from "react";
import { renderMetricsDashboard, type MetricsDashboardResponse } from "../../api";
import { STATIC_MODE } from "../../runtime";
import { CodeBlock } from "./CodeBlock";
import { NoteVaultContext } from "./context";
import { EChartsBlock } from "./EChartsBlock";

export function MetricsDashboard({ text }: { text: string }) {
  const vault = useContext(NoteVaultContext);
  const [days, setDays] = useState(0);
  const query = useQuery({
    queryKey: ["metrics-dashboard", vault, text, days],
    queryFn: () => renderMetricsDashboard(text, days, vault),
    enabled: !STATIC_MODE,
    retry: false,
    placeholderData: keepPreviousData,
  });
  if (STATIC_MODE) return <CodeBlock lang="json" text={text} />;
  return <section className="metrics-dashboard" aria-label="Metrics dashboard">
    <div className="metrics-dashboard-controls">
      <label>Period <select aria-label="Dashboard period" value={days} onChange={(e) => setDays(Number(e.target.value))}>
        <option value={0}>All observations</option><option value={1}>Last day</option>
        <option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={365}>Last year</option>
      </select></label>
      <button type="button" onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh</button>
      <span role="status">{query.isFetching ? "Updating…" : ""}</span>
    </div>
    {query.isError ? <p role="alert">Dashboard error: {query.error.message}</p> : null}
    {query.data && !query.isError ? <DashboardContents view={query.data} /> : !query.isError ? <p>Loading dashboard…</p> : null}
  </section>;
}

function DashboardContents({ view }: { view: MetricsDashboardResponse }) {
  return <>
      <h2>{view.title}</h2>
      <div className="metrics-dashboard-grid">
        {view.panels.map((panel, i) => <section key={i} className="metrics-dashboard-panel"
          style={{ gridColumn: `${panel.x + 1} / span ${panel.width}` }} aria-label={panel.title || `Panel ${i + 1}`}>
          <p className="metrics-dashboard-meta">{panel.unit ? `Unit: ${panel.unit} · ` : ""}Data: {panel.source}<br />
            Latest observation: {panel.latest_observation || "none"}<br />
            File updated: {panel.data_updated_at || "unknown"} · {panel.points} observations · {panel.missing} missing
          </p>
          {panel.series.map((series, j) => <details key={j} className="metrics-dashboard-meta">
            <summary>{series.entity || series.metric} · {series.frequency || "frequency unknown"} · {series.unit || panel.unit || "unit unknown"}</summary>
            <dl>{Object.entries(series).filter(([, value]) => value).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value}</dd></div>)}</dl>
          </details>)}
          {panel.gaps.length > 0 ? <details className="metrics-dashboard-meta"><summary>{panel.gaps.length} missing observations</summary>
            <ul>{panel.gaps.map((gap, j) => <li key={j}>{gap.time} · {gap.entity}: {gap.reason}</li>)}</ul>
          </details> : null}
          {panel.points === 0 ? <p>No observations in this period.</p> : <EChartsBlock option={panel.echarts} />}
        </section>)}
      </div>
    </>;
}

// The site exporter resolves vault bindings once. A snapshot has no live data endpoint or updates.
export function MetricsSnapshot({ text }: { text: string }) {
  try {
    const view = JSON.parse(text) as MetricsDashboardResponse;
    if (!view || typeof view.title !== "string" || !Array.isArray(view.panels) || !view.panels.every((panel) =>
      panel && typeof panel.title === "string" && typeof panel.source === "string" &&
      [panel.unit, panel.latest_observation, panel.data_updated_at].every((value) => value === undefined || typeof value === "string") &&
      Number.isInteger(panel.points) && panel.points >= 0 && Number.isInteger(panel.missing) && panel.missing >= 0 &&
      Number.isInteger(panel.x) && Number.isInteger(panel.width) && panel.x >= 0 && panel.width >= 1 && panel.x + panel.width <= 24 &&
      Array.isArray(panel.series) && panel.series.every((series) => series && typeof series === "object" && !Array.isArray(series) && Object.values(series).every((value) => typeof value === "string")) &&
      Array.isArray(panel.gaps) && panel.gaps.every((gap) => gap && typeof gap.time === "string" && typeof gap.entity === "string" && typeof gap.reason === "string") &&
      panel.echarts && typeof panel.echarts === "object" && !Array.isArray(panel.echarts)
    )) throw new Error("invalid snapshot");
    return <section className="metrics-dashboard" aria-label="Metrics dashboard snapshot">
      <p className="metrics-dashboard-meta">Published snapshot · observations at export time</p>
      <DashboardContents view={view} />
    </section>;
  } catch { return <CodeBlock lang="json" text={text} />; }
}
