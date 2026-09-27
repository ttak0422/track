import { QueryClient, hydrate } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { STATIC_MODE } from "./runtime";
import { queryKeys } from "./queries";

// Exercise the real static route tree, reader and Markdown links with both deployment bases:
// VITE_TRACK_STATIC=1 npm test -- src/entry-server.test.tsx
const body = "[Open counter](apps/counter/)";
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  getSite: async () => ({ root: "static-apps", title: "Help" }),
  getNote: async (id: string) => ({
    note: { note_id: id, title: "Static apps", body, tags: [] },
    backlinks: [],
  }),
  renderMarkdown: async (markdown: string) => ({ markdown }),
  getLocalGraph: async () => ({ nodes: [], edges: [] }),
}));

describe.skipIf(!STATIC_MODE).each(["/", "/guide/help/"])("static prerender routes at %s", (base) => {
  afterEach(() => {
    vi.unstubAllEnvs();
    delete window.__trackStartPage;
  });

  it.each(["/", "/notes/static-apps"])("renders %s with the same body and links as the client", async (route) => {
    // Vitest normalizes Vite's base to "/". Set the build-time value before loading the route tree.
    vi.stubEnv("BASE_URL", base);
    window.__trackStartPage = "static-apps";
    vi.resetModules();
    const { renderPage } = await import("./entry-server");
    const { AppTree, createAppRouter } = await import("./App");
    const { html, state } = await renderPage(route);
    const ssr = document.createElement("div");
    ssr.innerHTML = html;
    const href = `${import.meta.env.BASE_URL}apps/counter/`;
    expect(ssr.querySelector(".note-preview a")?.getAttribute("href")).toBe(href);
    expect(ssr.querySelector(".note-preview")?.textContent).toContain("Static apps");

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    hydrate(client, JSON.parse(state));
    expect(client.getQueryData(queryKeys.note("static-apps"))).toBeDefined();
    expect(client.getQueryData(queryKeys.render(body))).toEqual({ markdown: body });

    // Published directory indexes have trailing slashes; use actual browser history, not the SSR
    // memory-history setup under test. Static boot hydrates the cache and mounts with createRoot.
    const path = import.meta.env.BASE_URL + (route === "/" ? "" : `${route.slice(1)}/`);
    window.history.replaceState(null, "", path);
    const router = createAppRouter();
    await router.load();
    const mounted = render(<AppTree queryClient={client}><RouterProvider router={router} /></AppTree>);
    try {
      await waitFor(() => expect(mounted.container.querySelector(".note-preview a")).toHaveAttribute("href", href));
      expect(mounted.container.querySelector(".note-preview")?.textContent).toBe(
        ssr.querySelector(".note-preview")?.textContent,
      );
      expect(mounted.container.querySelector('a[aria-label="Start page"]')?.getAttribute("href")).toBe(
        ssr.querySelector('a[aria-label="Start page"]')?.getAttribute("href"),
      );
    } finally {
      mounted.unmount();
      router.history.destroy();
      client.clear();
    }
  });
});
