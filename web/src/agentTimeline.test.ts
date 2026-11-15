import { afterEach, describe, expect, it, vi } from "vitest";
import { applyAgentRequest, getAgentProject } from "./agentTimeline";

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
});
