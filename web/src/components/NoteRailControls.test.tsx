import { fireEvent, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { describe, expect, it, vi } from "vitest";
import { NoteControlsProvider, useNoteControls, type NoteActions } from "../noteControls";
import { NoteRailControls } from "./NoteRailControls";

// Stands in for the note view: registers what the rail may act on, and reports back what the rail set.
function FakeNote({ actions, onState }: { actions: NoteActions | null; onState?: (s: string) => void }) {
  const { setActions, mode, follow } = useNoteControls();
  useEffect(() => {
    setActions(actions);
    return () => setActions(null);
  }, [actions, setActions]);
  onState?.(`${mode}/${follow}`);
  return null;
}

function renderRail(actions: NoteActions | null, onState?: (s: string) => void) {
  return render(
    <NoteControlsProvider>
      <FakeNote actions={actions} onState={onState} />
      <NoteRailControls />
    </NoteControlsProvider>,
  );
}

const noopActions: NoteActions = { getBody: () => "# body", onMeta: () => {}, onDelete: () => {} };

describe("NoteRailControls", () => {
  it("shows nothing while no note is open, so the rail stays navigation alone", async () => {
    renderRail(null);
    await Promise.resolve();
    expect(screen.queryByRole("button", { name: "Preview" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "More actions" })).not.toBeInTheDocument();
  });

  it("shows the open note's controls once it registers", async () => {
    renderRail(noopActions);
    expect(await screen.findByRole("button", { name: "Preview" })).toBeInTheDocument();
    for (const name of ["Follow the editor", "Preview", "Edit", "Split", "More actions"]) {
      expect(screen.getByRole("button", { name })).toBeInTheDocument();
    }
  });

  it("marks the current mode pressed and switches on click", async () => {
    const seen: string[] = [];
    renderRail(noopActions, (s) => seen.push(s));
    const preview = await screen.findByRole("button", { name: "Preview" });
    expect(preview).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Split" })).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(screen.getByRole("button", { name: "Split" }));
    expect(screen.getByRole("button", { name: "Split" })).toHaveAttribute("aria-pressed", "true");
    expect(seen.at(-1)).toBe("split/false");
  });

  it("toggles follow, which the note view reads back", async () => {
    const seen: string[] = [];
    renderRail(noopActions, (s) => seen.push(s));
    const follow = await screen.findByRole("button", { name: "Follow the editor" });
    expect(follow).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(follow);
    expect(screen.getByRole("button", { name: "Follow the editor" })).toHaveAttribute("aria-pressed", "true");
    expect(seen.at(-1)).toBe("preview/true");
  });

  it("reads the body through the getter, so a keystroke never re-renders the rail", async () => {
    const getBody = vi.fn(() => "# body");
    renderRail({ ...noopActions, getBody });
    await screen.findByRole("button", { name: "More actions" });
    // Nothing has asked for the body yet: the rail holds the getter, not the text.
    expect(getBody).not.toHaveBeenCalled();
  });
});
