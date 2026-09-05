import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VoiceView } from "./VoiceView";

vi.mock("./useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({ isSupported: true, isListening: false, finalText: "", interimText: "", transcript: "", start: vi.fn(), stop: vi.fn() }),
}));
vi.mock("../../api", () => ({ resolveTerm: vi.fn().mockResolvedValue({ found: false, note: { note_id: "", title: "" } }), searchNotes: vi.fn().mockResolvedValue({ results: [] }), openJournal: vi.fn() }));
vi.mock("../../queries", () => ({ useNoteQuery: () => ({ data: undefined }), useSaveNoteMutation: () => ({ isPending: false, mutate: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: vi.fn() }) }));

describe("VoiceView", () => {
  it("renders the microphone, transcript, link, and save controls", () => {
    render(<VoiceView />);
    expect(screen.getByRole("heading", { name: "Voice input" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /音声入力を開始/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: "選択範囲をリンク" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "今日のjournalへ保存" })).toBeDisabled();
  });

  it("enables journal saving after transcript text is entered", () => {
    render(<VoiceView />);
    fireEvent.change(screen.getByLabelText("文字起こし"), { target: { value: "spoken words" } });
    expect(screen.getByRole("button", { name: "今日のjournalへ保存" })).toBeEnabled();
  });
});
