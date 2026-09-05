import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VoiceView } from "./VoiceView";

const notify = vi.fn();

vi.mock("./useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({ isSupported: true, isListening: false, finalText: "", interimText: "", transcript: "", start: vi.fn(), stop: vi.fn() }),
}));
vi.mock("../../api", () => ({ resolveTerm: vi.fn().mockResolvedValue({ found: false, note: { note_id: "", title: "" } }), searchNotes: vi.fn().mockResolvedValue({ results: [] }), openJournal: vi.fn() }));
vi.mock("../../notifications", () => ({ useNotifications: () => ({ notification: null, notify, dismiss: vi.fn() }) }));
vi.mock("../../queries", () => ({ useNoteQuery: () => ({ data: undefined }), useSaveNoteMutation: () => ({ isPending: false, mutate: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: vi.fn() }) }));

describe("VoiceView", () => {
  it("renders the microphone, transcript, and save controls without header clutter", () => {
    render(<VoiceView />);
    expect(screen.getByRole("heading", { name: "Voice input" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /音声入力を開始/ })).toBeEnabled();
    expect(screen.getByRole("button", { name: "今日のjournalへ保存" })).toBeDisabled();
    expect(screen.queryByText(/Phase 1/)).not.toBeInTheDocument();
    expect(screen.queryByText(/60:00/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "選択範囲をリンク" })).not.toBeInTheDocument();
  });

  it("enables journal saving after transcript text is entered", () => {
    render(<VoiceView />);
    fireEvent.change(screen.getByPlaceholderText("音声入力を開始してください…"), { target: { value: "spoken words" } });
    expect(screen.getByRole("button", { name: "今日のjournalへ保存" })).toBeEnabled();
  });

  it("opens a selection popover instead of a permanent link button", () => {
    render(<VoiceView />);
    const area = screen.getByPlaceholderText("音声入力を開始してください…") as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: "spoken words" } });
    area.setSelectionRange(0, 6);
    fireEvent.select(area);
    expect(screen.getByRole("button", { name: /「spoken」を検索/ })).toBeInTheDocument();
  });
});
