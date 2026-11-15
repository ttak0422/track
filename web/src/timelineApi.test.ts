import { afterEach, describe, expect, it, vi } from "vitest";
import { getTaskTimeline } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("getTaskTimeline", () => {
  it("preserves vault scope and forwards request cancellation", async () => {
    const controller = new AbortController();
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      schema_version: 1, note_id: 1, vault: "work", title: "Project", items: [], history: [],
      progress: { total: 0, done: 0, cancelled: 0, open: 0 },
    })));
    vi.stubGlobal("fetch", fetcher);
    const result = await getTaskTimeline("work~1", controller.signal);
    expect(fetcher).toHaveBeenCalledWith("/api/timeline?id=1&vault=work", expect.objectContaining({ signal: controller.signal }));
    expect(result.note_id).toBe("work~1");
  });
});
