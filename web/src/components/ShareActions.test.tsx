import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ShareActions, publishedNoteURL } from "./ShareActions";

const copyText = vi.hoisted(() => vi.fn());

vi.mock("./markdown/clipboard", () => ({ copyText }));

describe("ShareActions", () => {
  it("builds the published note URL and an X intent link", () => {
    expect(publishedNoteURL("note-1", "https://example.com/site/")).toBe(
      "https://example.com/site/notes/note-1/",
    );

    render(<ShareActions noteID="note-1" title="A note" baseURL="https://example.com/site" />);
    const xLink = screen.getByRole("link", { name: "Share on X" });
    const intent = new URL(xLink.getAttribute("href") ?? "");

    expect(xLink.querySelector("svg")).toHaveClass("share-action-icon");
    expect(intent.origin + intent.pathname).toBe("https://x.com/intent/tweet");
    expect(intent.searchParams.get("text")).toBe(
      "A note\n\nhttps://example.com/site/notes/note-1/",
    );
    expect(intent.searchParams.get("url")).toBeNull();
  });

  it("copies the published URL and acknowledges success", async () => {
    copyText.mockResolvedValue(true);
    render(<ShareActions noteID="note-1" title="A note" baseURL="https://example.com" />);

    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("https://example.com/notes/note-1/"));
    expect(await screen.findByRole("button", { name: "Copied" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copied" }).querySelector("svg")).toHaveClass(
      "share-action-icon",
    );
  });

  it("falls back to the browser URL when no public base is configured", () => {
    render(<ShareActions noteID="note-1" title="A note" />);
    const xLink = screen.getByRole("link", { name: "Share on X" });
    const intent = new URL(xLink.getAttribute("href") ?? "");
    expect(intent.searchParams.get("text")).toBe(`A note\n\n${window.location.href}`);
  });
});
