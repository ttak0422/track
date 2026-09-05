import { useEffect, useState, type CSSProperties } from "react";
import { openJournal, resolveTerm, searchNotes } from "../../api";
import { useNoteQuery, useSaveNoteMutation } from "../../queries";
import { useFloating } from "../preview/floatingStore";
import { VoiceIcon } from "./VoiceIcon";
import { useSpeechRecognition } from "./useSpeechRecognition";
import "./voice.css";

export function VoiceView() {
  const recognition = useSpeechRecognition();
  const [text, setText] = useState("");
  const [targetID, setTargetID] = useState("");
  const [candidates, setCandidates] = useState<Array<{ note_id: string; title: string }>>([]);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const floating = useFloating();
  const note = useNoteQuery(targetID, { enabled: targetID !== "" });
  const save = useSaveNoteMutation(targetID);
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!recognition.isListening) return;
    const timer = window.setInterval(() => setElapsed((current) => Math.min(current + 1, 3600)), 1000);
    return () => window.clearInterval(timer);
  }, [recognition.isListening]);

  useEffect(() => {
    if (recognition.isListening && elapsed >= 3600) recognition.stop();
  }, [elapsed, recognition.isListening, recognition.stop]);

  useEffect(() => {
    if (recognition.finalText) setText(recognition.finalText);
  }, [recognition.finalText]);

  function selectedTerm() {
    const area = document.querySelector<HTMLTextAreaElement>(".voice-transcript");
    const selected = area ? text.slice(area.selectionStart, area.selectionEnd).trim() : "";
    return selected;
  }

  async function findLink() {
    const term = selectedTerm();
    if (!term) return;
    setError("");
    setCandidates([]);
    const resolved = await resolveTerm(term);
    if (resolved.found) {
      setCandidates([{ note_id: resolved.note.note_id, title: resolved.note.title }]);
      return;
    }
    const result = await searchNotes(term, 8);
    setCandidates(result.results.map((item) => ({ note_id: item.note_id, title: item.title })));
    if (result.results.length === 0) setMessage("No matching note");
  }

  function insertLink(noteID: string, noteTitle: string) {
    const area = document.querySelector<HTMLTextAreaElement>(".voice-transcript");
    const term = selectedTerm();
    const replacement = term === noteTitle ? `[[${noteTitle}]]` : `[[${noteTitle}|${term}]]`;
    const start = area?.selectionStart ?? text.indexOf(term);
    const end = area?.selectionEnd ?? (start < 0 ? start : start + term.length);
    setText(start >= 0 ? text.slice(0, start) + replacement + text.slice(end) : `${text} ${replacement}`);
    setCandidates([]);
    floating.open(
      { kind: "note", noteID },
      { left: 72, top: 112, width: 420, height: 320 },
      false,
      { pinned: true },
    );
  }

  async function saveTranscript() {
    if (!text.trim()) return;
    setError("");
    setMessage("");
    let id = targetID;
    if (!id) {
      const today = new Date();
      const date = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
      const journal = await openJournal(date);
      id = journal.note_id;
      setTargetID(id);
      setMessage("Today’s journal is ready; save again to append the transcript.");
      return;
    }
    if (!note.data) {
      setMessage("Loading note…");
      return;
    }
    save.mutate({ body: text, etag: note.data.note.etag ?? "" }, {
      onSuccess: () => setMessage("Saved to today’s journal"),
      onError: (reason) => setError(reason instanceof Error ? reason.message : "Save failed"),
    });
  }

  return (
    <section className="voice-view" aria-labelledby="voice-title">
      <header className="voice-header">
        <h1 className="voice-title" id="voice-title">Voice input</h1>
        <span className="voice-label">Phase 1 · today’s journal</span>
      </header>
      <div className={`voice-console${recognition.isListening ? " listening" : ""}`} aria-label="音声入力">
        <div className="voice-wave" aria-hidden="true">
          {Array.from({ length: 30 }, (_, index) => <i key={index} style={{ "--height": `${6 + ((index * 17) % 58)}px`, "--delay": `${index * -0.17}s` } as CSSProperties} />)}
        </div>
        <div className="voice-indicator"><span className="voice-dot" aria-hidden="true" /><span role="status">{recognition.isListening ? "音声入力中" : "停止中"}</span><span className="voice-timer">{formatTime(elapsed)} / 60:00</span></div>
        <div className="voice-actions">
          <button className="voice-action primary" type="button" disabled={!recognition.isSupported} onClick={recognition.isListening ? recognition.stop : recognition.start}>
            <VoiceIcon listening={recognition.isListening} /> {recognition.isListening ? "音声入力を停止" : "音声入力を開始"}
          </button>
          <button className="voice-action" type="button" onClick={() => void saveTranscript()} disabled={!text.trim() || save.isPending}>今日のjournalへ保存</button>
        </div>
      </div>
      <div className="voice-transcript-head"><label htmlFor="voice-transcript">文字起こし</label><button className="voice-text-action" type="button" onClick={() => void findLink()}>選択範囲をリンク</button></div>
      <textarea className="voice-transcript" id="voice-transcript" value={text} onChange={(event) => setText(event.target.value)} placeholder="音声入力を開始してください…" />
      {recognition.interimText ? <p className="voice-interim" aria-live="polite">認識中… {recognition.interimText}</p> : null}
      {candidates.length > 0 ? <div className="voice-candidates" aria-label="Link candidates">
        <span className="voice-label">Choose a note</span>
        {candidates.map((candidate) => <button className="voice-candidate" type="button" key={candidate.note_id} onClick={() => insertLink(candidate.note_id, candidate.title)}>{candidate.title}</button>)}
      </div> : null}
      {message ? <p className="voice-status" role="status">{message}</p> : null}
      {error ? <p className="voice-error" role="alert">{error}</p> : null}
    </section>
  );
}

function formatTime(seconds: number) {
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
