import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { APIError } from "../../api";
import { VoiceView } from "./VoiceView";

const { recognitionState, apiMocks } = vi.hoisted(() => ({
  recognitionState: { isListening: false, finalText: "", interimText: "" },
  apiMocks: {
    resolveTerm: (...args: unknown[]): Promise<any> => Promise.resolve({ found: false, note: { note_id: "", title: "" }, args }),
    searchNotes: (...args: unknown[]): Promise<any> => Promise.resolve({ results: [], args }),
    openJournal: (...args: unknown[]): Promise<any> => Promise.resolve({ note_id: "journal-1", args }),
    getNote: (...args: unknown[]): Promise<any> => Promise.resolve({ note: { body: "", etag: "etag-1" }, args }),
    saveNote: (...args: unknown[]): Promise<any> => Promise.resolve({ etag: "etag-2", args }),
    createNote: (...args: unknown[]): Promise<any> => Promise.resolve({ note_id: "n9", title: "spoken", args }),
  },
}));

const notify = vi.fn();
const stopFn = vi.fn();
const floatingOpen = vi.fn();

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
vi.mock("../../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api")>();
  return {
    ...actual,
    resolveTerm: (...args: unknown[]) => apiMocks.resolveTerm(...args),
    searchNotes: (...args: unknown[]) => apiMocks.searchNotes(...args),
    openJournal: (...args: unknown[]) => apiMocks.openJournal(...args),
    getNote: (...args: unknown[]) => apiMocks.getNote(...args),
    saveNote: (...args: unknown[]) => apiMocks.saveNote(...args),
    createNote: (...args: unknown[]) => apiMocks.createNote(...args),
  };
});
vi.mock("../../notifications", () => ({ useNotifications: () => ({ notification: null, notify, dismiss: vi.fn() }) }));
vi.mock("../preview/floatingStore", () => ({ useFloating: () => ({ open: floatingOpen }) }));

function resetAll() {
  recognitionState.isListening = false;
  recognitionState.finalText = "";
  recognitionState.interimText = "";
  notify.mockClear();
  stopFn.mockClear();
  floatingOpen.mockClear();
  apiMocks.resolveTerm = (...args: unknown[]) => Promise.resolve({ found: false, note: { note_id: "", title: "" }, args });
  apiMocks.searchNotes = (...args: unknown[]) => Promise.resolve({ results: [], args });
  apiMocks.openJournal = (...args: unknown[]) => Promise.resolve({ note_id: "journal-1", args });
  apiMocks.getNote = (...args: unknown[]) => Promise.resolve({ note: { body: "", etag: "etag-1" }, args });
  apiMocks.saveNote = (...args: unknown[]) => Promise.resolve({ etag: "etag-2", args });
  apiMocks.createNote = (...args: unknown[]) => Promise.resolve({ note_id: "n9", title: "spoken", args });
}

function transcript() {
  return screen.getByPlaceholderText("音声入力を開始してください…") as HTMLTextAreaElement;
}

describe("VoiceView", () => {
  it("renders one mic control and an English status, without a title or extra buttons", () => {
    resetAll();
    render(<VoiceView />);
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "音声入力を開始" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "今日のjournalへ保存" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "選択範囲をリンク" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Phase 1/)).not.toBeInTheDocument();
  });

  it("shows interim text in the same field, wearing the faint step", () => {
    resetAll();
    recognitionState.interimText = "みかん";
    const { container } = render(<VoiceView />);
    expect(transcript().value).toBe("みかん");
    expect(container.querySelector(".voice-interim-faint")).toHaveTextContent("みかん");
  });

  it("keeps a touched tail instead of duplicating it on finalize", () => {
    resetAll();
    recognitionState.finalText = "hello";
    recognitionState.interimText = " world";
    const view = render(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld");
    // The user confirms the tail by hand; it must not come back doubled.
    fireEvent.change(transcript(), { target: { value: "hello\nworld!" } });
    recognitionState.interimText = "";
    recognitionState.finalText = "hello world";
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld!");
  });

  it("closes every finalized chunk with a line break, listening or not", () => {
    resetAll();
    recognitionState.isListening = true;
    recognitionState.finalText = "hello";
    const view = render(<VoiceView />);
    expect(transcript().value).toBe("hello\n");
    recognitionState.finalText = "hello world";
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld\n");
  });

  it("freezes the field while a selection drag is in flight", () => {
    resetAll();
    recognitionState.finalText = "hello";
    const view = render(<VoiceView />);
    const area = transcript();
    fireEvent.mouseDown(area);
    recognitionState.interimText = " world";
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\n");
    fireEvent.mouseUp(area);
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld");
  });

  it("keeps the mirror on the frozen string mid-drag, and releases off-field", () => {
    resetAll();
    recognitionState.finalText = "hello";
    const view = render(<VoiceView />);
    const area = transcript();
    fireEvent.mouseDown(area);
    recognitionState.interimText = " world";
    view.rerender(<VoiceView />);
    expect(view.container.querySelector(".voice-transcript-mirror")?.textContent).toBe("hello\n\u200b");
    window.dispatchEvent(new window.MouseEvent("mouseup", { bubbles: true }));
    view.rerender(<VoiceView />);
    expect(transcript().value).toBe("hello\nworld");
  });

  it("shows elapsed recording time beside the mic, with no cap", () => {
    resetAll();
    recognitionState.isListening = true;
    render(<VoiceView />);
    expect(screen.getByRole("timer")).toHaveTextContent("00:00");
    expect(screen.queryByText(/60:00/)).not.toBeInTheDocument();
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

  it("offers creation when nothing matches, and opens the new note", async () => {
    resetAll();
    render(<VoiceView />);
    fireEvent.change(transcript(), { target: { value: "spoken words" } });
    transcript().setSelectionRange(0, 6);
    fireEvent.mouseUp(transcript());
    fireEvent.click(await screen.findByRole("button", { name: "「spoken」を新規作成" }));
    await waitFor(() => expect(floatingOpen).toHaveBeenCalled());
    expect(notify).toHaveBeenCalledWith("「spoken」を作成しました", "n9");
  });

  it("toasts a duplicate title instead of failing the creation", async () => {
    resetAll();
    apiMocks.createNote = () => Promise.reject(new APIError(409, "note already exists"));
    render(<VoiceView />);
    fireEvent.change(transcript(), { target: { value: "spoken words" } });
    transcript().setSelectionRange(0, 6);
    fireEvent.mouseUp(transcript());
    fireEvent.click(await screen.findByRole("button", { name: "「spoken」を新規作成" }));
    await waitFor(() => expect(notify).toHaveBeenCalledWith("同名タイトルのノートが存在します"));
    expect(floatingOpen).not.toHaveBeenCalled();
  });
});
