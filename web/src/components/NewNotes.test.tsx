import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { NewNotes } from "./NewNotes";

const newNotes = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="#note">{children}</a>,
}));

vi.mock("../queries", () => ({ useNewNotesQuery: (limit?: number) => newNotes(limit) }));

describe("NewNotes", () => {
  it("shows the newest notes in the order returned by the engine", () => {
    newNotes.mockReturnValue({
      data: {
        notes: [
          { note_id: "3", file_kind: "note", title: "Newest" },
          { note_id: "2", file_kind: "note", title: "Earlier" },
        ],
      },
    });

    render(<NewNotes />);

    expect(newNotes).toHaveBeenCalledWith(10);
    expect(screen.getByRole("heading", { name: "New" })).toBeTruthy();
    expect(screen.getAllByRole("link").map((link) => link.textContent)).toEqual(["Newest", "Earlier"]);
  });

  it("does not reserve an empty panel while the listing loads or the vault is empty", () => {
    newNotes.mockReturnValue({ data: { notes: [] } });
    const { container } = render(<NewNotes />);
    expect(container.firstChild).toBeNull();
  });
});
