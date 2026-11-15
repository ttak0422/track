import { api, APIError } from "./api";
import { vaultParams } from "./vaultId";

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
export interface AgentProjectSummary { project_id: string; title: string; revision: number; progress: AgentProgress }
export interface AgentProject extends AgentProjectSummary {
  schema_version: number; note_id?: string; description?: string; mode: "serial" | "parallel";
  tasks: AgentTask[]; reports: AgentReport[]; discussions: AgentDiscussion[]; controls: AgentControl[]; events: AgentEvent[];
}
export interface AgentRequest { source: string; event_id: string; expected_revision: number; kind: string; payload: unknown }
export const agentKeys = {
  projects: (vault: string) => ["agent-timeline", vault, "projects"] as const,
  project: (vault: string, id: string) => ["agent-timeline", vault, "project", id] as const,
};
export function listAgentProjects(vault: string, signal?: AbortSignal) {
  return api<{ projects: AgentProjectSummary[] }>(`/api/timeline/projects${vaultParams(vault, "?")}`, { signal });
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
  return checkedProject(await api<AgentProject>(`/api/timeline/project?id=${encodeURIComponent(id)}${vaultParams(vault)}`, { signal }), id);
}
export async function applyAgentRequest(vault: string, id: string, request: AgentRequest) {
  const saved = checkedProject(await api<AgentProject>(`/api/timeline/project?id=${encodeURIComponent(id)}${vaultParams(vault)}`, { method: "POST", body: request }), id);
  if (!saved.events.some(e => e.source === request.source && e.event_id === request.event_id && e.kind === request.kind)) {
    throw new APIError(502, "Timeline acknowledgement is missing; retry the same request to verify it");
  }
  return saved;
}
