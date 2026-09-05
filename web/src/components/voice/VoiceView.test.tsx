import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VoiceView } from "./VoiceView";

vi.mock("./useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({ isSupported: true, isListening: false, transcript: "", start: vi.fn(), stop: vi.fn() }),
}));
vi.mock("../../api", () => ({ resolveTerm: vi.fn().mockResolvedValue({ found: false, note: { note_id: "", title: "" } }), searchNotes: vi.fn().mockResolvedValue({ results: [] }), openJournal: vi.fn() }));
vi.mock("../../queries", () => ({ useNoteQuery: () => ({ data: undefined }), useSaveNoteMutation: () => ({ isPending: false, mutate: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: vi.fn() }) }));

describe("VoiceView", () => {
  it("renders the microphone, transcript, link, and save controls", () => {
    render(<VoiceView />);
    expect(screen.getByRole("heading", { name: "Voice input" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Start microphone/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Find link" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save transcript" })).toBeDisabled();
  });

  it("allows transcript text and a destination title to be entered", () => {
    render(<VoiceView />);
    fireEvent.change(screen.getByLabelText("Save to note"), { target: { value: "Ideas" } });
    fireEvent.change(screen.getByLabelText("Transcript"), { target: { value: "spoken words" } });
    expect(screen.getByRole("button", { name: "Save transcript" })).toBeEnabled();
  });
});
