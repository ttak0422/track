import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { SlideDeckControl } from "./SlideDeck";

const deckMarkdown = "# First\n\n---\n\n# Second\n\n---\n\n# Third";

describe("SlideDeckControl", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  it("renders nothing for a note without separators", () => {
    const { container } = render(<SlideDeckControl markdown="just a note" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("opens the deck from the toggle and navigates with keys, mirroring the hash", () => {
    render(<SlideDeckControl markdown={deckMarkdown} />);

    fireEvent.click(screen.getByRole("button", { name: "Slides" }));
    expect(window.location.hash).toBe("#slide=1");
    const deck = screen.getByRole("dialog", { name: "Slide view" });
    expect(deck).toHaveTextContent("First");
    expect(deck).not.toHaveTextContent("Second");

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(window.location.hash).toBe("#slide=2");
    expect(screen.getByRole("dialog")).toHaveTextContent("Second");

    fireEvent.keyDown(window, { key: "End" });
    expect(window.location.hash).toBe("#slide=3");
    // The last slide clamps: another "next" stays put.
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(window.location.hash).toBe("#slide=3");

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(window.location.hash).toBe("");
  });

  it("opens directly from a #slide=N hash, clamping past-the-end numbers", () => {
    window.history.replaceState(null, "", "/#slide=9");
    render(<SlideDeckControl markdown={deckMarkdown} />);
    expect(screen.getByRole("dialog")).toHaveTextContent("Third");
    expect(screen.getByText("3 / 3")).toBeInTheDocument();
  });

  it("closes when the hash is removed externally (browser Back)", () => {
    window.history.replaceState(null, "", "/#slide=1");
    render(<SlideDeckControl markdown={deckMarkdown} />);
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    window.history.replaceState(null, "", "/");
    fireEvent(window, new HashChangeEvent("hashchange"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
