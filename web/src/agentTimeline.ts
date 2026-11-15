import { api, APIError } from "./api";
import { qualify, vaultParams } from "./vaultId";

export interface AgentTask {
  id: string; parent_id?: string; title: string; detail?: string;
  status: "pending" | "active" | "paused" | "blocked" | "done" | "retired";
  owner?: string; priority?: number; weight: number; done_minutes: number; remaining_minutes: number;
  mode: "serial" | "parallel";
}
export interface AgentReport {
  id: string; task_id?: string; author: string; period_start: string; period_end: string;
  summary: string; uncertainty?: string; user_action?: string; event_sequences: number[];
}
export interface AgentDiscussion { id: string; target_type: string; target_id: string; author: string; text: string }
export interface AgentControl { id: string; task_id: string; owner: string; action: string; status: string; result?: string }
export interface AgentEvent { sequence: number; at: string; source: string; event_id: string; kind: string; payload: unknown }
export interface AgentProgress { fraction: number; done_minutes: number; remaining_minutes: number; total: number; done: number; retired: number }
export interface AgentProjectSummary { project_id: string; title: string; note_id?: string; revision: number; progress: AgentProgress }
export interface AgentProject extends AgentProjectSummary {
  schema_version: number; description?: string; mode: "serial" | "parallel";
  task_progress?: Record<string, AgentProgress>;
  tasks: AgentTask[]; reports: AgentReport[]; discussions: AgentDiscussion[]; controls: AgentControl[]; events: AgentEvent[];
}
export interface AgentRequest { source: string; event_id: string; expected_revision: number; kind: string; payload: unknown }
export const agentKeys = {
  projects: (vault: string) => ["agent-timeline", vault, "projects"] as const,
  project: (vault: string, id: string) => ["agent-timeline", vault, "project", id] as const,
};
// Only the explicit linked note is a note identity. Discussion targets and event
// payloads belong to the timeline ledger and must round-trip without rewriting.
type WireProject<T> = Omit<T, "note_id"> & { note_id?: number | string; vault?: string };
function linkedNote<T>(value: WireProject<T>, vault: string): T {
  return { ...value, ...(value.note_id === undefined ? {} : { note_id: qualify(value.vault ?? vault, value.note_id) }) } as T;
}
export async function listAgentProjects(vault: string, signal?: AbortSignal) {
  const data = await api<{ projects: WireProject<AgentProjectSummary>[]; vault?: string }>(
    `/api/timeline/projects${vaultParams(vault, "?")}`, { signal, normalizeNoteIDs: false });
  return { ...data, projects: data.projects.map(project => linkedNote<AgentProjectSummary>(project, data.vault ?? vault)) };
}
function checkedProject(value: AgentProject, id: string): AgentProject {
  if (value?.schema_version !== 1 || value.project_id !== id || !Number.isSafeInteger(value.revision) || value.revision < 1 ||
    !value.progress || !Number.isFinite(value.progress.fraction) ||
    ![value.tasks, value.reports, value.discussions, value.controls, value.events].every(Array.isArray)) {
    throw new APIError(502, "Invalid timeline response; reload to verify the saved state");
  }
  return value;
}
export async function getAgentProject(vault: string, id: string, signal?: AbortSignal) {
  return checkedProject(linkedNote<AgentProject>(await api<WireProject<AgentProject>>(`/api/timeline/project?id=${encodeURIComponent(id)}${vaultParams(vault)}`, { signal, normalizeNoteIDs: false }), vault), id);
}
export async function applyAgentRequest(vault: string, id: string, request: AgentRequest) {
  const saved = checkedProject(linkedNote<AgentProject>(await api<WireProject<AgentProject>>(`/api/timeline/project?id=${encodeURIComponent(id)}${vaultParams(vault)}`, { method: "POST", body: request, normalizeNoteIDs: false }), vault), id);
  if (!saved.events.some(e => e.source === request.source && e.event_id === request.event_id && e.kind === request.kind)) {
    throw new APIError(502, "Timeline acknowledgement is missing; retry the same request to verify it");
  }
  return saved;
}
