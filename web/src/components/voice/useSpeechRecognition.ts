import { useCallback, useEffect, useRef, useState } from "react";

export interface SpeechRecognitionLike {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  start(): void;
  stop(): void;
  onresult: ((event: SpeechResultEvent) => void) | null;
  onend: (() => void) | null;
  onerror: ((event: SpeechErrorEvent) => void) | null;
}

interface SpeechResultEvent {
  resultIndex: number;
  results: ArrayLike<{ isFinal: boolean; 0: { transcript: string } }>;
}
interface SpeechErrorEvent { error: string }
interface SpeechRecognitionConstructor { new (): SpeechRecognitionLike }

declare global {
  interface Window {
    SpeechRecognition?: SpeechRecognitionConstructor;
    webkitSpeechRecognition?: SpeechRecognitionConstructor;
  }
}

const restartDelay = 300;

export function useSpeechRecognition() {
  const supported = typeof window !== "undefined" && Boolean(window.webkitSpeechRecognition || window.SpeechRecognition);
  const [isListening, setIsListening] = useState(false);
  const [finalText, setFinalText] = useState("");
  const [interimText, setInterimText] = useState("");
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const listeningRef = useRef(false);
  const stopRequested = useRef(false);
  const backoff = useRef(500);
  const timer = useRef<number | undefined>(undefined);

  const clearRestart = useCallback(() => {
    if (timer.current !== undefined) window.clearTimeout(timer.current);
    timer.current = undefined;
  }, []);

  const makeRecognition = useCallback(() => {
    const Constructor = typeof window === "undefined" ? undefined : (window.webkitSpeechRecognition || window.SpeechRecognition);
    if (!Constructor) return null;
    const recognition = new Constructor();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = "ja-JP";
    recognition.onresult = (event) => {
      let interim = "";
      let finals = "";
      for (let index = event.resultIndex; index < event.results.length; index += 1) {
        const result = event.results[index];
        if (result.isFinal) finals += result[0].transcript;
        else interim += result[0].transcript;
      }
      if (finals) setFinalText((current) => current + finals);
      setInterimText(interim);
    };
    recognition.onerror = (event) => {
      if (stopRequested.current || event.error === "not-allowed" || event.error === "service-not-allowed") {
        listeningRef.current = false;
        setIsListening(false);
        return;
      }
      // no-speech/audio-busy are normal pauses; network gets a bounded backoff.
      const delay = event.error === "network" ? backoff.current : restartDelay;
      if (event.error === "network") backoff.current = Math.min(backoff.current * 2, 8000);
      clearRestart();
      timer.current = window.setTimeout(() => {
        if (listeningRef.current && !stopRequested.current) {
          const next = makeRecognition();
          recognitionRef.current = next;
          try { next?.start(); } catch { /* onend will retry */ }
        }
      }, delay);
    };
    recognition.onend = () => {
      if (!listeningRef.current || stopRequested.current) return;
      // The instance is dead: its interim belongs to no live session, so drop
      // it rather than showing a stale tail until the next result lands.
      setInterimText("");
      clearRestart();
      timer.current = window.setTimeout(() => {
        if (!listeningRef.current || stopRequested.current) return;
        const next = makeRecognition();
        recognitionRef.current = next;
        try { next?.start(); } catch { /* a later end/error retries */ }
      }, restartDelay);
    };
    return recognition;
  }, [clearRestart]);

  const start = useCallback(() => {
    if (!supported || listeningRef.current) return;
    stopRequested.current = false;
    listeningRef.current = true;
    backoff.current = 500;
    setIsListening(true);
    const recognition = makeRecognition();
    recognitionRef.current = recognition;
    try { recognition?.start(); } catch { /* browser will issue end/error */ }
  }, [makeRecognition, supported]);

  const stop = useCallback(() => {
    if (!listeningRef.current) return;
    stopRequested.current = true;
    listeningRef.current = false;
    clearRestart();
    setInterimText("");
    recognitionRef.current?.stop();
    // Let the browser deliver the final result before exposing the stopped state.
    window.setTimeout(() => setIsListening(false), 300);
  }, [clearRestart]);

  useEffect(() => () => {
    listeningRef.current = false;
    stopRequested.current = true;
    clearRestart();
    recognitionRef.current?.stop();
  }, [clearRestart]);

  return { isSupported: supported, isListening, finalText, interimText, transcript: finalText + interimText, setFinalText, start, stop };
}
