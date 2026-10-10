import { act, fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useReadingKeybindings } from "./useReadingKeybindings";

const state = vi.hoisted(() => ({
  tabs: [{ id: "a", title: "A" }, { id: "b", title: "B" }, { id: "c", title: "C" }],
  activeID: "a" as string | null,
  navigate: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => state.navigate }));
vi.mock("../components/tabs/tabsStore", () => ({
  useTabs: () => state,
  isViewTab: (id: string) => ["graph", "calendar", "markdown"].includes(id),
  tabRoute: (id: string) => ({ to: "/notes/$noteId", params: { noteId: id } }),
}));

function Reader({ reading = true, editor = false }) {
  const ref = useRef<HTMLElement>(null);
  const enabled = useReadingKeybindings(ref, reading);
  return <><button>Outside</button><section ref={ref} data-testid="reader" tabIndex={enabled ? 0 : undefined}>
    <div className="note-preview"><p>Prose</p><button><span>Control</span></button><a href="#heading">Heading link</a>
      <div contentEditable suppressContentEditableWarning>Editable</div><input aria-label="Field" /></div>
    {editor && <textarea aria-label="Editor" defaultValue="draft" />}
  </section></>;
}
const key = (target: Element, name: string, init: KeyboardEventInit = {}) => {
  const event = new KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true, ...init });
  act(() => { target.dispatchEvent(event); });
  return event;
};
function setup(props = {}) {
  const view = render(<Reader {...props} />);
  const reader = screen.getByTestId("reader");
  const scrollBy = vi.fn();
  reader.scrollBy = scrollBy;
  return { ...view, reader, scrollBy };
}

beforeEach(() => {
  window.__trackNativeReading = true;
  state.activeID = "a";
  state.tabs = [{ id: "a", title: "A" }, { id: "b", title: "B" }, { id: "c", title: "C" }];
  state.navigate.mockClear();
  window.getSelection()?.removeAllRanges();
});
afterEach(() => { delete window.__trackNativeReading; vi.restoreAllMocks(); });

describe("native reading shortcut context", () => {
  it("focuses prose on click and scrolls only the reader, including held keys", () => {
    const { reader, scrollBy } = setup();
    fireEvent.click(screen.getByText("Prose"));
    expect(reader).toHaveFocus();
    expect(key(reader, "j").defaultPrevented).toBe(true);
    key(reader, "j", { repeat: true });
    key(reader, "k");
    expect(scrollBy.mock.calls).toEqual([
      [{ top: 72, behavior: "instant" }], [{ top: 72, behavior: "instant" }], [{ top: -72, behavior: "instant" }],
    ]);
  });
  it("uses the rendered prose line-height, including font scaling", () => {
    const { reader, scrollBy } = setup();
    const prose = screen.getByText("Prose");
    prose.classList.add("markdown-view");
    prose.style.lineHeight = "36px";
    reader.focus();
    key(reader, "j");
    expect(scrollBy).toHaveBeenCalledWith({ top: 108, behavior: "instant" });
  });
  it("never claims ordinary web keyboard or pointer focus", () => {
    delete window.__trackNativeReading;
    const { reader, scrollBy } = setup();
    fireEvent.click(screen.getByText("Prose"));
    expect(reader).not.toHaveAttribute("tabindex");
    expect(reader).not.toHaveFocus();
    for (const name of ["j", "k", "Tab"]) expect(key(reader, name).defaultPrevented).toBe(false);
    expect(scrollBy).not.toHaveBeenCalled();
    expect(state.navigate).not.toHaveBeenCalled();
  });
  it("Escape returns Tab to focus navigation, and refocusing restores shortcuts", () => {
    const { reader } = setup();
    reader.focus();
    expect(key(reader, "Escape").defaultPrevented).toBe(true);
    expect(key(reader, "Tab").defaultPrevented).toBe(false);
    expect(key(reader, "Tab", { shiftKey: true }).defaultPrevented).toBe(false);
    expect(key(reader, "j").defaultPrevented).toBe(false);
    screen.getByText("Outside").focus();
    reader.focus();
    expect(key(reader, "j").defaultPrevented).toBe(true);
  });
  it("requires reader focus and preserves all control/editor input", () => {
    const { reader, scrollBy } = setup();
    expect(key(document.body, "j").defaultPrevented).toBe(false);
    for (const target of [screen.getByText("Control").parentElement!, screen.getByText("Heading link"),
      screen.getByText("Editable"), screen.getByLabelText("Field")]) {
      (target as HTMLElement).focus();
      fireEvent.click(target);
      expect(reader).not.toHaveFocus();
      for (const name of ["j", "k", "Tab"]) expect(key(target, name).defaultPrevented).toBe(false);
    }
    expect(scrollBy).not.toHaveBeenCalled();
    expect(state.navigate).not.toHaveBeenCalled();
  });
  it("stays inactive in edit/split mode, including clicks on its preview", () => {
    const { reader, scrollBy } = setup({ reading: false, editor: true });
    fireEvent.click(screen.getByText("Prose"));
    const editor = screen.getByLabelText("Editor");
    editor.focus();
    for (const name of ["j", "k", "Tab"]) expect(key(editor, name).defaultPrevented).toBe(false);
    expect(reader).not.toHaveAttribute("tabindex");
    expect(scrollBy).not.toHaveBeenCalled();
    expect(editor).toHaveValue("draft");
  });
  it("preserves selected text and modified or non-primary clicks", () => {
    const { reader } = setup();
    const prose = screen.getByText("Prose");
    for (const init of [{ button: 1 }, { shiftKey: true }, { metaKey: true }, { ctrlKey: true }, { altKey: true }]) {
      fireEvent.click(prose, init);
      expect(reader).not.toHaveFocus();
    }
    const range = document.createRange();
    range.selectNodeContents(prose);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);
    expect(window.getSelection()?.isCollapsed).toBe(false);
    fireEvent.click(prose);
    expect(reader).not.toHaveFocus();
    expect(window.getSelection()?.toString()).toBe("Prose");
  });
  it("keeps selection actions reachable when text is selected after reader focus", () => {
    const { reader, scrollBy } = setup();
    reader.focus();
    const range = document.createRange();
    range.selectNodeContents(screen.getByText("Prose"));
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);
    expect(window.getSelection()?.isCollapsed).toBe(false);
    for (const name of ["j", "k", "Tab"]) expect(key(reader, name).defaultPrevented).toBe(false);
    expect(scrollBy).not.toHaveBeenCalled();
    expect(state.navigate).not.toHaveBeenCalled();
  });
  it("blocks dialogs, search, and hover-open settings even when reader keeps focus", () => {
    const { reader, scrollBy } = setup();
    reader.focus();
    for (const html of ['<div role="dialog"></div>', '<div class="menu-panel"></div>', '<div class="selection-copy"></div>']) {
      const overlay = document.createElement("div");
      overlay.innerHTML = html;
      document.body.append(overlay);
      for (const name of ["j", "Tab", "Escape"]) expect(key(reader, name).defaultPrevented).toBe(false);
      overlay.remove();
    }
    expect(scrollBy).not.toHaveBeenCalled();
    expect(key(reader, "j").defaultPrevented).toBe(true); // overlay Escape did not release reader
  });
  it("blocks active composition and handles canceled composition without a stale state", () => {
    const { reader } = setup();
    reader.focus();
    fireEvent.compositionStart(reader);
    expect(key(reader, "j").defaultPrevented).toBe(false);
    expect(key(reader, "Tab").defaultPrevented).toBe(false);
    fireEvent.compositionEnd(reader);
    expect(key(reader, "j").defaultPrevented).toBe(true);
  });
  it("skips view tabs, leaves one-note Tab alone, and permits home-note scrolling", () => {
    state.tabs = [{ id: "a", title: "A" }, { id: "graph", title: "Graph" }];
    const { reader, scrollBy, rerender } = setup();
    reader.focus();
    expect(key(reader, "Tab").defaultPrevented).toBe(false);
    state.activeID = null;
    rerender(<Reader />);
    expect(key(reader, "Tab").defaultPrevented).toBe(false);
    key(reader, "j");
    expect(scrollBy).toHaveBeenCalledOnce();
  });
  it("does not repeat navigation or advance past an unobserved destination", async () => {
    let finish!: () => void;
    state.navigate.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
    const { reader } = setup();
    reader.focus();
    key(reader, "Tab");
    key(reader, "Tab", { repeat: true });
    expect(state.navigate).toHaveBeenCalledTimes(1);
    key(reader, "Tab");
    expect(state.navigate.mock.calls.map(([target]) => target.params.noteId)).toEqual(["b", "b"]);
    await act(async () => { finish(); });
  });
  it("removes document listeners on unmount", () => {
    const added = vi.spyOn(document, "addEventListener");
    const removed = vi.spyOn(document, "removeEventListener");
    const { reader, unmount, scrollBy } = setup();
    reader.focus();
    unmount();
    key(reader, "j");
    expect(scrollBy).not.toHaveBeenCalled();
    for (const type of ["keydown", "click", "compositionstart", "compositionend"]) {
      const subscription = added.mock.calls.find(([name]) => name === type)!;
      expect(removed).toHaveBeenCalledWith(...subscription);
    }
  });
});
