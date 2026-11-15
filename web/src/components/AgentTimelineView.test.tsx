import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError } from "../api";
import { applyAgentRequest, getAgentProject, listAgentProjects, type AgentProject, type AgentRequest } from "../agentTimeline";
import { AgentTimelineView, TimelineForm } from "./AgentTimelineView";

vi.mock("@tanstack/react-router", () => ({ useBlocker: vi.fn() }));

vi.mock("../agentTimeline", async original => ({
  ...(await original<typeof import("../agentTimeline")>()),
  applyAgentRequest: vi.fn(), getAgentProject: vi.fn(), listAgentProjects: vi.fn(),
}));
beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

const project: AgentProject = {
  schema_version: 1, project_id: "alpha", title: "Alpha", revision: 2, mode: "serial",
  tasks: [{ id: "task-a", title: "Work", status: "active", owner: "worker", weight: 1, done_minutes: 2, remaining_minutes: 8, mode: "serial" }],
  task_progress: { "task-a": { fraction: 0.2, total: 1, done: 0, retired: 0, done_minutes: 2, remaining_minutes: 8 } },
  reports: [], discussions: [], controls: [{ id: "control", task_id: "task-a", owner: "worker", action: "pause", status: "requested" }],
  events: [{ sequence: 1, at: "2026-11-15T00:00:00Z", source: "web", event_id: "create", kind: "project.create", payload: { title: "Alpha" } }],
  progress: { fraction: 0.2, total: 1, done: 0, retired: 0, done_minutes: 2, remaining_minutes: 8 },
};
function provider() { return new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } }); }

function form(send: (request: AgentRequest, data: FormData) => Promise<void>, revision = 2) {
  return <TimelineForm revision={revision} kind="discussion.add" build={d => ({ text: d.get("text") })} send={send} label="Send">
    <label>Message<input name="text" /></label>
  </TimelineForm>;
}

describe("TimelineForm", () => {
  it("reuses the exact event identity after an uncertain failure and prevents a duplicate submit", async () => {
    const send = vi.fn().mockRejectedValueOnce(new Error("Network lost")).mockResolvedValueOnce(undefined);
    render(form(send));
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "hello" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Network lost");
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Saved.");
    expect(send.mock.calls[0][0]).toEqual(send.mock.calls[1][0]);
    expect(send.mock.calls[0][0]).toMatchObject({ source: "web", expected_revision: 2 });
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("does not rebase a dirty form or automatically retry a revision conflict", async () => {
    const send = vi.fn().mockRejectedValueOnce(new APIError(409, "Revision conflict")).mockResolvedValueOnce(undefined);
    const view = render(form(send));
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "hello" } });
    view.rerender(form(send, 3));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Revision conflict");
    expect(send.mock.calls[0][0].expected_revision).toBe(2);
    expect(send).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Use refreshed revision after review" }));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByRole("status");
    expect(send.mock.calls[1][0].expected_revision).toBe(3);
    expect(send.mock.calls[1][0].event_id).not.toBe(send.mock.calls[0][0].event_id);
  });

  it("keeps edit-form provenance even before its first keystroke", async () => {
    const send = vi.fn().mockResolvedValue(undefined);
    const editor = (revision: number) => <TimelineForm stickyRevision revision={revision} kind="task.upsert" build={() => ({ title: "Old snapshot" })} send={send} label="Save"><input name="title" defaultValue="Old snapshot" /></TimelineForm>;
    const view = render(editor(2));
    view.rerender(editor(3));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByRole("status");
    expect(send.mock.calls[0][0].expected_revision).toBe(2);
  });

  it("keeps repeated clicks to one request and disables fields during submission", async () => {
    let finish!: () => void;
    const send = vi.fn(() => new Promise<void>(resolve => { finish = resolve; }));
    render(form(send));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.submit(screen.getByRole("button", { name: "Saving..." }).closest("form")!);
    expect(send).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Message")).toBeDisabled();
    await act(async () => finish());
  });
});

describe("AgentTimelineView", () => {
  it("loads a selected project and distinguishes a control request from a receipt", async () => {
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project] });
    vi.mocked(getAgentProject).mockResolvedValue(project);
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    expect(await screen.findByRole("heading", { name: "Alpha" })).toBeTruthy();
    expect(screen.getByText(/task-a: pause → worker · requested/)).toBeTruthy();
    expect(screen.getByText(/A request is not an acknowledgement/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /acknowledge/i })).toBeNull();
    cache.clear();
  });

  it("creates an explicit report with provenance, without a model call", async () => {
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project] });
    vi.mocked(getAgentProject).mockResolvedValue(project);
    vi.mocked(applyAgentRequest).mockResolvedValue({ ...project, revision: 3 });
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    await screen.findByRole("heading", { name: "Alpha" });
    fireEvent.click(screen.getByText("Add authored report"));
    expect(screen.getByText(/20% subtree/)).toHaveTextContent("8 min remaining");
    fireEvent.change(screen.getByLabelText("Report author"), { target: { value: "Reviewer" } });
    fireEvent.change(screen.getByLabelText("Period start (RFC3339)"), { target: { value: "2026-11-15T00:00:00Z" } });
    fireEvent.change(screen.getByLabelText("Period end (RFC3339)"), { target: { value: "2026-11-15T01:00:00Z" } });
    fireEvent.change(screen.getByLabelText("Source event numbers"), { target: { value: "1,2" } });
    fireEvent.change(screen.getByLabelText("Summary"), { target: { value: "<script>literal</script>" } });
    fireEvent.click(screen.getByRole("button", { name: "Add report" }));
    await waitFor(() => expect(applyAgentRequest).toHaveBeenCalledTimes(1));
    expect(vi.mocked(applyAgentRequest).mock.calls[0]).toEqual(["", "alpha", expect.objectContaining({ kind: "report.add", expected_revision: 2,
      payload: expect.objectContaining({ author: "Reviewer", summary: "<script>literal</script>", event_sequences: [1, 2] }) })]);
    cache.clear();
  });

  it("requests a cooperative pause without pretending to change task status", async () => {
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project] });
    vi.mocked(getAgentProject).mockResolvedValue(project);
    vi.mocked(applyAgentRequest).mockResolvedValue({ ...project, revision: 3 });
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    await screen.findByRole("heading", { name: "Alpha" });
    fireEvent.click(screen.getByText("Request pause or resume"));
    const control = screen.getByRole("button", { name: "Send request" }).closest("form")!;
    fireEvent.change(within(control).getByLabelText("Task"), { target: { value: "task-a" } });
    fireEvent.click(within(control).getByRole("button", { name: "Send request" }));
    await waitFor(() => expect(applyAgentRequest).toHaveBeenCalledTimes(1));
    expect(vi.mocked(applyAgentRequest).mock.calls[0][2]).toMatchObject({ kind: "control.request", payload: { task_id: "task-a", owner: "worker", action: "pause" } });
    expect(vi.mocked(applyAgentRequest).mock.calls[0][2].payload).not.toHaveProperty("status");
    cache.clear();
  });

  it("preserves a qualified note link when updating project metadata", async () => {
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project] });
    vi.mocked(getAgentProject).mockResolvedValue({ ...project, note_id: "work~123" });
    vi.mocked(applyAgentRequest).mockResolvedValue({ ...project, revision: 3 });
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    await screen.findByRole("heading", { name: "Alpha" });
    fireEvent.click(screen.getByText("Edit project"));
    fireEvent.change(screen.getByLabelText("Project title"), { target: { value: "New title" } });
    fireEvent.click(screen.getByRole("button", { name: "Save project" }));
    await waitFor(() => expect(applyAgentRequest).toHaveBeenCalledTimes(1));
    expect(vi.mocked(applyAgentRequest).mock.calls[0][2]).toMatchObject({ kind: "project.update", payload: { title: "New title", note_id: 123 } });
    cache.clear();
  });

  it("keeps a dirty project visible when switching is declined", async () => {
    const beta = { ...project, project_id: "beta", title: "Beta" };
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project, beta] });
    vi.mocked(getAgentProject).mockResolvedValue(project);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    await screen.findByRole("heading", { name: "Alpha" });
    fireEvent.click(screen.getByText("Edit project"));
    fireEvent.change(screen.getByLabelText("Project title"), { target: { value: "Unsaved" } });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "beta" } });
    expect(window.confirm).toHaveBeenCalledOnce();
    expect(screen.getByLabelText("Project")).toHaveValue("alpha");
    expect(screen.getByLabelText("Project title")).toHaveValue("Unsaved");
    cache.clear();
  });

  it("does not replace a newer selected project with a late older response", async () => {
    let finish!: (p: AgentProject) => void;
    const beta = { ...project, project_id: "beta", title: "Beta" };
    vi.mocked(listAgentProjects).mockResolvedValue({ projects: [project, beta] });
    vi.mocked(getAgentProject).mockImplementation((_vault, id) => id === "alpha" ? new Promise(resolve => { finish = resolve; }) : Promise.resolve(beta));
    const cache = provider();
    render(<QueryClientProvider client={cache}><AgentTimelineView /></QueryClientProvider>);
    await screen.findByRole("option", { name: "Alpha" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "alpha" } });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "beta" } });
    await screen.findByRole("heading", { name: "Beta" });
    await act(async () => finish(project));
    expect(screen.queryByRole("heading", { name: "Alpha" })).toBeNull();
    expect(within(screen.getByRole("region", { name: "Project timeline" })).getByRole("heading", { name: "Beta" })).toBeTruthy();
    cache.clear();
  });
});
