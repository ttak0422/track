import { useContext, useState } from "react";
import { useMetricsDashboardQuery } from "../../queries";
import { STATIC_MODE } from "../../runtime";
import type { MetricsDashboardFilters, MetricsDashboardPanel, MetricsDashboardValue } from "../../types";
import { CodeBlock } from "./CodeBlock";
import { NoteVaultContext } from "./context";
import { EChartsBlock } from "./EChartsBlock";
import "./metrics-dashboard.css";

const emptyFilters: MetricsDashboardFilters = { from: "", to: "", entity: "", metric: "" };

export function MetricsDashboard({ text }: { text: string }) {
  const vault = useContext(NoteVaultContext);
  const [filters, setFilters] = useState(emptyFilters);
  const query = useMetricsDashboardQuery(text, filters, vault);
  const data = query.data;

  if (STATIC_MODE) {
    return (
      <section className="metrics-dashboard">
        <p>Interactive metrics dashboards require the live track workspace.</p>
        <CodeBlock lang="json" text={text} />
      </section>
    );
  }

  return (
    <section className="metrics-dashboard" aria-label={data?.title || "Metrics dashboard"} aria-busy={query.isFetching}>
      <h2>{data?.title || "Metrics dashboard"}</h2>
      <div className="metrics-dashboard-controls">
        <label>From<input type="date" value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} /></label>
        <label>To<input type="date" value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} /></label>
        <label>Target<select value={filters.entity} onChange={(e) => setFilters({ ...filters, entity: e.target.value })}>
          <option value="">All targets</option>
          {(data?.entities ?? []).map((entity) => <option key={entity} value={entity}>{entity}</option>)}
          {filters.entity && !data?.entities.includes(filters.entity) && <option value={filters.entity}>{filters.entity}</option>}
        </select></label>
        <label>Metric<select value={filters.metric} onChange={(e) => setFilters({ ...filters, metric: e.target.value })}>
          <option value="">All metrics</option>
          {(data?.metrics ?? []).map((metric) => <option key={metric} value={metric}>{metric}</option>)}
          {filters.metric && !data?.metrics.includes(filters.metric) && <option value={filters.metric}>{filters.metric}</option>}
        </select></label>
        <button type="button" className="graph-reset" onClick={() => setFilters(emptyFilters)}>Reset</button>
        <button type="button" className="graph-reset" disabled={query.isFetching} onClick={() => void query.refetch()}>Refresh</button>
      </div>
      {query.isFetching && <p role="status">{data ? "Updating dashboard…" : "Loading dashboard…"}</p>}
      {query.isError ? <div role="alert">
        <p>Dashboard error: {query.error.message}</p>
        <CodeBlock lang="json" text={text} />
      </div> : data && !query.isPlaceholderData ? <>
        <p className="metrics-dashboard-asof">Latest sample: {data.asof ? <time dateTime={data.asof}>{data.asof}</time> : "No data"}</p>
        {data.panels.length === 0 && <p>No panels configured.</p>}
        <div className="metrics-dashboard-grid">
          {[...data.panels].sort((a, b) => a.grid.y - b.grid.y || a.grid.x - b.grid.x).map((panel) => (
            <section className="metrics-dashboard-panel" key={panel.id} aria-label={panel.title}
              style={{ gridColumn: `${panel.grid.x + 1} / span ${panel.grid.w}`, gridRow: `${panel.grid.y + 1} / span ${panel.grid.h}` }}>
              <h3>{panel.title}</h3>
              <PanelContent panel={panel} />
            </section>
          ))}
        </div>
      </> : null}
    </section>
  );
}

function thresholdState(value: MetricsDashboardValue): string {
  return value.threshold === undefined ? "Baseline" : `At or above ${value.thresholdDisplay ?? value.threshold}`;
}

function PanelContent({ panel }: { panel: MetricsDashboardPanel }) {
  if (panel.error) return <p role="alert">{panel.error}</p>;
  if (panel.type === "timeseries") {
    return panel.echarts ? <EChartsBlock option={panel.echarts} /> : <p>No data for this selection.</p>;
  }
  if (panel.values.length === 0) return <p>No data for this selection.</p>;
  if (panel.type === "table") return (
    <div className="metrics-dashboard-table"><table>
      <thead><tr><th scope="col">Target</th><th scope="col">Metric</th><th scope="col">Value</th><th scope="col">Sample time</th><th scope="col">Threshold</th></tr></thead>
      <tbody>{panel.values.map((value, index) => <tr key={index}>
        <td>{value.entity || "—"}</td><td>{value.name}</td><td>{value.display}</td>
        <td><time dateTime={value.time}>{value.time}</time></td><td>{thresholdState(value)}</td>
      </tr>)}</tbody>
    </table></div>
  );
  return <dl className="metrics-dashboard-stats">{panel.values.map((value, index) => <div key={index}>
    <dt>{value.name}{value.entity && ` · ${value.entity}`}</dt>
    <dd><strong>{value.display}</strong><span>{thresholdState(value)}</span><time dateTime={value.time}>{value.time}</time></dd>
  </div>)}</dl>;
}
