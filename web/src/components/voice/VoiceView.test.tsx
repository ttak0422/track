import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VoiceView } from "./VoiceView";

const { recognitionState, apiMocks } = vi.hoisted(() => ({
  recognitionState: { isListening: false, finalText: "", interimText: "" },
  apiMocks: {
    resolveTerm: (...args: unknown[]): Promise<any> => Promise.resolve({ found: false, note: { note_id: "", title: "" }, args }),
    searchNotes: (...args: unknown[]): Promise<any> => Promise.resolve({ results: [], args }),
    openJournal: (...args: unknown[]): Promise<any> => Promise.resolve({ note_id: "journal-1", args }),
    getNote: (...args: unknown[]): Promise<any> => Promise.resolve({ note: { body: "", etag: "etag-1" }, args }),
    saveNote: (...args: unknown[]): Promise<any> => Promise.resolve({ etag: "etag-2", args }),
  },
}));

const notify = vi.fn();
const stopFn = vi.fn();

vi.mock("./useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({
    isSupported: true,
    ...recognitionState,
    finalText: recognitionState.finalText,
    interimText: recognitionState.interimText,
    transcript: recognitionState.finalText + recognitionState.interimText,
    start: vi.fn(),
    stop: stopFn,
  }),
}));
vi.mock("../../api", () => ({
  resolveTerm: (...args: unknown[]) => apiMocks.resolveTerm(...args),
  searchNotes: (...args: unknown[]) => apiMocks.searchNotes(...args),
  openJournal: (...args: unknown[]) => apiMocks.openJournal(...args),
  getNote: (...args: unknown[]) => apiMocks.getNote(...args),
  saveNote: (...args: unknown[]) => apiMocks.saveNote(...args),
}));
vi.mock("../../notifications", () => ({ useNotifications: () => ({ notification: null, notify, dismiss: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: vi.fn() }) }));

function resetAll() {
  recognitionState.isListening = false;
  recognitionState.finalText = "";
  recognitionState.interimText = "";
  notify.mockClear();
  stopFn.mockClear();
  apiMocks.resolveTerm = (...args: unknown[]) => Promise.resolve({ found: false, note: { note_id: "", title: "" }, args });
  apiMocks.searchNotes = (...args: unknown[]) => Promise.resolve({ results: [], args });
  apiMocks.openJournal = (...args: unknown[]) => Promise.resolve({ note_id: "journal-1", args });
  apiMocks.getNote = (...args: unknown[]) => Promise.resolve({ note: { body: "", etag: "etag-1" }, args });
  apiMocks.saveNote = (...args: unknown[]) => Promise.resolve({ etag: "etag-2", args });
}

function transcript() {
  return screen.getByPlaceholderText("音声入力を開始してください…") as HTMLTextAreaElement;
}

describe("VoiceView", () => {
  it("renders one mic control and an English status, without save or link buttons", () => {
    resetAll();
    render(<VoiceView />);
    expect(screen.getByRole("heading", { name: "Voice input" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "音声入力を開始" })).toBeInTheDocument();
    expect(screen.getByText("Idle")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "今日のjournalへ保存" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "選択範囲をリンク" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Phase 1/)).not.toBeInTheDocument();
  });

  it("shows interim text in the same field, outside the confirmed value", () => {
    resetAll();
    recognitionState.interimText = "みかん";
    render(<VoiceView />);
    expect(transcript().value).toBe("みかん");
  });

  it("keeps a touched tail instead of duplicating it on finalize", () => {
    resetAll();
    recognitionState.finalText = "hello";
    recognitionState.interimText = " world";
    const view = render(<VoiceView />);
    expect(transcript().value).toBe("hello world");
    // The user confirms the tail by hand; it must not come back doubled.
    fireEvent.change(transcript(), { target: { value: "hello world!" } });
    recognitionState.interimText = "";
    recognitionState.finalText = "hello world";
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello world!");
  });

  it("starts a paused chunk on a fresh line while listening", () => {
    resetAll();
    recognitionState.isListening = true;
    recognitionState.finalText = "hello";
    const view = render(<VoiceView />);
    recognitionState.finalText = "hello world";
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld");
  });

  it("searches a selection by itself, with no tap on an action", async () => {
    resetAll();
    apiMocks.searchNotes = () => Promise.resolve({ results: [{ note_id: "n1", title: "T1" }] });
    render(<VoiceView />);
    fireEvent.change(transcript(), { target: { value: "spoken words" } });
    transcript().setSelectionRange(0, 6);
    fireEvent.mouseUp(transcript());
    expect(await screen.findByRole("button", { name: "T1" })).toBeInTheDocument();
  });

  it("appends the unsaved tail to today's journal when stopping", async () => {
    resetAll();
    recognitionState.isListening = true;
    render(<VoiceView />);
    fireEvent.change(transcript(), { target: { value: "dictated line" } });
    fireEvent.click(screen.getByRole("button", { name: "音声入力を停止" }));
    expect(stopFn).toHaveBeenCalled();
    await waitFor(() => expect(notify).toHaveBeenCalledWith("Saved to today’s journal", "journal-1"));
  });
});
