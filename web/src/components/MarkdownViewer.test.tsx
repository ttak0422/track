import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ExternalMarkdownProvider } from "./ExternalMarkdownSource";
import { MarkdownViewer } from "./MarkdownViewer";

const router = vi.hoisted(() => ({ navigate: vi.fn(), hash: "" }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => router.navigate, useRouterState: () => router.hash }));
vi.mock("./MarkdownView", () => ({ MarkdownView: ({ markdown, title, external }: {
  markdown: string; title: string; external: { onOpenLink: (href: string) => void };
}) => <div><h2>{title}</h2><pre>{markdown}</pre><button onClick={() => external.onOpenLink("docs/next.md#setup")}>Follow next</button><button onClick={() => external.onOpenLink("../../private.md")}>Follow missing</button></div> }));

function file(path: string, text = "# README"): File {
  const bytes = new TextEncoder().encode(text);
  const item = new File([bytes], path.split("/").at(-1)!);
  Object.defineProperty(item, "webkitRelativePath", { value: path.includes("/") ? path : "" });
  Object.defineProperty(item, "arrayBuffer", { value: vi.fn(async () => bytes.buffer) });
  return item;
}

function Harness() {
  const [doc, setDoc] = useState("");
  const [away, setAway] = useState(false);
  const [first, setFirst] = useState("");
  router.navigate.mockImplementation(({ search, hash }: { search: { doc?: string }; hash?: string }) => {
    router.hash = hash ?? "";
    setFirst((value) => value || search.doc || "");
    setDoc(search.doc ?? "");
  });
  return <ExternalMarkdownProvider>
    <button onClick={() => setAway(!away)}>Toggle vault view</button>
    <button onClick={() => setDoc("unknown-old-source")}>Go to old source</button>
    <button onClick={() => setDoc(first)}>History back</button>
    <button onClick={() => setDoc("")}>Open viewer tab</button>
    {away ? <div>Vault note</div> : <MarkdownViewer documentID={doc} />}
  </ExternalMarkdownProvider>;
}

function choose(files: File[], folder = false) {
  fireEvent.change(screen.getByLabelText(folder ? "Select Markdown folder" : "Select Markdown files"), { target: { files } });
}

beforeEach(() => { router.navigate.mockReset(); router.hash = ""; window.localStorage.clear(); });

describe("MarkdownViewer", () => {
  it("chooses local files, follows scoped links, survives note navigation, and clears the source", async () => {
    const fetch = vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("No network reads allowed"));
    render(<Harness />);
    choose([file("repo/README.md"), file("repo/docs/next.md", "## Next")], true);
    expect(await screen.findByRole("heading", { name: "repo/README.md" })).toBeVisible();
    expect(screen.getByText(/External source · Read-only/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Follow missing" }));
    expect(screen.getByRole("alert")).toHaveTextContent(/outside the selected source/);
    fireEvent.click(screen.getByRole("button", { name: "Follow next" }));
    expect(await screen.findByRole("heading", { name: "repo/docs/next.md" })).toBeVisible();
    expect(router.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ to: "/markdown", hash: "setup" }));
    fireEvent.click(screen.getByRole("button", { name: "Toggle vault view" }));
    expect(screen.getByText("Vault note")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Toggle vault view" }));
    expect(await screen.findByRole("heading", { name: "repo/docs/next.md" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "History back" }));
    expect(await screen.findByRole("heading", { name: "repo/README.md" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Open viewer tab" }));
    expect(await screen.findByRole("heading", { name: "repo/README.md" })).toBeVisible();
    expect(fetch).not.toHaveBeenCalled();
    expect(localStorage.length).toBe(0);
    fireEvent.click(screen.getByRole("button", { name: "Close source" }));
    expect(screen.queryByRole("heading", { name: "repo/docs/next.md" })).toBeNull();
    expect(screen.getByText(/Open a README/)).toBeVisible();
    fetch.mockRestore();
  });

  it("cancel and invalid selection keep the current document; reselecting the same file refreshes", async () => {
    render(<Harness />);
    choose([file("README.md", "First")]);
    expect(await screen.findByText("First")).toBeVisible();
    choose([]);
    expect(screen.getByText("First")).toBeVisible();
    choose([file("not-markdown.txt")]);
    expect(screen.getByRole("alert")).toHaveTextContent(/Choose a .md/);
    expect(screen.getByText("First")).toBeVisible();
    choose([file("README.md", "Updated")]);
    expect(await screen.findByText("Updated")).toBeVisible();
    expect(screen.queryByText("First")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("an older read cannot replace a newer selection, even after closing", async () => {
    render(<Harness />);
    const slow = file("slow.md");
    let resolve!: (value: ArrayBuffer) => void;
    vi.mocked(slow.arrayBuffer).mockImplementation(() => new Promise((done) => { resolve = done; }));
    choose([slow]);
    await waitFor(() => expect(slow.arrayBuffer).toHaveBeenCalledOnce());
    choose([file("new.md", "New source")]);
    expect(await screen.findByText("New source")).toBeVisible();
    await act(async () => { resolve(new TextEncoder().encode("Old source").buffer); });
    expect(screen.queryByText("Old source")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Close source" }));
    expect(screen.queryByText("New source")).toBeNull();
  });

  it("reports vanished files and stale source routes, and forgets selections on full remount", async () => {
    const { unmount } = render(<Harness />);
    const missing = file("missing.md");
    vi.mocked(missing.arrayBuffer).mockRejectedValue(new Error("NotReadableError"));
    choose([missing]);
    expect(await screen.findByRole("alert")).toHaveTextContent(/no longer readable/);
    fireEvent.click(screen.getByRole("button", { name: "Go to old source" }));
    expect(screen.getByRole("status")).toHaveTextContent(/older source/);
    unmount();
    render(<ExternalMarkdownProvider><MarkdownViewer documentID="old-id" /></ExternalMarkdownProvider>);
    expect(screen.getByText(/This source is no longer open/)).toBeVisible();
    expect(screen.queryByLabelText("File")).toBeNull();
  });
});
