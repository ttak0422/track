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

  it("disables itself when the browser has no recognition API", () => {
    const { result } = renderHook(() => useSpeechRecognition());
    expect(result.current.isSupported).toBe(false);
    act(() => result.current.start());
    expect(result.current.isListening).toBe(false);
  });
});
