import { useEffect, useRef, useState } from "react";
import { openJournal, resolveTerm, searchNotes } from "../../api";
import { useNotifications } from "../../notifications";
import { useNoteQuery, useSaveNoteMutation } from "../../queries";
import { useFloating } from "../preview/floatingStore";
import { VoiceIcon } from "./VoiceIcon";
import { useSpeechRecognition } from "./useSpeechRecognition";
import "./voice.css";

interface VoiceSelection {
  term: string;
  left: number;
  top: number;
}

export function VoiceView() {
  const recognition = useSpeechRecognition();
  const [text, setText] = useState("");
  const [targetID, setTargetID] = useState("");
  const [candidates, setCandidates] = useState<Array<{ note_id: string; title: string }>>([]);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [selection, setSelection] = useState<VoiceSelection | null>(null);
  const floating = useFloating();
  const { notify } = useNotifications();
  const note = useNoteQuery(targetID, { enabled: targetID !== "" });
  const save = useSaveNoteMutation(targetID);
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const mirrorRef = useRef<HTMLDivElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  const mouseRef = useRef<{ x: number; y: number } | null>(null);

  useEffect(() => {
    if (recognition.finalText) setText(recognition.finalText);
  }, [recognition.finalText]);

  // Follow the tail while recognition appends: stay pinned to the newest text
  // unless the user has scrolled up to re-read, in which case leave them there.
  useEffect(() => {
    const area = areaRef.current;
    if (!area || !followRef.current) return;
    area.scrollTop = area.scrollHeight;
    syncMirror();
  }, [text, recognition.interimText]);

  function syncMirror() {
    const area = areaRef.current;
    const mirror = mirrorRef.current;
    if (area && mirror) mirror.scrollTop = area.scrollTop;
  }

  function handleScroll() {
    const area = areaRef.current;
    if (!area) return;
    followRef.current = area.scrollHeight - (area.scrollTop + area.clientHeight) < 24;
    syncMirror();
  }

  function handleMouseUp(event: React.MouseEvent) {
    mouseRef.current = { x: event.clientX, y: event.clientY };
  }

  function handleSelect() {
    const area = areaRef.current;
    const wrap = wrapRef.current;
    if (!area || !wrap) return;
    const term = text.slice(area.selectionStart, area.selectionEnd).trim();
    if (!term) {
      setSelection(null);
      return;
    }
    const rect = wrap.getBoundingClientRect();
    const mouse = mouseRef.current;
    const inside =
      mouse &&
      mouse.x >= rect.left &&
      mouse.x <= rect.right &&
      mouse.y >= rect.top &&
      mouse.y <= rect.bottom;
    const left = inside ? mouse.x - rect.left : rect.width / 2;
    const top = inside ? mouse.y - rect.top : 40;
    setSelection({
      term,
      left: Math.min(Math.max(left, 84), Math.max(84, rect.width - 84)),
      top: Math.max(top, 8),
    });
  }

  async function findLink(term: string) {
    if (!term) return;
    setError("");
    setNotice("");
    setCandidates([]);
    setSelection(null);
    const resolved = await resolveTerm(term);
    if (resolved.found) {
      setCandidates([{ note_id: resolved.note.note_id, title: resolved.note.title }]);
      return;
    }
    const result = await searchNotes(term, 8);
    setCandidates(result.results.map((item) => ({ note_id: item.note_id, title: item.title })));
    if (result.results.length === 0) setNotice("No matching note");
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

  async function saveTranscript() {
    if (!text.trim()) return;
    setError("");
    setNotice("");
    let id = targetID;
    if (!id) {
      const today = new Date();
      const date = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
      const journal = await openJournal(date);
      id = journal.note_id;
      setTargetID(id);
      notify("Today’s journal is ready — save again to append the transcript.", id);
      return;
    }
    if (!note.data) {
      setNotice("Loading note…");
      return;
    }
    save.mutate({ body: text, etag: note.data.note.etag ?? "" }, {
      onSuccess: () => notify("Saved to today’s journal", id),
      onError: (reason) => setError(reason instanceof Error ? reason.message : "Save failed"),
    });
  }

  return (
    <section className="voice-view" aria-labelledby="voice-title">
      <header className="voice-header">
        <h1 className="voice-title" id="voice-title">Voice input</h1>
      </header>
      <div className={`voice-console${recognition.isListening ? " listening" : ""}`} aria-label="音声入力">
        <div className="voice-indicator"><span className="voice-dot" aria-hidden="true" /><span role="status">{recognition.isListening ? "音声入力中" : "停止中"}</span></div>
        <div className="voice-actions">
          <button className="voice-action primary" type="button" disabled={!recognition.isSupported} onClick={recognition.isListening ? recognition.stop : recognition.start}>
            <VoiceIcon listening={recognition.isListening} /> {recognition.isListening ? "音声入力を停止" : "音声入力を開始"}
          </button>
          <button className="voice-action" type="button" onClick={() => void saveTranscript()} disabled={!text.trim() || save.isPending}>今日のjournalへ保存</button>
        </div>
      </div>
      <div className="voice-transcript-wrap" ref={wrapRef} onMouseUp={handleMouseUp}>
        <div className="voice-transcript-mirror" ref={mirrorRef} aria-hidden="true">{text}{recognition.interimText ? <span className="voice-interim-inline">{text && !text.endsWith("\n") ? " " : ""}{recognition.interimText}</span> : null}{"\u200b"}</div>
        <textarea
          className="voice-transcript"
          ref={areaRef}
          aria-label="音声入力の文字起こし"
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setSelection(null);
          }}
          onScroll={handleScroll}
          onSelect={handleSelect}
          placeholder="音声入力を開始してください…"
        />
        {selection ? (
          <div className="voice-select-pop" style={{ left: selection.left, top: selection.top }}>
            <button
              type="button"
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => void findLink(selection.term)}
            >
              「{selection.term.length > 12 ? `${selection.term.slice(0, 12)}…` : selection.term}」を検索
            </button>
          </div>
        ) : null}
      </div>
      {candidates.length > 0 ? <div className="voice-candidates" aria-label="Link candidates">
        <span className="voice-candidates-title">Choose a note</span>
        {candidates.map((candidate) => <button className="voice-candidate" type="button" key={candidate.note_id} onClick={() => openLink(candidate.note_id)}>{candidate.title}</button>)}
      </div> : null}
      {notice ? <p className="voice-status" role="status">{notice}</p> : null}
      {error ? <p className="voice-error" role="alert">{error}</p> : null}
    </section>
  );
}
