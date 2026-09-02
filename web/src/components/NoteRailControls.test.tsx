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
    expect(screen.queryByRole("button", { name: "Display mode: Preview" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "More actions" })).not.toBeInTheDocument();
  });

  it("shows the open note's controls once it registers", async () => {
    renderRail(noopActions);
    expect(await screen.findByRole("button", { name: "Display mode: Preview" })).toBeInTheDocument();
    for (const name of ["Follow the editor: Off", "Display mode: Preview", "More actions"]) {
      expect(screen.getByRole("button", { name })).toBeInTheDocument();
    }
    expect(screen.queryByRole("menuitemradio", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("shows only the current mode in the rail and switches from the labelled hover menu", async () => {
    const seen: string[] = [];
    renderRail(noopActions, (s) => seen.push(s));
    const toggle = await screen.findByRole("button", { name: "Display mode: Preview" });
    expect(toggle.querySelector("svg")).toHaveAttribute("data-mode", "preview");

    fireEvent.pointerEnter(toggle);
    expect(screen.getByRole("menu", { name: "Display mode" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "Preview" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemradio", { name: "Edit" })).toHaveAttribute("aria-checked", "false");

    fireEvent.click(screen.getByRole("menuitemradio", { name: "Edit" }));
    const editToggle = screen.getByRole("button", { name: "Display mode: Edit" });
    expect(editToggle.querySelector("svg")).toHaveAttribute("data-mode", "edit");
    expect(screen.queryByRole("menu", { name: "Display mode" })).not.toBeInTheDocument();
    expect(seen.at(-1)).toBe("edit/false");
  });

  // Follow opens no panel, so it says its name the way the rail's other panel-less glyphs do: at
  // once, beside the rail. The two that do open a panel are named by that panel and say nothing
  // extra — and none of the three carries the browser's own tooltip, which arrives seconds later
  // and lands on whatever the control just opened.
  it("names follow with a rail tip and the menu toggles with their panels alone", async () => {
    renderRail(noopActions);
    const follow = await screen.findByRole("button", { name: "Follow the editor: Off" });
    for (const name of ["Follow the editor: Off", "Display mode: Preview", "More actions"]) {
      expect(screen.getByRole("button", { name })).not.toHaveAttribute("title");
    }

    fireEvent.pointerEnter(follow);
    const tip = screen.getByText("Follow the editor: Off");
    expect(tip).toHaveClass("rail-tip");
    expect(tip).toHaveAttribute("aria-hidden", "true");
  });

  it("toggles follow, which the note view reads back", async () => {
    const seen: string[] = [];
    renderRail(noopActions, (s) => seen.push(s));
    const follow = await screen.findByRole("button", { name: "Follow the editor: Off" });
    expect(follow).toHaveAttribute("aria-pressed", "false");
    expect(follow.querySelector("svg")).toHaveAttribute("data-state", "off");
    fireEvent.click(follow);
    const activeFollow = screen.getByRole("button", { name: "Follow the editor: On" });
    expect(activeFollow).toHaveAttribute("aria-pressed", "true");
    expect(activeFollow.querySelector("svg")).toHaveAttribute("data-state", "on");
    expect(seen.at(-1)).toBe("preview/true");
  });

  it("opens the actions menu on hover, like the rail's other flyouts", async () => {
    renderRail(noopActions);
    const toggle = await screen.findByRole("button", { name: "More actions" });
    expect(screen.queryByRole("menuitem", { name: "Copy MD" })).not.toBeInTheDocument();

    fireEvent.pointerEnter(toggle);
    expect(screen.getByRole("menuitem", { name: "Copy MD" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Copy for Confluence" })).toBeInTheDocument();
  });

  it("reads the body through the getter, so a keystroke never re-renders the rail", async () => {
    const getBody = vi.fn(() => "# body");
    renderRail({ ...noopActions, getBody });
    await screen.findByRole("button", { name: "More actions" });
    // Nothing has asked for the body yet: the rail holds the getter, not the text.
    expect(getBody).not.toHaveBeenCalled();
  });
});
