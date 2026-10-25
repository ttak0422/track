import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSpeechRecognition } from "./useSpeechRecognition";

class MockRecognition {
  static instances: MockRecognition[] = [];
  continuous = false;
  interimResults = false;
  lang = "";
  onresult: ((event: any) => void) | null = null;
  onend: (() => void) | null = null;
  onerror: ((event: any) => void) | null = null;
  start = vi.fn();
  stop = vi.fn();
  constructor() { MockRecognition.instances.push(this); }
}

describe("useSpeechRecognition", () => {
  afterEach(() => { MockRecognition.instances = []; delete (window as any).SpeechRecognition; vi.useRealTimers(); });

  it("keeps final and interim results separate and recreates after end", () => {
    vi.useFakeTimers();
    (window as any).SpeechRecognition = MockRecognition;
    const { result } = renderHook(() => useSpeechRecognition());
    act(() => result.current.start());
    const first = MockRecognition.instances[0];
    expect(first.lang).toBe("ja-JP");
    act(() => first.onresult?.({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: "確定" } }, { isFinal: false, 0: { transcript: "途中" } }] }));
    expect(result.current.transcript).toBe("確定途中");
    act(() => first.onend?.());
    act(() => vi.advanceTimersByTime(300));
    expect(MockRecognition.instances).toHaveLength(2);
    expect(result.current.finalText).toBe("確定");
  });

  it("drops stale interim when the session restarts or stops", () => {
    vi.useFakeTimers();
    (window as any).SpeechRecognition = MockRecognition;
    const { result } = renderHook(() => useSpeechRecognition());
    act(() => result.current.start());
    const first = MockRecognition.instances[0];
    act(() => first.onresult?.({ resultIndex: 0, results: [{ isFinal: false, 0: { transcript: "途中" } }] }));
    expect(result.current.interimText).toBe("途中");
    act(() => first.onend?.());
    act(() => vi.advanceTimersByTime(300));
    expect(MockRecognition.instances).toHaveLength(2);
    expect(result.current.interimText).toBe("");
    const second = MockRecognition.instances[1];
    act(() => second.onresult?.({ resultIndex: 0, results: [{ isFinal: false, 0: { transcript: "つづき" } }] }));
    expect(result.current.interimText).toBe("つづき");
    act(() => result.current.stop());
    expect(result.current.interimText).toBe("");
  });

  it("disables itself when the browser has no recognition API", () => {
    const { result } = renderHook(() => useSpeechRecognition());
    expect(result.current.isSupported).toBe(false);
    act(() => result.current.start());
    expect(result.current.isListening).toBe(false);
  });
});

describe("native speech backend", () => {
  function setupNative() {
    const postMessage = vi.fn();
    window.__trackNativeSpeech = { postMessage };
    (window as any).SpeechRecognition = MockRecognition;
    const hook = renderHook(() => useSpeechRecognition());
    return { ...hook, postMessage };
  }
  function event(id: string, type: string, text = "") {
    act(() => { window.dispatchEvent(new CustomEvent("track-native-speech", { detail: { id, type, text } })); });
  }
  afterEach(() => {
    delete window.__trackNativeSpeech;
    delete (window as any).SpeechRecognition;
    localStorage.clear();
    MockRecognition.instances = [];
    vi.useRealTimers();
  });

  it("defaults native to macOS, keeps explicit browser choice, and leaves web defaults unchanged", () => {
    const first = setupNative();
    expect(first.result.current.backend).toBe("macos");
    expect(first.postMessage).not.toHaveBeenCalled();
    act(() => first.result.current.selectBackend("browser"));
    first.unmount();
    const second = setupNative();
    expect(second.result.current.backend).toBe("browser");
    second.unmount();
    delete window.__trackNativeSpeech;
    const web = renderHook(() => useSpeechRecognition());
    expect(web.result.current.backend).toBe("browser");
    web.unmount();
    localStorage.setItem("track.voice.backend.native", "invalid");
    const restored = setupNative();
    expect(restored.result.current.backend).toBe("macos");
    restored.unmount();
  });

  it("replaces Japanese partial hypotheses, inserts final once, and accepts final after stop", () => {
    const { result, postMessage, unmount } = setupNative();
    act(() => result.current.start());
    const start = postMessage.mock.calls[0][0];
    expect(start).toMatchObject({ action: "start", locale: "ja-JP" });
    expect(MockRecognition.instances).toHaveLength(0);
    event(start.id, "partial", "に");
    event(start.id, "partial", "日本語");
    expect(result.current.transcript).toBe("日本語");
    act(() => result.current.stop());
    expect(postMessage).toHaveBeenLastCalledWith({ action: "stop", id: start.id });
    expect(result.current.interimText).toBe("");
    event(start.id, "final", "日本語です");
    event(start.id, "final", "日本語です");
    event(start.id, "ended");
    expect(result.current.transcript).toBe("日本語です");
    expect(result.current.isListening).toBe(false);
    unmount();
  });

  it("never falls back or retries after native permission or availability errors", () => {
    vi.useFakeTimers();
    const { result, postMessage, unmount } = setupNative();
    act(() => result.current.start());
    const { id } = postMessage.mock.calls[0][0];
    event(id, "error", "Speech Recognition is restricted");
    expect(result.current.error).toBe("Speech Recognition is restricted");
    expect(result.current.isListening).toBe(false);
    act(() => vi.advanceTimersByTime(10000));
    expect(MockRecognition.instances).toHaveLength(0);
    expect(postMessage.mock.calls.filter(([message]) => message.action === "start")).toHaveLength(1);
    expect(postMessage).toHaveBeenLastCalledWith({ action: "cancel", id });
    unmount();
  });

  it("cancels on unmount and backend changes, rejects stale callbacks, and restarts without duplicate listeners", () => {
    const remove = vi.spyOn(window, "removeEventListener");
    const { result, postMessage, unmount } = setupNative();
    act(() => result.current.start());
    const first = postMessage.mock.calls[0][0];
    event(first.id, "final", "一");
    event(first.id, "ended");
    act(() => result.current.start());
    const second = postMessage.mock.calls.find(([message]) => message.action === "start" && message.id !== first.id)![0];
    event(first.id, "final", "古い");
    event(second.id, "final", "二");
    expect(result.current.finalText).toBe("一二");
    act(() => result.current.selectBackend("browser"));
    expect(postMessage).toHaveBeenLastCalledWith({ action: "cancel", id: second.id });
    act(() => result.current.selectBackend("macos"));
    act(() => result.current.start());
    const last = postMessage.mock.calls.at(-1)![0];
    unmount();
    expect(postMessage).toHaveBeenLastCalledWith({ action: "cancel", id: last.id });
    expect(remove.mock.calls.filter(([name]) => name === "track-native-speech").length).toBeGreaterThanOrEqual(3);
    remove.mockRestore();
  });
});

describe("browser speech lifecycle", () => {
  afterEach(() => { MockRecognition.instances = []; delete (window as any).SpeechRecognition; localStorage.clear(); vi.useRealTimers(); });

  it("retires failed instances, rejects stale results and releases resources on cancel", () => {
    vi.useFakeTimers();
    (window as any).SpeechRecognition = MockRecognition;
    const { result, unmount } = renderHook(() => useSpeechRecognition());
    act(() => result.current.start());
    const first = MockRecognition.instances[0];
    const lateResult = first.onresult!;
    act(() => first.onerror?.({ error: "network" }));
    expect(first.stop).toHaveBeenCalledOnce();
    expect(first.onend).toBeNull();
    act(() => vi.advanceTimersByTime(500));
    expect(MockRecognition.instances).toHaveLength(2);
    act(() => lateResult({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: "stale" } }] }));
    expect(result.current.finalText).toBe("");
    const second = MockRecognition.instances[1];
    unmount();
    expect(second.stop).toHaveBeenCalledOnce();
    expect(second.onresult).toBeNull();
    act(() => vi.advanceTimersByTime(10000));
    expect(MockRecognition.instances).toHaveLength(2);
  });

  it("does not let an earlier stop timer change a restarted session", () => {
    vi.useFakeTimers();
    (window as any).SpeechRecognition = MockRecognition;
    const { result, unmount } = renderHook(() => useSpeechRecognition());
    act(() => result.current.start());
    act(() => result.current.stop());
    act(() => result.current.start());
    act(() => vi.advanceTimersByTime(300));
    expect(result.current.isListening).toBe(true);
    const current = MockRecognition.instances[1];
    const final = { resultIndex: 0, results: [{ isFinal: true, 0: { transcript: "確定" } }] };
    act(() => current.onresult?.(final));
    act(() => current.onresult?.(final));
    expect(result.current.finalText).toBe("確定");
    unmount();
  });
});
