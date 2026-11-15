import { afterEach, describe, expect, it, vi } from "vitest";
import { applyAgentRequest, getAgentProject, listAgentProjects } from "./agentTimeline";

const request = { source: "web", event_id: "event-1", expected_revision: 0, kind: "project.create", payload: { title: "Project", mode: "serial" } };
const saved = { schema_version: 1, project_id: "p", title: "Project", revision: 1, mode: "serial", progress: { fraction: 0 }, tasks: [], reports: [], discussions: [], controls: [],
  events: [{ source: "web", event_id: "event-1", kind: "project.create" }] };
afterEach(() => vi.unstubAllGlobals());

describe("agent timeline API", () => {
  it("scopes writes by vault and preserves the exact request identity", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(saved)));
    vi.stubGlobal("fetch", fetcher);
    await applyAgentRequest("work", "p", request);
    expect(fetcher).toHaveBeenCalledWith("/api/timeline/project?id=p&vault=work", expect.objectContaining({ method: "POST", body: JSON.stringify(request) }));
  });
  it("rejects a successful response without the submitted event receipt", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...saved, events: [] }))));
    await expect(applyAgentRequest("", "p", request)).rejects.toThrow("acknowledgement is missing");
  });
  it("rejects a response belonging to a different project", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(saved))));
    await expect(getAgentProject("", "another")).rejects.toThrow("Invalid timeline response");
  });
  it("preserves named-vault discussion and event identities through a read/write round trip", async () => {
    const discussion = { id: "comment-1", target_type: "task", target_id: "task-1", author: "human", text: "Reviewed" };
    const payload = { ...discussion, note_id: 123, source_id: "agent-1", root: "task-1" };
    const wire = { ...saved, vault: "work", note_id: 42, discussions: [discussion],
      events: [{ sequence: 1, source: "web", event_id: "event-1", kind: "discussion.add", payload }] };
    const fetcher = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(wire))));
    vi.stubGlobal("fetch", fetcher);
    const read = await getAgentProject("work", "p");
    expect(read.note_id).toBe("work~42");
    expect(read.discussions[0]).toEqual(discussion);
    expect(read.events[0].payload).toEqual(payload);
    const write = { source: "web", event_id: "event-1", expected_revision: 1, kind: "discussion.add", payload: read.events[0].payload };
    const receipt = await applyAgentRequest("work", "p", write);
    expect(JSON.parse(fetcher.mock.calls[1][1].body).payload).toEqual(payload);
    expect(receipt.discussions[0].target_id).toBe("task-1");
    expect(receipt.events[0].payload).toEqual(payload);
    expect(receipt.note_id).toBe("work~42");
  });
  it("qualifies only linked notes in project summaries, using the response vault", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ vault: "work", projects: [
      { project_id: "p", title: "Project", revision: 1, note_id: 42, progress: { fraction: 0 } },
      { project_id: "no-note", title: "Unlinked", revision: 1, progress: { fraction: 0 } },
    ] }))));
    const data = await listAgentProjects("");
    expect(data.projects[0].note_id).toBe("work~42");
    expect(data.projects[0].project_id).toBe("p");
    expect(data.projects[1]).not.toHaveProperty("note_id");
  });

});
