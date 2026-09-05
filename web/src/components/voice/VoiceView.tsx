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
  const [candidatePos, setCandidatePos] = useState<VoiceCandidatePos>({ left: 360, top: 60, above: false });
  const [elapsed, setElapsed] = useState(0);
  const [selecting, setSelecting] = useState(false);
  const [composing, setComposing] = useState(false);
  const floating = useFloating();
  const { notify } = useNotifications();
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const mirrorRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  const appliedFinalRef = useRef(0);
  const absorbedRef = useRef("");
  const prevInterimRef = useRef("");
  const lastSavedRef = useRef("");
  const frozenRef = useRef("");
  const lastSelRef = useRef<{ start: number; end: number } | null>(null);
  const searchTimerRef = useRef<number | undefined>(undefined);
  const lastSearchedRef = useRef("");

  const interim = recognition.interimText;
  // An interim still finding its words reads as provisional: it rides the
  // tail without leading whitespace, which the field's line start absorbs.
  // A leading blank of a fresh line is the line break's own: trim it only
  // there, never mid-line where it still separates words.
  const shadowTail = interim === "" || (text !== "" && !text.endsWith("\n")) ? interim : interim.trimStart();
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
  const liveValue = (() => {
    const shadowing = shadowTail !== "" && absorbedRef.current === "";
    return shadowing && !text.endsWith(shadowTail) ? text + shadowTail : text;
  })();
  // While the user drags a selection the field freezes: rewriting the value
  // mid-gesture aborts the drag in the browser, so live arrivals wait for
  // mouse-up. Appends land at the tail, where the released range still maps.
  const displayValue = selecting ? frozenRef.current : liveValue;
  // The mirror draws the very same string the field shows — frozen mid-drag
  // included: confirmed ink plus the provisional tail faint. Identical
  // content, font, and box means the highlight the user drags lands exactly
  // on the glyphs they see, and the caret never drifts from them.
  const mirrorTail = displayValue.length > text.length ? displayValue.slice(text.length) : "";
  const mirrorHead = displayValue.slice(0, displayValue.length - mirrorTail.length);

  useEffect(() => () => {
    window.clearTimeout(searchTimerRef.current);
  }, []);

  // A drag released outside the field never reaches its mouse-up: catch it on
  // the window so the freeze cannot stick and strand a stale view.
  useEffect(() => {
    const release = () => setSelecting(false);
    window.addEventListener("mouseup", release);
    return () => window.removeEventListener("mouseup", release);
  }, []);

  // Seconds since recording started, for the mic's side. Purely informative:
  // the restart loop runs unbounded, there is no cap to count down to.
  useEffect(() => {
    if (!recognition.isListening) {
      setElapsed(0);
      return;
    }
    setElapsed(0);
    const timer = window.setInterval(() => setElapsed((current) => current + 1), 1000);
    return () => window.clearInterval(timer);
  }, [recognition.isListening]);

  // Every finalized chunk closes its line, listening or not: a late final
  // landing after stop keeps the pause it carries instead of gluing onto the
  // previous line. Appends only, so edits made while dictating survive.
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
      const sep = base === "" || base.endsWith("\n") ? "" : "\n";
      return `${base}${sep}${delta.trimStart()}\n`;
    });
  }, [recognition.finalText, recognition.isListening]);

  // Keep the user's caret and selection across value updates: recognition
  // ticks rewrite the whole value, which would otherwise yank the caret to
  // the end and break a selection. Never fight an active drag or IME.
  useLayoutEffect(() => {
    const area = areaRef.current;
    const sel = lastSelRef.current;
    if (!area || !sel || selecting || composing) return;
    if (document.activeElement !== area) return;
    const length = area.value.length;
    const start = Math.min(sel.start, length);
    const end = Math.min(sel.end, length);
    if (area.selectionStart !== start || area.selectionEnd !== end) {
      area.setSelectionRange(start, end);
    }
  });

  function syncMirror() {
    const area = areaRef.current;
    const mirror = mirrorRef.current;
    if (area && mirror) mirror.scrollTop = area.scrollTop;
  }

  // Follow the tail while recognition appends, unless the user is working in
  // the field: a focused field never moves under them.
  useEffect(() => {
    const area = areaRef.current;
    if (!area || !followRef.current) return;
    if (document.activeElement === area) return;
    area.scrollTop = area.scrollHeight;
    syncMirror();
  }, [displayValue]);

  function handleScroll() {
    const area = areaRef.current;
    if (!area) return;
    followRef.current = area.scrollHeight - (area.scrollTop + area.clientHeight) < 24;
    syncMirror();
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

  // Measure the selection's end in the mirror, which lays the field's string
  // out glyph for glyph: mouse drags and keyboard ranges anchor the same
  // panel, with no cursor-vs-centre split.
  function measureSelection() {
    const mirror = mirrorRef.current;
    const area = areaRef.current;
    const wrap = wrapRef.current;
    if (!mirror || !area || !wrap) return null;
    const end = Math.min(area.selectionEnd, displayValue.length);
    let acc = 0;
    let node: Text | null = null;
    let offset = 0;
    const walker = document.createTreeWalker(mirror, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const current = walker.currentNode as Text;
      const next = acc + current.length;
      if (end <= next) {
        node = current;
        offset = end - acc;
        break;
      }
      acc = next;
    }
    if (!node) return null;
    const range = document.createRange();
    range.setStart(node, Math.min(offset, node.length));
    range.collapse(true);
    // Older DOMs resolve the selection without exposing geometry; keep the
    // previous panel spot rather than dropping a valid search.
    if (typeof range.getBoundingClientRect !== "function") return null;
    const caret = range.getBoundingClientRect();
    const rect = wrap.getBoundingClientRect();
    if (rect.width === 0) return null;
    return { x: caret.left - rect.left, y: caret.bottom - rect.top, height: rect.height, width: rect.width };
  }

  function refreshSelection() {
    const area = areaRef.current;
    if (area) {
      lastSelRef.current = { start: area.selectionStart, end: area.selectionEnd };
    }
    const term = selectedTerm();
    const measured = term === "" ? null : measureSelection();
    if (measured) {
      const half = 170;
      setCandidatePos({
        left: Math.min(Math.max(measured.x, Math.min(half, measured.width - half)), Math.max(half, measured.width - half)),
        top: Math.min(Math.max(measured.y, 8), Math.max(8, measured.height - 8)),
        above: measured.y > 200,
      });
    }
    scheduleSearch(term);
  }

  function handleChange(event: React.ChangeEvent<HTMLTextAreaElement>) {
    const value = event.target.value;
    if (shadowTail !== "" && value.endsWith(shadowTail)) {
      absorbedRef.current = "";
      setText(value.slice(0, value.length - shadowTail.length));
    } else {
      if (shadowTail !== "") absorbedRef.current = shadowTail;
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
          {recognition.isListening ? <span className="voice-elapsed" role="timer">{formatTime(elapsed)}</span> : null}
        </div>
      </div>
      <div className={`voice-transcript-wrap${composing ? " composing" : ""}`} ref={wrapRef}>
        <div className="voice-transcript-mirror" ref={mirrorRef} aria-hidden="true">{mirrorHead}{mirrorTail !== "" ? <span className="voice-interim-faint">{mirrorTail}</span> : null}{"\u200b"}</div>
        <textarea
          className="voice-transcript"
          ref={areaRef}
          aria-label="音声入力の文字起こし"
          value={displayValue}
          onChange={handleChange}
          onScroll={handleScroll}
          onMouseDown={() => {
            frozenRef.current = displayValue;
            setSelecting(true);
          }}
          onMouseUp={() => {
            setSelecting(false);
            refreshSelection();
          }}
          onSelect={refreshSelection}
          onKeyDown={(event) => {
            if (event.key === "Escape") clearSearch();
          }}
          onBlur={() => {
            // Leaving the field means the user is done pointing elsewhere on
            // purpose: hand the tail-follow back on for the next arrival.
            followRef.current = true;
            setSelecting(false);
          }}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
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

function formatTime(seconds: number) {
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
