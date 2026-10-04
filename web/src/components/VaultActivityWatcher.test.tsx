import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { listNewNotes, listNotes } from "../api";
import { NotificationProvider, NotificationToast } from "../notifications";
import { queryKeys } from "../queries";
import type { NotesResponse, SearchResult } from "../types";
import { markSelfWrite, newlyActive, today } from "../vaultActivity";
import { SidebarNew } from "./SidebarNew";
import { VaultActivityWatcher } from "./VaultActivityWatcher";

const scope = vi.hoisted(() => ({ value: "" }));
vi.mock("../vaultScope", () => ({ useVaultScope: () => ({ scope: scope.value }) }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, params }: { children: ReactNode; params: { noteId: string } }) => (
    <a role="menuitem" href={`/notes/${params.noteId}`}>{children}</a>
  ),
  useNavigate: () => vi.fn(),
}));
vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  listNotes: vi.fn(),
  listNewNotes: vi.fn(),
}));

function note(id: string, title: string): SearchResult {
  return { note_id: id, title, file_kind: "note", path: `/vault/note/${id}.md`, days: [today()] };
}
function pending<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
async function tick(ms = 50) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  function Workspace() {
    return (
      <QueryClientProvider client={client}>
        <NotificationProvider>
          <VaultActivityWatcher />
          <SidebarNew />
          <NotificationToast />
        </NotificationProvider>
      </QueryClientProvider>
    );
  }
  const view = render(<Workspace />);
  return { client, switchVault: (vault: string) => { scope.value = vault; view.rerender(<Workspace />); } };
}
function items() {
  return screen.queryAllByRole("menuitem").map((item) => item.textContent);
}
async function poll(client: QueryClient) {
  await act(async () => { await client.refetchQueries({ queryKey: queryKeys.notes(), exact: true }); });
  await tick();
}

// Keep the real QueryClient, watcher, and sidebar together: spying on invalidateQueries alone misses
// initial-request deduplication, cancelled responses, inactive caches, and the creation-order list.
describe("activity notification refreshes New", () => {
  let old: SearchResult;
  beforeEach(() => {
    vi.useFakeTimers();
    scope.value = "";
    newlyActive([], "reset");
    vi.mocked(listNotes).mockReset();
    vi.mocked(listNewNotes).mockReset();
    old = note("1", "Existing note");
    vi.mocked(listNotes).mockResolvedValue({ notes: [old] });
    vi.mocked(listNewNotes).mockResolvedValue({ notes: [old] });
  });
  afterEach(() => vi.useRealTimers());

  it("refreshes the open sidebar when the existing poll discovers an external creation without SSE", async () => {
    mount();
    await tick();
    fireEvent.click(screen.getByRole("button", { name: "Recently created notes" }));
    expect(items()).toEqual(["Existing note"]);
    expect(screen.queryByRole("alert")).toBeNull();

    const added = note("2", "Created by CLI");
    vi.mocked(listNotes).mockResolvedValue({ notes: [old, added] });
    vi.mocked(listNewNotes).mockResolvedValue({ notes: [added, old] });
    await tick(60_000);

    expect(screen.getByRole("alert")).toHaveTextContent("Updated: Created by CLI");
    // Use the server's creation order, never the activity response's modification order.
    expect(items()).toEqual(["Created by CLI", "Existing note"]);
    expect(listNewNotes).toHaveBeenLastCalledWith(100, "");
    expect(listNotes).toHaveBeenCalledTimes(2); // Refreshing New does not create a polling loop.
    expect(listNewNotes).toHaveBeenCalledTimes(2);
  });

  it("does not refresh or notify on priming, unchanged polls, or this tab's own save", async () => {
    const { client } = mount();
    await tick();
    await poll(client);
    markSelfWrite("2");
    vi.mocked(listNotes).mockResolvedValue({ notes: [old, note("2", "My save")] });
    await poll(client);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(listNewNotes).toHaveBeenCalledTimes(1);
  });

  it("replaces an initial in-flight listing and ignores its late pre-creation response", async () => {
    const stale = pending<NotesResponse>();
    vi.mocked(listNewNotes).mockReturnValueOnce(stale.promise);
    const { client } = mount();
    await tick();
    fireEvent.click(screen.getByRole("button", { name: "Recently created notes" }));

    const added = note("2", "Arrived while loading");
    vi.mocked(listNotes).mockResolvedValue({ notes: [added, old] });
    vi.mocked(listNewNotes).mockResolvedValue({ notes: [added, old] });
    await poll(client);
    expect(items()).toEqual(["Arrived while loading", "Existing note"]);
    await act(async () => { stale.resolve({ notes: [old] }); });
    await tick();
    expect(items()).toEqual(["Arrived while loading", "Existing note"]);
  });

  it("keeps every arrival when a second notification interrupts a pending refresh", async () => {
    const { client } = mount();
    await tick();
    const first = note("2", "First arrival");
    const second = note("3", "Second arrival");
    const stale = pending<NotesResponse>();
    vi.mocked(listNewNotes).mockReturnValueOnce(stale.promise);
    vi.mocked(listNotes).mockResolvedValue({ notes: [first, old] });
    await poll(client);
    vi.mocked(listNewNotes).mockResolvedValue({ notes: [second, first, old] });
    vi.mocked(listNotes).mockResolvedValue({ notes: [second, first, old] });
    await poll(client);
    await act(async () => { stale.resolve({ notes: [first, old] }); });
    await tick();
    fireEvent.click(screen.getByRole("button", { name: "Recently created notes" }));
    expect(items()).toEqual(["Second arrival", "First arrival", "Existing note"]);
    expect(screen.getByRole("alert")).toHaveTextContent("Updated: Second arrival");
  });

  it("invalidates inactive vaults without letting their late responses replace the selected vault", async () => {
    const stale = pending<NotesResponse>();
    vi.mocked(listNewNotes).mockReturnValueOnce(stale.promise);
    const { client, switchVault } = mount();
    await tick();
    const workOld = note("work~1", "Work existing");
    const workNew = note("work~2", "Work new");
    const launchNew = note("2", "Launch new");
    vi.mocked(listNewNotes).mockImplementation(async (_limit, vault) => ({
      notes: vault === "work" ? [workOld] : [launchNew, old],
    }));
    switchVault("work");
    await tick();
    fireEvent.click(screen.getByRole("button", { name: "Recently created notes" }));
    expect(items()).toEqual(["Work existing"]);
    vi.mocked(listNewNotes).mockImplementation(async (_limit, vault) => ({
      notes: vault === "work" ? [workNew, workOld] : [launchNew, old],
    }));
    vi.mocked(listNotes).mockResolvedValue({ notes: [launchNew, old] });
    await poll(client);
    await act(async () => { stale.resolve({ notes: [old] }); });
    await tick();
    expect(items()).toEqual(["Work new", "Work existing"]);
    expect(listNewNotes).toHaveBeenLastCalledWith(100, "work");
    expect(client.getQueryState(queryKeys.newNotes(100, ""))?.isInvalidated).toBe(true);

    switchVault("");
    await tick();
    expect(items()).toEqual(["Launch new", "Existing note"]);
    expect(listNewNotes).toHaveBeenLastCalledWith(100, "");
  });
});
