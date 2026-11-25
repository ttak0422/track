import { useBlocker } from "@tanstack/react-router";
import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIError } from "../api";
import { agentKeys, projectNoteID, applyAgentRequest, getAgentProject, listAgentProjects, type AgentProject, type AgentProgress, type AgentRequest, type AgentTask } from "../agentTimeline";
import { useVaultScope } from "../vaultScope";
import "./AgentTimelineView.css";

const DraftContext = createContext<{ report: (id: string, dirty: boolean) => void; mayLeave: () => boolean }>({ report: (_id: string, _dirty: boolean) => {}, mayLeave: () => true });
const leaveMessage = "Discard unsaved timeline edits? A request already sent may still complete.";
const identity = () => crypto.randomUUID();
const text = (data: FormData, key: string) => String(data.get(key) ?? "").trim();
const requiredText = (data: FormData, key: string, label: string) => {
  const value = text(data, key);
  if (!value) throw new Error(label + " is required");
  return value;
};
const number = (data: FormData, key: string) => {
  const result = Number(text(data, key));
  if (!Number.isFinite(result) || result < 0) throw new Error(`${key} must be a nonnegative number`);
  return result;
};

export function AgentTimelineView() {
  const { scope } = useVaultScope();
  const [vault, setVault] = useState(scope);
  const protectedDraft = useRef(false);
  const reportDraft = useCallback((dirty: boolean) => { protectedDraft.current = dirty; }, []);
  useEffect(() => {
    if (scope !== vault && (!protectedDraft.current || window.confirm(leaveMessage))) setVault(scope);
  }, [scope, vault]);
  return <>
    {scope !== vault ? <p className="muted">Still editing vault {vault || "default"}.
      <button className="timeline-text-button" type="button" onClick={() => {
        if (!protectedDraft.current || window.confirm(leaveMessage)) setVault(scope);
      }}>Switch to selected vault</button></p> : null}
    <TimelineWorkspace key={vault} vault={vault} reportDraft={reportDraft} />
  </>;
}

function TimelineWorkspace({ vault, reportDraft }: { vault: string; reportDraft: (dirty: boolean) => void }) {
  const [selected, setSelected] = useState("");
  const cache = useQueryClient();
  const drafts = useRef(new Set<string>());
  const [hasDraft, setHasDraft] = useState(false);
  const report = useCallback((key: string, dirty: boolean) => {
    if (dirty) drafts.current.add(key); else drafts.current.delete(key);
    setHasDraft(drafts.current.size > 0);
    reportDraft(drafts.current.size > 0);
  }, [reportDraft]);
  const mayLeave = () => drafts.current.size === 0 || window.confirm(leaveMessage);
  useBlocker({ shouldBlockFn: () => !mayLeave(), enableBeforeUnload: hasDraft });
  const projects = useQuery({ queryKey: agentKeys.projects(vault), queryFn: ({ signal }) => listAgentProjects(vault, signal), refetchInterval: 30_000 });
  return <DraftContext.Provider value={{ report, mayLeave }}><div className="agent-timeline">
    <header className="day-head"><h1 className="day-title">Agent timeline</h1></header>
    <p>Follow the plan and catch up on progress.</p>
    {projects.isError ? <p role="alert" className="error">{projects.error.message}</p> : null}
    {projects.isPending ? <p role="status">Loading projects...</p> : null}
    <label>Project <select value={selected} onChange={(e) => { if (mayLeave()) setSelected(e.target.value); }}>
      <option value="">Choose a project</option>
      {(projects.data?.projects ?? []).map((p) => <option value={p.project_id} key={p.project_id}>{p.title}</option>)}
    </select></label>
    <details><summary>New project</summary>
      <TimelineForm revision={0} kind="project.create" build={(d) => ({ title: text(d, "title"), description: text(d, "description"), mode: text(d, "mode") })}
        send={async (request, d) => {
          const id = text(d, "project_id");
          const result = await applyAgentRequest(vault, id, request);
          await cache.cancelQueries({ queryKey: agentKeys.project(vault, id) });
          cache.setQueryData<AgentProject>(agentKeys.project(vault, id), current => current && current.revision > result.revision ? current : result);
          await cache.invalidateQueries({ queryKey: agentKeys.projects(vault) });
          return result.revision;
        }} label="Create project">
        <Field name="project_id" label="Project ID" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,95}" />
        <Field name="title" label="Title" required maxLength={512} />
        <Area name="description" label="Description" />
        <Mode />
      </TimelineForm>
    </details>
    {selected ? <ProjectWorkspace key={selected} vault={vault} id={selected} /> : <p className="muted">Choose a project or create one to begin.</p>}
  </div></DraftContext.Provider>;
}

function ProjectWorkspace({ vault, id }: { vault: string; id: string }) {
  const cache = useQueryClient();
  const query = useQuery({ queryKey: agentKeys.project(vault, id), queryFn: ({ signal }) => getAgentProject(vault, id, signal), refetchInterval: 10_000 });
  const [editing, setEditing] = useState<AgentTask | null>(null);
  const [editingRevision, setEditingRevision] = useState(0);
  const [eventLimit, setEventLimit] = useState(100);
  const { mayLeave } = useContext(DraftContext);
  const send = async (request: AgentRequest) => {
    try {
      const saved = await applyAgentRequest(vault, id, request);
      // Never write into whichever project happens to be selected when this resolves.
      await cache.cancelQueries({ queryKey: agentKeys.project(vault, id) });
      cache.setQueryData<AgentProject>(agentKeys.project(vault, id), current => current && current.revision > saved.revision ? current : saved);
      void cache.invalidateQueries({ queryKey: agentKeys.projects(vault) });
      return saved.revision;
    } catch (error) {
      if (error instanceof APIError && error.status === 409) await query.refetch();
      throw error;
    }
  };
  const p = query.data;
  return <section aria-label="Project timeline">
    {query.isPending ? <p role="status">Loading project...</p> : null}
    {query.isError ? <p role="alert" className="error">{query.error.message}{p ? " Showing an earlier snapshot; it may be stale." : ""}</p> : null}
    {p ? <>
      <TimelineOverview project={p} onEdit={task => { if (mayLeave()) { setEditing(task); setEditingRevision(p.revision); } }} />
      <details><summary>Edit project</summary>
        <TimelineForm revision={p.revision} stickyRevision kind="project.update" send={send} label="Save project" build={d => ({
          title: text(d, "title"), description: text(d, "description"), mode: text(d, "mode"),
          ...(p.note_id ? { note_id: projectNoteID(p.note_id) } : {}),
        })}>
          <Field name="title" label="Project title" value={p.title} required maxLength={512} />
          <Area name="description" label="Project description" value={p.description} /><Mode value={p.mode} />
        </TimelineForm>
      </details>
      <details open={editing !== null}><summary>{editing ? `Edit ${editing.title}` : "Add task"}</summary>
        <TimelineForm key={editing ? `${editing.id}:${editingRevision}` : "new"} revision={p.revision} stickyRevision={editing !== null} kind="task.upsert" send={send} label={editing ? "Save task" : "Create task"} resetAfterSave={!editing} build={(d) => ({
          id: editing?.id ?? (text(d, "id") || identity()), title: requiredText(d, "title", "Task title"), detail: text(d, "detail"), parent_id: text(d, "parent_id"),
          status: text(d, "status"), owner: text(d, "owner"), mode: text(d, "mode"),
          ...(text(d, "priority") !== "" ? { priority: number(d, "priority") } : {}),
          weight: number(d, "weight"), done_minutes: number(d, "done_minutes"), remaining_minutes: number(d, "remaining_minutes"),
        })}>
          <Field name="title" label="Task title" value={editing?.title} required maxLength={512} />
          <details><summary>Task options</summary><div className="timeline-options">
          <Field name="id" label="Task ID" value={editing?.id} pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,95}" readOnly={editing !== null} />
          <Area name="detail" label="Task detail" value={editing?.detail} />
          <label>Parent <select name="parent_id" defaultValue={editing?.parent_id ?? ""}><option value="">Project root</option>{p.tasks.filter(t => t.id !== editing?.id).map(t => <option key={t.id} value={t.id}>{t.title}</option>)}</select></label>
          <label>Status <select name="status" defaultValue={editing?.status ?? "pending"}>{["pending", "active", "paused", "blocked", "done", "retired"].map(s => <option key={s}>{s}</option>)}</select></label>
          <Field name="owner" label="Owner/source ID" value={editing?.owner} pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,95}" />
          <label>Priority <select name="priority" defaultValue={editing?.priority ?? ""}><option value="">Unprioritized</option>{[0, 1, 2, 3].map(n => <option key={n} value={n}>P{n}</option>)}</select></label>
          <Field name="weight" label="Weight" type="number" min="0.001" step="any" value={editing?.weight ?? 1} required />
          <Field name="done_minutes" label="Recorded minutes" type="number" min="0" step="any" value={editing?.done_minutes ?? 0} required />
          <Field name="remaining_minutes" label="Remaining minutes" type="number" min="0" step="any" value={editing?.remaining_minutes ?? 0} required />
          <Mode value={editing?.mode} />
          </div></details>
        </TimelineForm>
        {editing ? <button className="timeline-text-button" type="button" onClick={() => { if (mayLeave()) setEditing(null); }}>Finish editing</button> : null}
      </details>
      <DiscussionSection project={p} send={send} />
      <details className="timeline-history"><summary>History &amp; advanced tools</summary>
      <ReportSection project={p} send={send} />
      <ControlSection project={p} send={send} />
      <details><summary>Recorded events ({p.events.length})</summary>
        <p className="muted">Showing the latest {Math.min(eventLimit, p.events.length)} records in sequence order.</p>
        {p.events.length > eventLimit ? <button className="timeline-text-button" type="button" onClick={() => setEventLimit(eventLimit + 100)}>Show earlier records</button> : null}
        <ol className="agent-events">{p.events.slice(-eventLimit).map(e => <li key={e.sequence}>
          <time>{e.at}</time> · #{e.sequence} · {e.source} · {e.kind}
          <details><summary>Payload</summary><pre>{JSON.stringify(e.payload, null, 2)}</pre></details>
        </li>)}</ol>
      </details>
      </details>
    </> : null}
  </section>;
}

const taskStatus: Record<AgentTask["status"], { glyph: string; label: string }> = {
  done: { glyph: "✓", label: "Done" },
  active: { glyph: "●", label: "In progress" },
  pending: { glyph: "○", label: "To do" },
  paused: { glyph: "Ⅱ", label: "Paused" },
  blocked: { glyph: "!", label: "Blocked" },
  retired: { glyph: "–", label: "Retired" },
};

function recentRows(project: AgentProject) {
  const lastStatus = new Map<string, AgentTask["status"]>();
  const lastNote = new Map<string, string>();
  const rows: { sequence: number; at: string; text: string; type: "Report" | "Task" | "Note"; covered: number[] }[] = [];
  for (const event of project.events) {
    if (!event.payload || typeof event.payload !== "object") continue;
    const p = event.payload as Record<string, unknown>;
    if (event.kind === "task.upsert" && typeof p.id === "string" && typeof p.title === "string"
        && typeof p.status === "string" && Object.hasOwn(taskStatus, p.status)) {
      const status = p.status as AgentTask["status"];
      const previous = lastStatus.get(p.id);
      lastStatus.set(p.id, status);
      if (previous && previous !== status) {
        rows.push({ ...event, type: "Task", text: taskStatus[status].label + ": " + p.title, covered: [] });
      }
    } else if (event.kind === "report.add" && typeof p.summary === "string") {
      const covered = Array.isArray(p.event_sequences) ? p.event_sequences.filter((n): n is number => typeof n === "number") : [];
      rows.push({ ...event, type: "Report", text: p.summary, covered });
    } else if (event.kind === "discussion.add" && typeof p.text === "string") {
      if (event.source === "interval-progress-v1") {
        const id = event.event_id.match(/^progress-(result|blocker|next)-.+$/);
        if (!id || p.id !== event.event_id || p.target_type !== "task" || typeof p.target_id !== "string") continue;
        const key = p.target_id + ":" + id[1];
        if (lastNote.get(key) === p.text) continue;
        lastNote.set(key, p.text);
      }
      rows.push({ ...event, type: "Note", text: p.text, covered: [] });
    }
  }
  const visible: typeof rows = [];
  const covered = new Set<number>();
  // Traverse newest first. Only a visible report suppresses its older evidence.
  for (let i = rows.length - 1; i >= 0 && visible.length < 5; i--) {
    const row = rows[i];
    if (covered.has(row.sequence)) continue;
    visible.push(row);
    for (const sequence of row.covered) covered.add(sequence);
  }
  return visible;
}

function TimelineOverview({ project, onEdit }: { project: AgentProject; onEdit: (t: AgentTask) => void }) {
  const progress = Math.round(Math.max(0, Math.min(1, project.progress.fraction)) * 100);
  const recent = recentRows(project);
  return <div className="timeline-overview">
    <section aria-label="Plan" className="timeline-plan">
      <div className="timeline-plan-title"><h2>{project.title}</h2><strong>{progress}%</strong></div>
      <progress className="timeline-progress" aria-label="Weighted project progress" max={100} value={progress} />
      {project.description ? <p className="muted">{project.description}</p> : null}
      <TaskTree tasks={project.tasks} progress={project.task_progress} onEdit={onEdit} />
      <small className="muted" title="An active task may have stopped without reporting.">Task states are last reported records, not live process status.</small>
    </section>
    <section aria-label="Recent updates" className="timeline-recent">
      <h3>Recent updates</h3>
      {recent.length ? <ol>{recent.map(row => {
        const when = new Date(row.at);
        const time = Number.isNaN(when.getTime()) ? row.at : when.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
        return <li key={row.sequence}>
          <time dateTime={row.at} title={row.at}>{time}</time>
          <div><span className="timeline-update-kind">{row.type}</span><p>{row.text}</p></div>
        </li>;
      })}</ol> : <p className="muted">No updates yet.</p>}
    </section>
  </div>;
}

function TaskTree({ tasks, progress, onEdit }: { tasks: AgentTask[]; progress?: Record<string, AgentProgress>; onEdit: (t: AgentTask) => void }) {
  const byParent = new Map<string, AgentTask[]>();
  for (const task of tasks) {
    const parent = task.parent_id ?? "";
    const siblings = byParent.get(parent) ?? [];
    siblings.push(task);
    byParent.set(parent, siblings);
  }
  const children = (parent = ""): ReactNode => <ul>{byParent.get(parent)?.map(t => <li key={t.id}>
    <div className="timeline-task-row">
      <span className={"timeline-task-mark is-" + t.status} title={taskStatus[t.status].label}>
        <span aria-hidden="true">{taskStatus[t.status].glyph}</span><span className="timeline-sr-only">{taskStatus[t.status].label}</span>
      </span>
      <button className="timeline-text-button" type="button" onClick={() => onEdit(t)}>{t.title}</button>
      {progress?.[t.id] ? <small className="muted timeline-task-progress" title={progress[t.id].remaining_minutes + " min remaining"}>{Math.round(progress[t.id].fraction * 100)}%<span className="timeline-sr-only"> subtree · {progress[t.id].remaining_minutes} min remaining</span></small> : null}
    </div>
    {byParent.has(t.id) ? children(t.id) : null}
  </li>)}</ul>;
  return tasks.length ? <div className="timeline-task-tree" aria-label="Task tree">{children()}</div> : <p className="muted">No tasks yet.</p>;
}

type Send = (request: AgentRequest, data: FormData) => Promise<number | void>;
function ReportSection({ project: p, send }: { project: AgentProject; send: Send }) {
  return <section><h3>Reports</h3>
    {p.reports.map(r => <article key={r.id}><h4>{r.id}</h4><p className="muted">{r.author} · {r.period_start} to {r.period_end} · {r.task_id || "Project"}</p>
      <p>{r.summary}</p>{r.uncertainty ? <p>Uncertainty: {r.uncertainty}</p> : null}{r.user_action ? <p>User action: {r.user_action}</p> : null}
      <p className="muted">Source events: {r.event_sequences.join(", ")}</p></article>)}
    <details><summary>Add authored report</summary><p>Write your own summary. Track does not call a model or collect conversations.</p>
      <TimelineForm revision={p.revision} kind="report.add" send={send} label="Add report" build={d => {
        const sequences = text(d, "events").split(",").map(s => Number(s.trim()));
        if (!sequences.length || sequences.some(n => !Number.isSafeInteger(n) || n < 1)) throw new Error("Enter source event numbers separated by commas");
        return { id: identity(), task_id: text(d, "task_id"), author: text(d, "author"), period_start: text(d, "start"), period_end: text(d, "end"), summary: text(d, "summary"), uncertainty: text(d, "uncertainty"), user_action: text(d, "user_action"), event_sequences: sequences };
      }}>
        <TaskSelect tasks={p.tasks} optional />
        <Field name="author" label="Report author" required maxLength={256} />
        <Field name="start" label="Period start (RFC3339)" placeholder="2026-11-15T00:00:00Z" required />
        <Field name="end" label="Period end (RFC3339)" placeholder="2026-11-15T01:00:00Z" required />
        <Field name="events" label="Source event numbers" placeholder="1,2,3" required />
        <Area name="summary" label="Summary" required /><Area name="uncertainty" label="Uncertainty" /><Area name="user_action" label="User action" />
      </TimelineForm>
    </details>
  </section>;
}

function DiscussionSection({ project: p, send }: { project: AgentProject; send: Send }) {
  const taskTitles = new Map(p.tasks.map(t => [t.id, t.title]));
  return <details className="timeline-discussion"><summary>Discussion</summary>
    <TimelineForm revision={p.revision} kind="discussion.add" send={send} label="Post message" resetAfterSave build={d => {
      const [target_type, target_id] = text(d, "target").split(":");
      return { id: identity(), target_type, target_id, author: text(d, "author") || "Web", text: requiredText(d, "text", "Message") };
    }}>
      <Area name="text" label="Message" required />
      <details><summary>Message options</summary><div className="timeline-options">
        <label>Target <select name="target"><option value={`project:${p.project_id}`}>Project</option>{p.tasks.map(t => <option key={`task:${t.id}`} value={`task:${t.id}`}>Task: {t.title}</option>)}{p.reports.map(r => <option key={`report:${r.id}`} value={`report:${r.id}`}>Report: {r.summary}</option>)}</select></label>
        <Field name="author" label="Display name (optional)" placeholder="Web" maxLength={256} />
      </div></details>
    </TimelineForm>
    <ul className="timeline-thread">{p.discussions.slice().reverse().map(d => <li key={d.id}>
      <strong>{d.author}</strong>{d.target_type === "task" ? <small className="muted"> · {taskTitles.get(d.target_id) ?? "Task"}</small> : null}
      <p>{d.text}</p>
    </li>)}</ul>
  </details>;
}

function ControlSection({ project: p, send }: { project: AgentProject; send: Send }) {
  return <section><h3>Cooperative controls</h3><p>A request is not an acknowledgement or a stopped process. Only the routed owner can report a receipt or result.</p>
    {p.controls.map(c => <p key={c.id}>{c.task_id}: {c.action} → {c.owner} · {c.status}{c.result ? ` · ${c.result}` : ""}</p>)}
    <details><summary>Request pause or resume</summary><TimelineForm revision={p.revision} kind="control.request" send={send} label="Send request" build={d => {
      const task_id = text(d, "task_id"), task = p.tasks.find(t => t.id === task_id);
      if (!task?.owner) throw new Error("Choose a task with an assigned owner");
      return { id: identity(), task_id, owner: task.owner, action: text(d, "action") };
    }}>
      <TaskSelect tasks={p.tasks.filter(t => t.owner)} />
      <label>Action <select name="action"><option>pause</option><option>resume</option></select></label>
    </TimelineForm></details>
  </section>;
}

// A form owns its source event identity through uncertain retries. It never silently
// rebases a dirty edit onto a newer project revision or retries a conflict automatically.
export function TimelineForm({ revision, kind, build, send, label, children, stickyRevision = false, resetAfterSave = false }: {
  revision: number; stickyRevision?: boolean; resetAfterSave?: boolean; kind: string; build: (data: FormData) => unknown; send: Send; label: string; children: ReactNode;
}) {
  const [base, setBase] = useState(revision), [dirty, setDirty] = useState(false);
  const [pending, setPending] = useState(false), [error, setError] = useState(""), [success, setSuccess] = useState(false);
  const [conflict, setConflict] = useState(false);
  const formID = useId();
  const { report } = useContext(DraftContext);
  useEffect(() => { report(formID, dirty || pending); return () => report(formID, false); }, [report, formID, dirty, pending]);
  const request = useRef<AgentRequest | null>(null);
  const busy = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => { if (!stickyRevision && !dirty && !pending) setBase(revision); }, [revision, dirty, pending, stickyRevision]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (busy.current || conflict) return;
    busy.current = true;
    const form = event.currentTarget;
    const data = new FormData(form);
    setError(""); setSuccess(false);
    try {
      request.current ??= { source: "web", event_id: identity(), expected_revision: base, kind, payload: build(data) };
      setPending(true);
      const savedRevision = await send(request.current, data);
      if (!alive.current) return;
      if (resetAfterSave) form.reset();
      request.current = null; if (typeof savedRevision === "number") setBase(savedRevision); setDirty(false); setSuccess(true);
    } catch (e) {
      if (!alive.current) return;
      setError(e instanceof Error ? e.message : "Request failed");
      if (e instanceof APIError && e.status === 409) setConflict(true);
    } finally { busy.current = false; if (alive.current) setPending(false); }
  }
  return <form className="agent-form" onSubmit={submit} onChange={() => { setDirty(true); setSuccess(false); setConflict(false); setError(""); request.current = null; }}>
    <fieldset disabled={pending}>{children}</fieldset>
    {dirty && revision !== base ? <p className="muted">Project changed since editing began (revision {base} → {revision}). Review before submitting.</p> : null}
    {error ? <p className="error" role="alert">{error}</p> : null}
    {conflict ? <button type="button" className="timeline-text-button" onClick={() => { setBase(revision); request.current = null; setConflict(false); setError(""); }}>Use refreshed revision after review</button> : null}
    {success ? <p role="status">Saved.</p> : null}
    <button type="submit" className="timeline-text-button" disabled={pending || conflict || success}>{pending ? "Saving..." : label}</button>
  </form>;
}

function Field({ label, value, ...props }: { label: string; value?: string | number } & Omit<React.InputHTMLAttributes<HTMLInputElement>, "value">) {
  return <label>{label}<input {...props} defaultValue={value} /></label>;
}
function Area({ label, value, ...props }: { label: string; value?: string } & Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, "value">) {
  return <label>{label}<textarea {...props} defaultValue={value} maxLength={16384} /></label>;
}
function Mode({ value = "serial" }: { value?: string }) {
  return <label>Scheduling <select name="mode" defaultValue={value}><option>serial</option><option>parallel</option></select></label>;
}
function TaskSelect({ tasks, optional = false }: { tasks: AgentTask[]; optional?: boolean }) {
  return <label>Task <select name="task_id" required={!optional}><option value="">{optional ? "Whole project" : "Choose task"}</option>{tasks.map(t => <option key={t.id} value={t.id}>{t.title}</option>)}</select></label>;
}
