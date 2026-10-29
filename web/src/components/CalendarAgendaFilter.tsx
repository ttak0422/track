import { STATIC_MODE } from "../runtime";

export type AgendaContent = "all" | "events" | "tasks";

// Text-control variant, shared by relative agendas and date pages. This is only
// a view choice; no note, event, or completion state is written by switching it.
export function CalendarAgendaFilter({ value, onChange }: { value: AgendaContent; onChange: (value: AgendaContent) => void }) {
  if (STATIC_MODE) return null;
  return <div className="calendar-nav calendar-views" role="group" aria-label="Agenda contents">
    <span className="muted">Show</span>
    {([["all", "All"], ["events", "Events"], ["tasks", "Tasks"]] as const).map(([mode, label]) =>
      <button key={mode} type="button" aria-pressed={value === mode} onClick={() => onChange(mode)}>
        <span aria-hidden="true">{value === mode ? "✓ " : ""}</span>{label}
      </button>)}
  </div>;
}
