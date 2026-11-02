import { render } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";
import { ReadingPositionBoundary } from "./ReadingPositionBoundary";

type FixtureProps = { warning?: boolean; scope?: string; revision?: string; split?: boolean; compensate?: boolean; id?: string; upstream?: boolean };
function Fixture({ warning = false, scope = "a", revision = String(warning), split = false, compensate = true, id = "reader", upstream = false }: FixtureProps) {
  const root = useRef<HTMLElement>(null);
  const notice = warning && <div data-testid={`${id}-warning`}>Could not refresh. Retry</div>;
  const content = <article ref={root}>
    {upstream && notice}
    <div data-testid={`${id}-inner`} style={split ? { overflowY: "auto" } : undefined}>
      {!upstream && notice}
      <div className="markdown-view"><p data-testid={`${id}-anchor`}>The passage being read</p></div>
    </div>
  </article>;
  return <div data-testid={id} style={{ overflowY: "auto" }}>
    {compensate ? <ReadingPositionBoundary root={root} scope={scope} revision={revision}>{content}</ReadingPositionBoundary> : content}
  </div>;
}

// jsdom has no box layout, native scroll clamping, or scroll anchoring. The fixture's real React
// DOM mutation drives this explicit geometry model: a warning adds 80px above the visible passage;
// a 400px viewport clamps scrollTop to its current document height. This tests lifecycle ordering
// and pixel compensation deterministically, not actual browser/WebKit behavior.
function geometry(container: HTMLElement, id = "reader", { split = false, initial = 280, offset = 300, nativeAnchoring = false, movingViewport = false } = {}) {
  const outer = container.querySelector<HTMLElement>(`[data-testid="${id}"]`)!;
  const inner = container.querySelector<HTMLElement>(`[data-testid="${id}-inner"]`)!;
  const anchor = container.querySelector<HTMLElement>(`[data-testid="${id}-anchor"]`)!;
  const scroller = split ? inner : outer;
  let scroll = initial;
  let previousWarning = !!container.querySelector(`[data-testid="${id}-warning"]`);
  const warningHeight = () => container.querySelector(`[data-testid="${id}-warning"]`) ? 80 : 0;
  const contentWarningHeight = () => movingViewport ? 0 : warningHeight();
  const viewportTop = () => 100 + (movingViewport ? warningHeight() : 0);
  const syncLayout = () => {
    const hasWarning = warningHeight() > 0;
    if (nativeAnchoring && hasWarning !== previousWarning) scroll += hasWarning ? 80 : -80;
    previousWarning = hasWarning;
    scroll = Math.max(0, Math.min(scroll, 1000 + contentWarningHeight() - 400));
  };
  const rect = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 600, width: 600, x: 0, y: top, toJSON() {} });
  Object.defineProperties(scroller, {
    clientHeight: { configurable: true, get: () => 400 },
    scrollHeight: { configurable: true, get: () => 1000 + contentWarningHeight() },
    scrollTop: { configurable: true, get: () => { syncLayout(); return scroll; }, set: (value: number) => { scroll = value; syncLayout(); } },
  });
  scroller.getBoundingClientRect = () => rect(viewportTop(), 400);
  anchor.getBoundingClientRect = () => { syncLayout(); return rect(viewportTop() + offset + contentWarningHeight() - scroll, 40); };
  if (split) {
    Object.defineProperties(outer, {
      clientHeight: { configurable: true, value: 500 }, scrollHeight: { configurable: true, value: 1800 },
      scrollTop: { configurable: true, writable: true, value: 250 },
    });
    outer.getBoundingClientRect = () => rect(0, 500);
  }
  return { anchor, scroller, outer, pixel: () => anchor.getBoundingClientRect().top - scroller.getBoundingClientRect().top };
}

describe("ReadingPositionBoundary health layout changes", () => {
  it("has an unwrapped negative control: inserting the warning displaces the visible passage", () => {
    const view = render(<Fixture compensate={false} />);
    const model = geometry(view.container);
    const before = model.pixel();
    view.rerender(<Fixture compensate={false} warning />);
    expect(model.pixel()).toBe(before + 80);
    expect(model.scroller.scrollTop).toBe(280);
  });

  it("keeps the same passage pixel when warnings appear and disappear mid-page", () => {
    const view = render(<Fixture />);
    const model = geometry(view.container);
    const before = model.pixel();
    view.rerender(<Fixture warning />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(360);
    view.rerender(<Fixture />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(280);
  });

  it("uses the passage pixel rather than an already clamped scrollTop near the bottom", () => {
    const view = render(<Fixture warning />);
    const model = geometry(view.container, "reader", { initial: 650, offset: 900 });
    const before = model.pixel();
    view.rerender(<Fixture />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(570);
  });

  it("anchors the user's current position after scrolling between retries", () => {
    const view = render(<Fixture />);
    const model = geometry(view.container);
    view.rerender(<Fixture warning />);
    model.scroller.scrollTop = 340;
    const before = model.pixel();
    view.rerender(<Fixture />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(260);
  });

  it("does not compensate navigation, unchanged health revisions, or the top of a page", () => {
    const view = render(<Fixture revision="stable" />);
    const model = geometry(view.container);
    const before = model.pixel();
    view.rerender(<Fixture warning revision="stable" />);
    expect(model.pixel()).toBe(before + 80);
    expect(model.scroller.scrollTop).toBe(280);
    view.rerender(<Fixture scope="b" revision="changed" />);
    expect(model.scroller.scrollTop).toBe(280);
    model.scroller.scrollTop = 0;
    view.rerender(<Fixture scope="b" revision="again" warning />);
    expect(model.scroller.scrollTop).toBe(0);
  });

  it("does not apply a second adjustment after native anchoring already preserved the passage", () => {
    const view = render(<Fixture />);
    const model = geometry(view.container, "reader", { nativeAnchoring: true });
    const before = model.pixel();
    view.rerender(<Fixture warning />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(360);
    view.rerender(<Fixture />);
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(280);
  });

  it("adjusts only the nearest split scroller and leaves an independent floating pane untouched", () => {
    const panes = (warning: boolean) => <><Fixture split warning={warning} /><Fixture id="floating" /></>;
    const view = render(panes(false));
    const model = geometry(view.container, "reader", { split: true });
    const floating = geometry(view.container, "floating", { initial: 270 });
    const before = model.pixel();
    const floatingBefore = floating.pixel();
    view.rerender(panes(true));
    expect(model.pixel()).toBe(before);
    expect(model.scroller.scrollTop).toBe(360);
    expect(model.outer.scrollTop).toBe(250);
    expect(floating.pixel()).toBe(floatingBefore);
    expect(floating.scroller.scrollTop).toBe(270);
  });

  it("does not scroll a passage behind a split viewport that moves with an upstream health notice", () => {
    const view = render(<Fixture split upstream />);
    // Here the geometry represents a notice moving the whole split viewport downward, not adding
    // content inside its scroll area. Both anchor and viewport move by 80px in document coordinates.
    const model = geometry(view.container, "reader", { split: true, movingViewport: true });
    const before = model.pixel();
    view.rerender(<Fixture split warning upstream />);
    expect(model.scroller.getBoundingClientRect().top).toBe(180);
    expect(model.pixel()).toBe(before);
    expect(model.pixel()).toBeGreaterThanOrEqual(0);
    expect(model.scroller.scrollTop).toBe(280);
    expect(model.outer.scrollTop).toBe(250);
  });
});
