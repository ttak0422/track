import { useEffect, useMemo, useState } from "react";
import { openJournal, resolveTerm, searchNotes } from "../../api";
import { useNoteQuery, useSaveNoteMutation } from "../../queries";
import { useFloating } from "../preview/floatingStore";
import { VoiceIcon } from "./VoiceIcon";
import { useSpeechRecognition } from "./useSpeechRecognition";
import "./voice.css";

export function VoiceView() {
  const recognition = useSpeechRecognition();
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [targetID, setTargetID] = useState("");
  const [linkTerm, setLinkTerm] = useState("");
  const [candidates, setCandidates] = useState<Array<{ note_id: string; title: string }>>([]);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const floating = useFloating();
  const note = useNoteQuery(targetID, { enabled: targetID !== "" });
  const save = useSaveNoteMutation(targetID);
  const liveText = recognition.transcript;

  useEffect(() => {
    if (liveText) setText(liveText);
  }, [liveText]);

  const chosenText = useMemo(() => linkTerm.trim(), [linkTerm]);

  function selectedTerm() {
    const area = document.querySelector<HTMLTextAreaElement>(".voice-transcript");
    const selected = area ? text.slice(area.selectionStart, area.selectionEnd).trim() : "";
    return selected || chosenText;
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
    if (!title.trim() || !text.trim()) return;
    setError("");
    setMessage("");
    let id = targetID;
    if (!id) {
      const resolved = await resolveTerm(title.trim());
      if (resolved.found) {
        id = resolved.note.note_id;
        setTargetID(id);
        setMessage("Opening existing note…");
        return;
      }
      // The current web API exposes journal creation, but no titled-note creation endpoint yet.
      const today = new Date();
      const date = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
      const journal = await openJournal(date);
      id = journal.note_id;
      setTargetID(id);
      setMessage("A titled-note endpoint is not available; preparing today’s journal.");
      return;
    }
    if (!note.data) {
      setMessage("Loading note…");
      return;
    }
    save.mutate({ body: text, etag: note.data.note.etag ?? "" }, {
      onSuccess: () => setMessage(`Saved to ${title.trim()}`),
      onError: (reason) => setError(reason instanceof Error ? reason.message : "Save failed"),
    });
  }

  return (
    <section className="voice-view" aria-labelledby="voice-title">
      <header className="voice-header">
        <h1 className="voice-title" id="voice-title">Voice input</h1>
        <span className="voice-label">Phase 1 · live transcription</span>
      </header>
      <div className="voice-field">
        <label htmlFor="voice-title-input">Save to note</label>
        <input className="voice-input" id="voice-title-input" value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Note title" />
      </div>
      <div className="voice-field">
        <label htmlFor="voice-transcript">Transcript</label>
        <textarea className="voice-transcript" id="voice-transcript" value={text} onChange={(event) => setText(event.target.value)} placeholder="Start the microphone, or type here…" />
      </div>
      <div className="voice-field">
        <label htmlFor="voice-link-term">Link term</label>
        <input className="voice-input" id="voice-link-term" value={linkTerm} onChange={(event) => setLinkTerm(event.target.value)} placeholder="Select text above, or enter a term" />
      </div>
      <div className="voice-actions">
        <button className="voice-action primary" type="button" disabled={!recognition.isSupported} onClick={recognition.isListening ? recognition.stop : recognition.start}>
          <VoiceIcon listening={recognition.isListening} /> {recognition.isListening ? "Stop microphone" : "Start microphone"}
        </button>
        <button className="voice-action" type="button" onClick={() => void findLink()}>Find link</button>
        <button className="voice-action primary" type="button" disabled={!title.trim() || !text.trim() || save.isPending} onClick={() => void saveTranscript()}>Save transcript</button>
        <span className="voice-status">{recognition.isSupported ? (recognition.isListening ? "Listening…" : "Ready") : "Speech recognition is not supported in this browser."}</span>
      </div>
      {candidates.length > 0 ? <div className="voice-candidates" aria-label="Link candidates">
        <span className="voice-label">Choose a note</span>
        {candidates.map((candidate) => <button className="voice-candidate" type="button" key={candidate.note_id} onClick={() => insertLink(candidate.note_id, candidate.title)}>{candidate.title}</button>)}
      </div> : null}
      {message ? <p className="voice-status" role="status">{message}</p> : null}
      {error ? <p className="voice-error" role="alert">{error}</p> : null}
    </section>
  );
}
