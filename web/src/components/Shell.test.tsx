import { within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderPage } from "../entry-server";
import { STATIC_MODE } from "../runtime";

vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  getSite: async () => ({ root: "sidebar-fixture", title: "Sidebar fixture" }),
  getNote: async (id: string) => ({
    note: { note_id: id, title: "Sidebar fixture", body: "Synthetic test note.", tags: [] },
    backlinks: [],
  }),
  renderMarkdown: async (markdown: string) => ({ markdown }),
}));

describe("sidebar actions", () => {
  afterEach(() => {
    delete window.__trackNativeReading;
  });

  // The native app uses the live frontend with this capability flag. Exercise both deployments
  // with the real Shell and route tree; run this file again with VITE_TRACK_STATIC=1 for SSG.
  it.each([false, true])("keeps New and Voice input live-only (native=%s)", async (native) => {
    window.__trackNativeReading = native;
    const { html } = await renderPage("/notes/sidebar-fixture");
    const container = document.createElement("div");
    container.innerHTML = html;
    const rail = within(container).getByRole("navigation", { name: "Workspace views" });
    expect(within(rail).queryByRole("button", { name: "Recently created notes" }) !== null).toBe(!STATIC_MODE);
    expect(within(rail).queryByRole("link", { name: "Voice input" }) !== null).toBe(!STATIC_MODE);
    expect(within(rail).getByRole("link", { name: "Full graph" })).toBeTruthy();
    expect(within(rail).queryByRole("button", { name: "Agent requests" })).toBeNull();
    expect(container.querySelector(".agent-request-panel")).toBeNull();
  });
});
