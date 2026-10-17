import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExternalMarkdownProvider } from "./ExternalMarkdownSource";
import { MarkdownViewer } from "./MarkdownViewer";
import * as handoff from "../markdownHandoff";

function file(path: string, markdown: string | Uint8Array): File {
  const bytes = typeof markdown === "string" ? new TextEncoder().encode(markdown) : markdown;
  const item = new File([bytes as BlobPart], path.split("/").at(-1)!);
  Object.defineProperty(item, "webkitRelativePath", { value: path });
  Object.defineProperty(item, "arrayBuffer", { value: async () => bytes.buffer });
  return item;
}

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("external Markdown router navigation", () => {
  it("opens an editor handoff through the real router, renders a local image and follows relative links", async () => {
    vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    const create = vi.fn(() => "blob:editor-image");
    const revoke = vi.fn();
    vi.stubGlobal("URL", class extends URL { static createObjectURL = create; static revokeObjectURL = revoke; });
    const consume = vi.spyOn(handoff, "consumeMarkdownHandoff").mockResolvedValue({ active: "日本語 file.md", files: [
      file("日本語 file.md", "# Current\n\n[Next](docs/next.md#details)\n\n![Local image](images/pic.png)"),
      file("docs/next.md", "# Next\n\n## Details\n\n[Back](../日本語%20file.md)"),
      file("images/pic.png", new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])),
    ] });
    const history = createMemoryHistory({ initialEntries: [`/markdown#handoff=${"a".repeat(64)}`] });
    const root = createRootRoute({ component: () => <ExternalMarkdownProvider><Outlet /></ExternalMarkdownProvider> });
    const route = createRoute({
      getParentRoute: () => root, path: "/markdown",
      validateSearch: (search: Record<string, unknown>): { doc?: string } => ({ doc: typeof search.doc === "string" ? search.doc : undefined }),
      component: () => <MarkdownViewer documentID={route.useSearch().doc} />,
    });
    const router = createRouter({ routeTree: root.addChildren([route]), history });
    const { unmount } = render(<RouterProvider router={router} />);
    expect(await screen.findByRole("article", { name: "External Markdown: 日本語 file.md" })).toBeVisible();
    await waitFor(() => expect(screen.getByRole("img", { name: "Local image" })).toHaveAttribute("src", "blob:editor-image"));
    expect(consume).toHaveBeenCalledExactlyOnceWith("a".repeat(64));
    expect(history.location.href).not.toContain("handoff=");
    fireEvent.click(screen.getByRole("link", { name: "Next" }));
    expect(await screen.findByRole("article", { name: "External Markdown: docs/next.md" })).toBeVisible();
    expect(history.location.hash).toBe("#details");
    fireEvent.click(screen.getByRole("link", { name: "Back" }));
    expect(await screen.findByRole("article", { name: "External Markdown: 日本語 file.md" })).toBeVisible();
    unmount();
    expect(revoke).toHaveBeenCalledWith("blob:editor-image");
  });

  it("keeps file and heading history through the real router and never requests vault content", async () => {
    vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    const scroll = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    const fetch = vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("No vault/network access"));
    const history = createMemoryHistory({ initialEntries: ["/markdown"] });
    const root = createRootRoute({ component: () => <ExternalMarkdownProvider><Outlet /></ExternalMarkdownProvider> });
    const route = createRoute({
      getParentRoute: () => root,
      path: "/markdown",
      validateSearch: (search: Record<string, unknown>): { doc?: string } => ({ doc: typeof search.doc === "string" ? search.doc : undefined }),
      component: () => <MarkdownViewer documentID={route.useSearch().doc} />,
    });
    const router = createRouter({ routeTree: root.addChildren([route]), history });
    const { unmount } = render(<RouterProvider router={router} />);
    const input = await screen.findByLabelText("Select Markdown folder");
    fireEvent.change(input, { target: { files: [
      file("repo/README.md", "# README\n\n[Setup](#setup)\n\n[Guide](docs/guide.md#requirements)\n\n## Setup\n\nLocal only."),
      file("repo/docs/guide.md", "Guide\n=====\n\n## Requirements\n\n[README](../README.md#setup)"),
    ] } });
    expect(await screen.findByRole("article", { name: "External Markdown: repo/README.md" })).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "Setup" }));
    await waitFor(() => expect(history.location.hash).toBe("#setup"));
    const setupURL = history.location.href;
    fireEvent.click(screen.getByRole("link", { name: "Guide" }));
    expect(await screen.findByRole("article", { name: "External Markdown: repo/docs/guide.md" })).toBeVisible();
    expect(history.location.hash).toBe("#requirements");
    await act(async () => { history.back(); });
    expect(await screen.findByRole("article", { name: "External Markdown: repo/README.md" })).toBeVisible();
    expect(history.location.href).toBe(setupURL);
    scroll.mockClear();
    await act(async () => { history.back(); });
    await waitFor(() => expect(history.location.hash).toBe(""));
    expect(scroll.mock.instances.some((element) => (element as HTMLElement).classList.contains("external-markdown-viewer"))).toBe(true);
    expect(screen.getByRole("article", { name: "External Markdown: repo/README.md" })).toBeVisible();
    await act(async () => { history.forward(); });
    await waitFor(() => expect(history.location.hash).toBe("#setup"));
    expect(fetch).not.toHaveBeenCalled();
    unmount();
    fetch.mockRestore();
  });
});
