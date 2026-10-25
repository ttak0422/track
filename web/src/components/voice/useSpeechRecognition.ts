import { useCallback, useEffect, useRef, useState } from "react";
import { createNativeRecognition, hasNativeSpeech, readSpeechBackend, saveSpeechBackend, type SpeechBackend } from "./nativeSpeech";

export interface SpeechRecognitionLike {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  start(): void;
  stop(): void;
  abort?(): void;
  onresult: ((event: SpeechResultEvent) => void) | null;
  onend: (() => void) | null;
  onerror: ((event: SpeechErrorEvent) => void) | null;
}

interface SpeechResultEvent {
  resultIndex: number;
  results: ArrayLike<{ isFinal: boolean; 0: { transcript: string } }>;
}
interface SpeechErrorEvent { error: string; message?: string }
interface SpeechRecognitionConstructor { new (): SpeechRecognitionLike }

declare global {
  interface Window {
    SpeechRecognition?: SpeechRecognitionConstructor;
    webkitSpeechRecognition?: SpeechRecognitionConstructor;
  }
}

const restartDelay = 300;

function releaseRecognition(recognition: SpeechRecognitionLike | null) {
  if (!recognition) return;
  recognition.onresult = null;
  recognition.onend = null;
  recognition.onerror = null;
  if (recognition.abort) recognition.abort();
  else recognition.stop();
}

export function useSpeechRecognition() {
  const [backend, setBackend] = useState<SpeechBackend>(readSpeechBackend);
  const [error, setError] = useState("");
  const native = backend === "macos";
  const browserSupported = typeof window !== "undefined" && Boolean(window.webkitSpeechRecognition || window.SpeechRecognition);
  const supported = native ? hasNativeSpeech() : browserSupported;
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

  const launchRecognition = useCallback((recognition: SpeechRecognitionLike | null) => {
    recognitionRef.current = recognition;
    try { recognition?.start(); } catch (reason) {
      releaseRecognition(recognition);
      recognitionRef.current = null;
      listeningRef.current = false;
      setIsListening(false);
      setError(reason instanceof Error ? reason.message : "Speech recognition could not start. Try again.");
    }
  }, []);

  const makeRecognition = useCallback(() => {
    const Constructor = typeof window === "undefined" ? undefined : (window.webkitSpeechRecognition || window.SpeechRecognition);
    if (!native && !Constructor) return null;
    const recognition = native ? createNativeRecognition() : new Constructor!();
    const finalized = new Set<number>();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = "ja-JP";
    recognition.onresult = (event) => {
      if (recognitionRef.current !== recognition) return;
      let interim = "";
      let finals = "";
      for (let index = event.resultIndex; index < event.results.length; index += 1) {
        const result = event.results[index];
        if (result.isFinal) {
          if (!finalized.has(index)) finals += result[0].transcript;
          finalized.add(index);
        }
        else interim += result[0].transcript;
      }
      if (finals) setFinalText((current) => current + finals);
      setInterimText(interim);
    };
    recognition.onerror = (event) => {
      if (recognitionRef.current !== recognition) return;
      if (native || ["not-allowed", "service-not-allowed", "audio-capture", "language-not-supported"].includes(event.error)) {
        setError(event.message || `Browser speech recognition failed (${event.error}). Check microphone and speech permissions or choose another backend.`);
        listeningRef.current = false;
        setIsListening(false);
        setInterimText("");
        clearRestart();
        recognitionRef.current = null;
        releaseRecognition(recognition);
        return;
      }
      if (stopRequested.current) {
        listeningRef.current = false;
        setIsListening(false);
        return;
      }
      recognitionRef.current = null;
      releaseRecognition(recognition);
      // no-speech/audio-busy are normal pauses; network gets a bounded backoff.
      const delay = event.error === "network" ? backoff.current : restartDelay;
      if (event.error === "network") backoff.current = Math.min(backoff.current * 2, 8000);
      clearRestart();
      timer.current = window.setTimeout(() => {
        if (listeningRef.current && !stopRequested.current) {
          launchRecognition(makeRecognition());
        }
      }, delay);
    };
    recognition.onend = () => {
      if (recognitionRef.current !== recognition) return;
      if (native) {
        listeningRef.current = false;
        setIsListening(false);
        setInterimText("");
        recognitionRef.current = null;
        return;
      }
      recognitionRef.current = null;
      recognition.onresult = recognition.onerror = recognition.onend = null;
      if (!listeningRef.current || stopRequested.current) return;
      // The instance is dead: its interim belongs to no live session, so drop
      // it rather than showing a stale tail until the next result lands.
      setInterimText("");
      clearRestart();
      timer.current = window.setTimeout(() => {
        if (!listeningRef.current || stopRequested.current) return;
        launchRecognition(makeRecognition());
      }, restartDelay);
    };
    return recognition;
  }, [clearRestart, native, launchRecognition]);

  const start = useCallback(() => {
    if (!supported || listeningRef.current) return;
    clearRestart();
    setError("");
    stopRequested.current = false;
    listeningRef.current = true;
    backoff.current = 500;
    setIsListening(true);
    releaseRecognition(recognitionRef.current);
    launchRecognition(makeRecognition());
  }, [clearRestart, launchRecognition, makeRecognition, supported]);

  const stop = useCallback(() => {
    if (!listeningRef.current) return;
    stopRequested.current = true;
    listeningRef.current = false;
    clearRestart();
    setInterimText("");
    recognitionRef.current?.stop();
    // Let the browser deliver the final result before exposing the stopped state.
    if (!native) timer.current = window.setTimeout(() => setIsListening(false), 300);
  }, [clearRestart, native]);

  const cancel = useCallback(() => {
    listeningRef.current = false;
    stopRequested.current = true;
    clearRestart();
    const recognition = recognitionRef.current;
    recognitionRef.current = null;
    releaseRecognition(recognition);
    setIsListening(false);
    setInterimText("");
  }, [clearRestart]);

  const selectBackend = useCallback((value: SpeechBackend) => {
    cancel();
    setError("");
    saveSpeechBackend(value);
    setBackend(value);
  }, [cancel]);

  useEffect(() => cancel, [cancel]);

  return { isSupported: supported, isListening, finalText, interimText, transcript: finalText + interimText, setFinalText, start, stop, cancel,
    backend, selectBackend, hasNativeSpeech: hasNativeSpeech(), browserSupported, error };
}
