import { act, fireEvent, render, screen } from "@testing-library/react";
// @ts-expect-error node builtin used by the test runner, not the application
import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CalendarEventDialog } from "./components/CalendarEventDialog";
import { useNativeWorkspaceState } from "./nativeNavigation";

const mutation = vi.hoisted(() => ({ mutateAsync: vi.fn(), isPending: false }));
vi.mock("./calendarEventQueries", () => ({ useSaveCalendarEvent: () => mutation }));
vi.mock("@tanstack/react-router", () => ({ useBlocker: () => {} }));

// Exercise the actual native probe, rather than a second implementation of its selector.
const source: string = readFileSync("../desktop/Sources/TrackWebApp/WebViewCoordinator.swift", "utf8");
const probe = source.split('"""').find((part) => part.includes("const read = window.__trackNativeWorkspaceState"));
if (!probe) throw new Error("Native workspace probe not found");
const nativeState = () => window.eval(probe);

function Workspace({ path = "/calendar", open = true, onClose = () => {}, onSaved = () => {} }) {
  useNativeWorkspaceState(path);
  return open ? <CalendarEventDialog vault="" date="2026-10-28" notes={[]} onClose={onClose} onSaved={onSaved} /> : null;
}

beforeEach(() => { mutation.mutateAsync.mockReset(); mutation.isPending = false; });
afterEach(() => vi.restoreAllMocks());

describe("native event editor data-loss probe", () => {
  it.each(["/calendar", "/day/2026-10-28", "/tasks"])("requires confirmation for an open event editor on %s", (path) => {
    const view = render(<Workspace path={path} />);
    expect(window.__trackNativeWorkspaceState?.()).toBe(false);
    expect(document.querySelector("dialog[open]")).not.toBeNull();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(nativeState()).toBeNull(); // Even an untouched open modal is conservative by contract.
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Unsubmitted event" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Keep this draft" } });
    expect(nativeState()).toBeNull();
    expect(screen.getByLabelText("Description")).toHaveValue("Keep this draft");
    view.rerender(<Workspace path={path} open={false} />);
    expect(nativeState()).toBe(false);
  });

  it("remains conservative through pending and failed saves and permits leaving after editor cancellation", async () => {
    let reject!: (reason: Error) => void;
    mutation.mutateAsync.mockImplementation(() => new Promise((_, fail) => { reject = fail; }));
    const onClose = vi.fn();
    const view = render(<Workspace onClose={onClose} />);
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Pending event" } });
    fireEvent.click(screen.getByRole("button", { name: "Save event" }));
    expect(nativeState()).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => reject(new Error("Fixture write failed")));
    expect(screen.getByRole("alert")).toHaveTextContent("Fixture write failed");
    expect(screen.getByLabelText("Title")).toHaveValue("Pending event");
    expect(nativeState()).toBeNull();
    fireEvent(screen.getByRole("dialog"), new Event("cancel", { bubbles: true, cancelable: true }));
    expect(onClose).toHaveBeenCalledTimes(1);
    view.rerender(<Workspace open={false} />);
    expect(nativeState()).toBe(false);
  });

  it("returns to clean after a confirmed save unmounts the editor", async () => {
    mutation.mutateAsync.mockResolvedValue({ event: { id: "fixture" } });
    const saved = vi.fn();
    const view = render(<Workspace onSaved={saved} />);
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Saved event" } });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Save event" })));
    expect(saved).toHaveBeenCalledOnce();
    view.rerender(<Workspace open={false} />);
    expect(nativeState()).toBe(false);
    view.unmount();
    expect(nativeState()).toBeNull(); // No capability is not proof of a clean document.
  });

  it("keeps explicit ARIA dialogs conservative and ignores closed native dialogs", () => {
    render(<Workspace open={false} />);
    const dialog = document.createElement("dialog");
    document.body.append(dialog);
    expect(nativeState()).toBe(false);
    dialog.setAttribute("role", "dialog");
    expect(nativeState()).toBeNull();
    dialog.remove();
  });
});
