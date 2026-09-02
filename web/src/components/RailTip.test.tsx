import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RailTip } from "./RailTip";

function renderTip() {
  // A button rather than a Link: jsdom never marks a programmatically focused anchor :focus-visible,
  // so the keyboard test below could not run against one. The journal button is the same shape.
  return render(
    <aside className="sidebar">
      <RailTip label="Calendar">
        <button className="rail-button" type="button" aria-label="Calendar" />
      </RailTip>
    </aside>,
  );
}

describe("RailTip", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("portals the label beside the trigger on hover, then closes after leaving", () => {
    vi.useFakeTimers();
    renderTip();
    const trigger = screen.getByRole("button", { name: "Calendar" });

    fireEvent.pointerEnter(trigger);
    const tip = screen.getByText("Calendar");
    expect(tip).toHaveClass("rail-tip");
    expect(tip).toHaveAttribute("aria-hidden", "true");
    // The fixed rail clips its overflow and owns a stacking context below floating previews, so the
    // label has to be a body child to escape both.
    expect(tip.parentElement).toBe(document.body);
    expect(tip.closest(".sidebar")).toBeNull();

    fireEvent.pointerLeave(trigger);
    act(() => vi.advanceTimersByTime(200));
    expect(screen.queryByText("Calendar")).not.toBeInTheDocument();
  });

  // A tap fires pointerenter on the way down and pointerleave on the way up all the same; a touch
  // pointer cannot hover, so it gets none of this and the click keeps the whole job.
  it("ignores a touch pointer", () => {
    renderTip();
    const trigger = screen.getByRole("button", { name: "Calendar" });

    fireEvent.pointerEnter(trigger, { pointerType: "touch" });
    fireEvent.pointerLeave(trigger, { pointerType: "touch" });
    expect(screen.queryByText("Calendar")).not.toBeInTheDocument();
  });

  it("opens from keyboard focus and closes when focus leaves", () => {
    renderTip();
    const trigger = screen.getByRole("button", { name: "Calendar" });

    // A real focus, not a synthesized event: what opens the label is keyboard focus specifically
    // (:focus-visible), and only actually focusing the control puts it in that state.
    act(() => trigger.focus());
    expect(screen.getByText("Calendar")).toBeInTheDocument();

    act(() => trigger.blur());
    expect(screen.queryByText("Calendar")).not.toBeInTheDocument();
  });
});
