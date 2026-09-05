import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { getNote, openJournal, resolveTerm, saveNote, searchNotes } from "../../api";
import { useNotifications } from "../../notifications";
import { useFloating } from "../preview/floatingStore";
import { VoiceIcon } from "./VoiceIcon";
import { useSpeechRecognition } from "./useSpeechRecognition";
import "./voice.css";

const SEARCH_DEBOUNCE_MS = 500;

interface VoiceCandidatePos {
  left: number;
  top: number;
  above: boolean;
}

export function VoiceView() {
  const recognition = useSpeechRecognition();
  const [text, setText] = useState("");
  const [candidates, setCandidates] = useState<Array<{ note_id: string; title: string }>>([]);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [candidatePos, setCandidatePos] = useState<VoiceCandidatePos>({ left: 280, top: 60, above: false });
  const floating = useFloating();
  const { notify } = useNotifications();
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  const mouseDownRef = useRef(false);
  const composingRef = useRef(false);
  const appliedFinalRef = useRef(0);
  const absorbedRef = useRef("");
  const prevInterimRef = useRef("");
  const lastSavedRef = useRef("");
  const lastSelRef = useRef<{ start: number; end: number } | null>(null);
  const searchTimerRef = useRef<number | undefined>(undefined);
  const lastSearchedRef = useRef("");

  const interim = recognition.interimText;
  // Track the interim across renders: a growing interim is the same utterance
  // (a touched tail stays the user's), a wholly new one resumes shadowing.
  // An interim going quiet keeps the absorbed tail: the pending finalize
  // still needs it to tell hand-confirmed speech from a fresh delta.
  if (prevInterimRef.current !== interim) {
    prevInterimRef.current = interim;
    if (absorbedRef.current !== "" && interim !== "" && !interim.startsWith(absorbedRef.current)) {
      absorbedRef.current = "";
    }
  }
  // The interim rides on the tail of the editable value so it reads in place,
  // in the same field. Once the user touches the tail it is theirs: stop
  // shadowing it and let the next interim start fresh.
  const shadowing = interim !== "" && absorbedRef.current === "";
  const displayValue = shadowing && !text.endsWith(interim) ? text + interim : text;

  useEffect(() => () => {
    window.clearTimeout(searchTimerRef.current);
  }, []);

  // Append only the not-yet-applied tail of the recognized finals, so edits
  // made while dictating are never overwritten. A chunk boundary means the
  // speaker paused, so it starts on a fresh line while listening. A tail the
  // user already absorbed is the same speech confirmed by hand: strip it
  // before appending so it is not duplicated.
  useEffect(() => {
    const full = recognition.finalText;
    if (full.length < appliedFinalRef.current) appliedFinalRef.current = 0;
    if (full.length <= appliedFinalRef.current) return;
    const delta = full.slice(appliedFinalRef.current);
    appliedFinalRef.current = full.length;
    setText((prev) => {
      if (prev.endsWith(delta)) {
        absorbedRef.current = "";
        return prev;
      }
      let base = prev;
      const absorbed = absorbedRef.current;
      if (absorbed !== "" && base.endsWith(absorbed)) base = base.slice(0, base.length - absorbed.length);
      absorbedRef.current = "";
      // The user confirmed the same speech by hand while it was interim:
      // it already reads in the text, so appending would double it. Untouched
      // flows never take this branch, so repeated dictated words still append.
      const core = delta.trim();
      if (absorbed !== "" && core !== "" && base.includes(core)) return base;
      const breakLine = recognition.isListening && base !== "" && !base.endsWith("\n");
      return base + (breakLine ? "\n" : "") + (breakLine ? delta.trimStart() : delta);
    });
  }, [recognition.finalText, recognition.isListening]);

  // Keep the user's caret and selection across value updates: recognition
  // ticks rewrite the whole value, which would otherwise yank the caret to
  // the end and break a selection. Never fight an active drag or IME.
  useLayoutEffect(() => {
    const area = areaRef.current;
    const sel = lastSelRef.current;
    if (!area || !sel || mouseDownRef.current || composingRef.current) return;
    if (document.activeElement !== area) return;
    const length = area.value.length;
    const start = Math.min(sel.start, length);
    const end = Math.min(sel.end, length);
    if (area.selectionStart !== start || area.selectionEnd !== end) {
      area.setSelectionRange(start, end);
    }
  });

  // Follow the tail while recognition appends, unless the user is working in
  // the field: a focused field never moves under them.
  useEffect(() => {
    const area = areaRef.current;
    if (!area || !followRef.current) return;
    if (document.activeElement === area) return;
    area.scrollTop = area.scrollHeight;
  }, [displayValue]);

  function handleScroll() {
    const area = areaRef.current;
    if (!area) return;
    followRef.current = area.scrollHeight - (area.scrollTop + area.clientHeight) < 24;
  }

  function selectedTerm() {
    const area = areaRef.current;
    if (!area) return "";
    return displayValue.slice(area.selectionStart, area.selectionEnd).trim();
  }

  function clearSearch() {
    window.clearTimeout(searchTimerRef.current);
    lastSearchedRef.current = "";
    setCandidates([]);
    setNotice("");
  }

  // A selection searches by itself after a beat: no tap on an action first.
  // The beat absorbs drags and cursor passes; an unchanged term never
  // re-searches, so caret restores stay quiet.
  function scheduleSearch(term: string) {
    window.clearTimeout(searchTimerRef.current);
    if (!term) {
      clearSearch();
      return;
    }
    if (term === lastSearchedRef.current) return;
    searchTimerRef.current = window.setTimeout(() => {
      void runSearch(term);
    }, SEARCH_DEBOUNCE_MS);
  }

  async function runSearch(term: string) {
    if (selectedTerm() !== term) return;
    lastSearchedRef.current = term;
    setError("");
    setNotice("");
    setCandidates([]);
    const resolved = await resolveTerm(term);
    if (selectedTerm() !== term) return;
    if (resolved.found) {
      setCandidates([{ note_id: resolved.note.note_id, title: resolved.note.title }]);
      return;
    }
    const result = await searchNotes(term, 8);
    if (selectedTerm() !== term) return;
    setCandidates(result.results.map((item) => ({ note_id: item.note_id, title: item.title })));
    if (result.results.length === 0) setNotice("No matching note");
  }

  function refreshSelection(clientX?: number, clientY?: number) {
    const area = areaRef.current;
    if (area) {
      lastSelRef.current = { start: area.selectionStart, end: area.selectionEnd };
    }
    const term = selectedTerm();
    // The candidates open as a floating panel at the selection, so they read
    // where the user is looking instead of below the field. Mid-drag passes
    // keep the panel still until the gesture lands.
    const rect = wrapRef.current?.getBoundingClientRect();
    if (rect && term !== "" && (clientX !== undefined || !mouseDownRef.current)) {
      const fallbackX = rect.width / 2;
      const fallbackY = 60;
      const x = clientX === undefined ? fallbackX : clientX - rect.left;
      const y = clientY === undefined ? fallbackY : clientY - rect.top;
      const half = 170;
      setCandidatePos({
        left: Math.min(Math.max(x, Math.min(half, rect.width - half)), Math.max(half, rect.width - half)),
        top: y,
        above: y > 110,
      });
    }
    scheduleSearch(term);
  }

  function handleChange(event: React.ChangeEvent<HTMLTextAreaElement>) {
    const value = event.target.value;
    if (interim !== "" && value.endsWith(interim)) {
      absorbedRef.current = "";
      setText(value.slice(0, value.length - interim.length));
    } else {
      if (interim !== "") absorbedRef.current = interim;
      setText(value);
    }
    lastSelRef.current = { start: event.target.selectionStart, end: event.target.selectionEnd };
    clearSearch();
  }

  // The transcript stays as dictated: a candidate only opens its note in the
  // surrounding floating layer, never rewrites the dictated words into a link.
  function openLink(noteID: string) {
    floating.open(
      { kind: "note", noteID },
      { left: 72, top: 112, width: 420, height: 320 },
      false,
      { pinned: true },
    );
    setCandidates([]);
  }

  // No save control: stopping with unsaved dictation appends it to today's
  // journal and says so in the toast. Only the unsaved tail goes, so a
  // stop–start loop never files the same words twice.
  async function stopAndSave(snapshot: string) {
    recognition.stop();
    const previous = lastSavedRef.current;
    const tail = snapshot.startsWith(previous) ? snapshot.slice(previous.length) : snapshot;
    if (!tail.trim()) return;
    setError("");
    try {
      const today = new Date();
      const date = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
      const journal = await openJournal(date);
      const current = await getNote(journal.note_id);
      const base = current.note.body.replace(/\n+$/, "");
      const next = base === "" ? tail : `${base}\n\n${tail}`;
      await saveNote(journal.note_id, { body: next, etag: current.note.etag ?? "" });
      lastSavedRef.current = snapshot;
      notify("Saved to today’s journal", journal.note_id);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Auto-save failed");
    }
  }

  return (
    <section className="voice-view" aria-label="音声入力">
      <div className={`voice-console${recognition.isListening ? " listening" : ""}`} aria-label="音声入力">
        <div className="voice-actions">
          <button
            className="voice-mic"
            type="button"
            disabled={!recognition.isSupported}
            aria-pressed={recognition.isListening}
            aria-label={recognition.isListening ? "音声入力を停止" : "音声入力を開始"}
            onClick={recognition.isListening ? () => void stopAndSave(text) : recognition.start}
          >
            <VoiceIcon />
          </button>
        </div>
        <div className="voice-indicator"><span className="voice-dot" aria-hidden="true" /><span role="status">{recognition.isListening ? "Recording" : "Idle"}</span></div>
      </div>
      <div className="voice-transcript-wrap" ref={wrapRef}>
        <textarea
          className="voice-transcript"
          ref={areaRef}
          aria-label="音声入力の文字起こし"
          value={displayValue}
          onChange={handleChange}
          onScroll={handleScroll}
          onMouseDown={() => {
            mouseDownRef.current = true;
          }}
          onMouseUp={(event) => {
            mouseDownRef.current = false;
            refreshSelection(event.clientX, event.clientY);
          }}
          onSelect={() => refreshSelection()}
          onKeyDown={(event) => {
            if (event.key === "Escape") clearSearch();
          }}
          onCompositionStart={() => {
            composingRef.current = true;
          }}
          onCompositionEnd={() => {
            composingRef.current = false;
          }}
          placeholder="音声入力を開始してください…"
        />
        {candidates.length > 0 ? <div
          className={`voice-candidates${candidatePos.above ? " above" : ""}`}
          aria-label="Link candidates"
          style={{ left: candidatePos.left, top: candidatePos.top }}
        >
          <span className="voice-candidates-title">Choose a note</span>
          {candidates.map((candidate) => <button className="voice-candidate" type="button" key={candidate.note_id} onMouseDown={(event) => event.preventDefault()} onClick={() => openLink(candidate.note_id)}>{candidate.title}</button>)}
        </div> : null}
      </div>
      {notice ? <p className="voice-status" role="status">{notice}</p> : null}
      {error ? <p className="voice-error" role="alert">{error}</p> : null}
    </section>
  );
}
