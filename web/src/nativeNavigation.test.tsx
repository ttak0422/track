import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";
import { useNativeEditorState, useNativeWorkspaceState } from "./nativeNavigation";

function Editor({ body = "saved", saved = "saved", pending = false, deleted = false }) {
  const textarea = useRef<HTMLTextAreaElement>(null);
  const baseline = useRef(saved);
  baseline.current = saved;
  useNativeEditorState({ body, textarea, savedBody: () => baseline.current, pending, isDeleted: () => deleted });
  return <textarea aria-label="Draft" ref={textarea} defaultValue={body} />;
}

const state = () => window.__trackNativeEditorState?.();

describe("native editor navigation state", () => {
  it("reports an unchanged note clean and removes its capability on unmount", () => {
    const view = render(<Editor />);
    expect(state()).toBe(false);
    view.unmount();
    expect(state()).toBeUndefined();
  });
  it("reads the current DOM buffer before React commits a keystroke", () => {
    render(<Editor />);
    (screen.getByRole("textbox") as HTMLTextAreaElement).value = "new input";
    expect(state()).toBe(true);
  });
  it("protects composition even before the buffer changes", () => {
    render(<Editor />);
    fireEvent.compositionStart(screen.getByRole("textbox"));
    expect(state()).toBe(true);
    fireEvent.compositionEnd(screen.getByRole("textbox"));
    expect(state()).toBe(false);
  });
  it("keeps pending and failed saves protected, then uses the saved baseline", () => {
    const view = render(<Editor body="draft" pending />);
    expect(state()).toBe(true);
    view.rerender(<Editor body="draft" pending={false} />);
    expect(state()).toBe(true);
    view.rerender(<Editor body="draft" saved="draft" />);
    expect(state()).toBe(false);
  });
  it("does not clear newer edits when an older save finishes", () => {
    const view = render(<Editor body="draft" pending />);
    (screen.getByRole("textbox") as HTMLTextAreaElement).value = "newer draft";
    view.rerender(<Editor body="newer draft" saved="draft" />);
    expect(state()).toBe(true);
  });
  it("permits leaving after confirmed deletion", () => {
    render(<Editor body="draft" deleted />);
    expect(state()).toBe(false);
  });
});

function Workspace({ path = "/empty", editor = false, draft = "saved" }) {
  useNativeWorkspaceState(path);
  return editor ? <Editor body={draft} /> : null;
}

const workspaceState = () => window.__trackNativeWorkspaceState?.();

describe("native workspace termination state", () => {
  it("reports an empty tab strip clean only while the shell is mounted", () => {
    expect(workspaceState()).toBeUndefined();
    const view = render(<Workspace />);
    expect(workspaceState()).toBe(false);
    view.unmount();
    expect(workspaceState()).toBeUndefined();
  });
  it("reads the editor on each query and returns to clean after it unmounts", () => {
    const view = render(<Workspace editor draft="draft" />);
    expect(workspaceState()).toBe(true);
    view.rerender(<Workspace />);
    expect(workspaceState()).toBe(false);
    view.rerender(<Workspace editor />);
    expect(workspaceState()).toBe(false);
  });
  it("does not claim unknown or voice views are clean", () => {
    const view = render(<Workspace path="/voice" />);
    expect(workspaceState()).toBeNull();
    view.rerender(<Workspace path="/other" />);
    expect(workspaceState()).toBeNull();
    view.rerender(<Workspace path="/graph" />);
    expect(workspaceState()).toBe(false);
  });
});
