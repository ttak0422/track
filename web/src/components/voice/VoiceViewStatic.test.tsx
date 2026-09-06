import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VoiceView } from "./VoiceView";

vi.mock("../../runtime", () => ({ STATIC_MODE: true, START_PAGE_ID: "" }));
vi.mock("./useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({
    isSupported: true,
    isListening: false,
    finalText: "",
    interimText: "",
    transcript: "",
    start: vi.fn(),
    stop: vi.fn(),
  }),
}));
vi.mock("../../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api")>();
  return {
    ...actual,
    resolveTerm: () => Promise.resolve({ found: false, note: { note_id: "", title: "" } }),
    searchNotes: () => Promise.resolve({ results: [{ note_id: "t1", title: "TitleHit", match: "title" }] }),
    openJournal: () => Promise.resolve({ note_id: "journal-1" }),
    getNote: () => Promise.resolve({ note: { body: "", etag: "etag-1" } }),
    saveNote: () => Promise.resolve({ etag: "etag-2" }),
    createNote: () => Promise.resolve({ note_id: "n9", title: "spoken" }),
  };
});
vi.mock("../../notifications", () => ({ useNotifications: () => ({ notification: null, notify: vi.fn(), dismiss: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: vi.fn(), windows: [] }) }));

describe("VoiceView in static export", () => {
  it("shows hits but never the create-note UI", async () => {
    render(<VoiceView />);
    const area = screen.getByPlaceholderText("Start voice input…") as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: "spoken words" } });
    area.setSelectionRange(0, 6);
    fireEvent.mouseUp(area);
    expect(await screen.findByRole("button", { name: "TitleHit" })).toBeInTheDocument();
    expect(screen.queryByText("New note")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: 'Create "spoken"' })).not.toBeInTheDocument();
  });
});
