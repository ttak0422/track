import AVFoundation
import AppKit
import Foundation
import Observation
import Speech
import SwiftUI
import TrackAPI

// Voice input for the native workspace: dictation through SFSpeechRecognizer
// (ja-JP) fed live audio by AVAudioEngine. VoiceInputModel owns the session —
// permission, the mic tap, and the recognizer task — and publishes the
// transcript (interim and final), the recording state, and any error.
//
// Transcript merging and journal write checkpoints are shared with the
// microphone-free regression check in VoiceWorkflow.swift.

// MARK: - Model

@MainActor
@Observable
public final class VoiceInputModel {
    private var transcriptState = VoiceTranscriptState()
    public var transcript: String { transcriptState.text }
    public var interimTranscript: String { transcriptState.interim }
    public private(set) var isStopping = false
    public private(set) var isStarting = false
    private var stopWaiter: CheckedContinuation<Void, Never>?
    private var stopTimeout: Task<Void, Never>?
    private var recognitionID = UUID()
    private var isInteracting = false
    private var queuedSpeech: [(text: String, final: Bool)] = []
    /// True while the engine is recording and feeding the recognizer.
    public private(set) var isRecording = false
    /// Last failure surfaced to the view (permission, no mic, recognizer
    /// error); nil while a session is healthy.
    public private(set) var error: String?

    /// The ja-JP recognizer, or nil when the locale is unsupported.
    private let recognizer: SFSpeechRecognizer?
    private let audioEngine = AVAudioEngine()
    /// The recognition request fed from the mic tap. The tap closure runs on
    /// an audio thread while start()/stop() run on the main actor; the SDK's
    /// Speech/AVFoundation APIs are non-Sendable, so the compiler does not
    /// cross-check that access (append is thread-safe by design).
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    /// Bumped per session so a finished session never tears down a newer one
    /// that started before its final result arrived.
    private var sessionID = 0
    private var retryCount = 0
    private var restartTask: Task<Void, Never>?

    public init() {
        recognizer = SFSpeechRecognizer(locale: Locale(identifier: "ja-JP"))
    }

    /// Starts a recording session: requests permission if needed, then wires
    /// the mic tap to a fresh recognition task. Failures land in `error`.
    public func start() async {
        guard !isRecording, !isStarting, !isStopping else { return }
        isStarting = true
        defer { isStarting = false }
        error = nil
        sessionID += 1
        let session = sessionID
        retryCount = 0
        restartTask?.cancel()
        restartTask = nil

        guard let recognizer else {
            error = "音声認識はこの機種では利用できません (ja-JP)。"
            return
        }
        guard recognizer.isAvailable else {
            error = "音声認識サーバーに接続できません。ネットワークを確認してください。"
            return
        }

        let status = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
        }
        guard status == .authorized else {
            error = "音声認識の権限がありません。システム設定 → プライバシーとセキュリティ → 音声認識で許可してください。"
            return
        }

        guard sessionID == session else { return }
        guard await AVCaptureDevice.requestAccess(for: .audio) else {
            error = "マイクの権限がありません。システム設定のマイク設定で許可してください。"
            return
        }
        guard sessionID == session else { return }

        let node = audioEngine.inputNode
        let format = node.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            error = "利用できるマイクがありません。入力デバイスを確認してください。"
            return
        }
        node.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            self?.request?.append(buffer)
        }

        audioEngine.prepare()
        do {
            try audioEngine.start()
        } catch let engineError {
            audioEngine.stop()
            node.removeTap(onBus: 0)
            self.request = nil
            self.error = "マイクを起動できませんでした: \(engineError.localizedDescription)"
            return
        }

        isRecording = true
        startRecognitionTask(with: recognizer, session: session)
    }

    /// Speech recognition tasks have a server-side lifetime. Keep the audio
    /// engine alive and replace only the request/task whenever a segment is
    /// finalized or the service reports a transient network failure.
    private func startRecognitionTask(with recognizer: SFSpeechRecognizer, session: Int) {
        guard isRecording, sessionID == session else { return }
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        self.request = request
        recognitionID = UUID()
        let recognition = recognitionID
        task = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor in
                guard let self, self.sessionID == session, self.recognitionID == recognition else { return }
                if let result {
                    self.acceptSpeech(result.bestTranscription.formattedString, isFinal: result.isFinal)
                    if result.isFinal {
                        self.recognitionID = UUID()
                        self.request = nil
                        self.task = nil
                        if self.isStopping { self.finishStop() }
                        else { self.scheduleRecognitionRestart(session: session) }
                    }
                } else {
                    // An error right after stop() is normal ("no speech
                    // detected"); keep the transcript and only surface the
                    // failure while recording was still active.
                    if self.isStopping { self.finishStop(); return }
                    guard self.isRecording else { return }
                    self.recognitionID = UUID()
                    self.transcriptState.finishSegment()
                    self.request = nil
                    self.task = nil
                    self.scheduleRecognitionRestart(session: session, failure: error)
                }
            }
        }
    }

    private func scheduleRecognitionRestart(session: Int, failure: Error? = nil) {
        guard isRecording, sessionID == session, restartTask == nil else { return }
        retryCount = failure == nil ? 0 : retryCount + 1
        if retryCount > 5 {
            error = failure?.localizedDescription ?? "音声認識を再開できませんでした。"
            isRecording = false
            audioEngine.stop()
            audioEngine.inputNode.removeTap(onBus: 0)
            return
        }
        let delay = failure == nil ? 150 : min(2_000, 250 * (1 << min(retryCount - 1, 3)))
        restartTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(delay))
            guard !Task.isCancelled else { return }
            await MainActor.run {
                guard let self, self.sessionID == session, self.isRecording else { return }
                self.restartTask = nil
                if let recognizer = self.recognizer {
                    self.startRecognitionTask(with: recognizer, session: session)
                }
            }
        }
    }

    /// Wait for the recognizer's final delivery, with the visible interim as a
    /// bounded fallback. Starting again is disabled until this snapshot settles.
    public func stop() async {
        if isStarting { sessionID += 1; return }
        guard isRecording else { return }
        isRecording = false
        isStopping = true
        audioEngine.stop()
        audioEngine.inputNode.removeTap(onBus: 0)
        restartTask?.cancel()
        restartTask = nil
        await withCheckedContinuation { continuation in
            stopWaiter = continuation
            guard request != nil else { finishStop(); return }
            request?.endAudio()
            stopTimeout = Task { [weak self] in
                try? await Task.sleep(for: .seconds(2))
                guard !Task.isCancelled else { return }
                self?.finishStop()
            }
        }
    }

    private func finishStop() {
        guard isStopping else { return }
        // A final/error/timeout can arrive only once for this stopped segment.
        flushSpeech()
        transcriptState.finishSegment()
        isStopping = false
        recognitionID = UUID()
        task?.cancel()
        task = nil
        request = nil
        stopTimeout?.cancel()
        stopTimeout = nil
        stopWaiter?.resume()
        stopWaiter = nil
    }

    public func setInteracting(_ active: Bool) {
        isInteracting = active
        if !active { flushSpeech() }
    }
    private func acceptSpeech(_ text: String, isFinal: Bool) {
        if isInteracting {
            if queuedSpeech.last?.final == false { queuedSpeech.removeLast() }
            queuedSpeech.append((text, isFinal))
        } else { transcriptState.receive(text, isFinal: isFinal) }
    }
    private func flushSpeech() {
        for event in queuedSpeech { transcriptState.receive(event.text, isFinal: event.final) }
        queuedSpeech = []
    }
    public func clear() { transcriptState.clear() }
    public func updateTranscript(_ value: String) { transcriptState.edit(value) }
}

// MARK: - View

/// The record-and-transcript surface. The record button toggles the session
/// and the transcript (live while speaking, final after stop) is shown in an
/// editable text area; the error line surfaces permission/mic/recognizer issues.
/// "Append to today's journal" opens (or creates) today's journal and writes
/// the transcript onto the end of its body.
public struct VoiceView: View {
    @Environment(VaultScope.self) private var vaultScope: VaultScope?
    @State private var model = VoiceInputModel()

    private let client: TrackClient
    @State private var writer: VoiceJournalWriter
    let onOpenNote: (TrackID) -> Void
    @State private var selectedText = ""
    @State private var searchGeneration = UUID()
    @State private var pendingSearchTerm = ""
    @State private var clearedCheckpoint = ""
    @Environment(\.colorScheme) private var colorScheme
    @State private var searchResults: [SearchResult] = []
    @State private var searchError: String?
    @State private var isSearching = false
    @State private var copied = false
    @State private var recordingStart: Date?
    @State private var isCreatingNote = false
    /// Transcript stashed by Clear so the destructive action can be undone
    /// (web VoiceView's undoable clearTranscript).
    @State private var clearedTranscript: String?
    /// Exact-match guard: when the selected text names an existing note,
    /// creation stays visible but disabled instead of
    /// 409ing into an error.
    @State private var createTaken = false
    /// Debounced auto-search (web VoiceView's selection auto-search): the
    /// pending task plus the selected query, so unchanged selections stay
    /// quiet and an insertion point clears the results.
    @State private var autoSearchTask: Task<Void, Never>?
    @State private var lastAutoSearchQuery = ""

    public init(client: TrackClient, onOpenNote: @escaping (TrackID) -> Void = { _ in }) {
        self.client = client
        self.onOpenNote = onOpenNote
        _writer = State(initialValue: VoiceJournalWriter(client: client))
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                Button {
                    if model.isRecording {
                        stopAndSave()
                    } else {
                        recordingStart = Date()
                        Task {
                            await model.start()
                            if !model.isRecording { recordingStart = nil }
                        }
                    }
                } label: {
                    Label(
                        model.isRecording ? "Stop" : "Record",
                        systemImage: model.isRecording ? "stop.fill" : "mic.fill"
                    )
                    .frame(minWidth: 96)
                }
                .buttonStyle(.borderedProminent)
                .tint(model.isRecording ? .red : .accentColor)
                .disabled(model.isStarting || model.isStopping || writer.isSaving)
                .help(model.isRecording ? "Stop recording" : "Start recording")

                if model.isRecording {
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        Text(Self.elapsedString(since: recordingStartedAt ?? context.date))
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("Recording time")
                    }
                }

                Button {
                    copyAll()
                } label: {
                    Label(copied ? "Copied" : "Copy all", systemImage: "doc.on.doc")
                }
                .buttonStyle(.bordered)
                .disabled(model.transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)

                Button {
                    clearTranscript()
                } label: {
                    Label("Clear", systemImage: "trash")
                }
                .buttonStyle(.bordered)
                .disabled(model.transcript.isEmpty || writer.isSaving || writer.hasPendingSave)

                if let cleared = clearedTranscript, !cleared.isEmpty {
                    Button {
                        model.updateTranscript(cleared)
                        writer.restoreCheckpoint(clearedCheckpoint)
                        clearedTranscript = nil
                        searchError = nil
                    } label: {
                        Label("Undo clear", systemImage: "arrow.uturn.backward")
                    }
                    .buttonStyle(.bordered)
                    .help("Restore the cleared transcript")
                }

                Button {
                    appendToJournal()
                } label: {
                    Label(
                        writer.isSaving ? "Saving…" : "Save unsaved text",
                        systemImage: "square.and.pencil"
                    )
                }
                .buttonStyle(.bordered)
                .disabled(model.transcript.isEmpty || writer.isSaving || model.isRecording || model.isStopping)
                .help("Append only the unsaved transcript to today’s journal")

                Button {
                    searchTranscript(query: selectedText)
                } label: {
                    Label(
                        isSearching ? "Searching…" : "Search notes",
                        systemImage: "magnifyingglass"
                    )
                }
                .buttonStyle(.bordered)
                .disabled(selectedText.isEmpty || isSearching)
                .help("Search notes with selected text")

                Spacer()
            }

            if let error = model.error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            if let appendError = writer.error {
                Text(appendError)
                    .font(.caption)
                    .foregroundStyle(.red)
            } else if let appendNote = writer.message {
                Text(appendNote)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if let journalID = writer.journalID {
                Button("Open journal") { onOpenNote(journalID) }
            }

            if let searchError {
                Text(searchError)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            ZStack(alignment: .topLeading) {
                VoiceTranscriptEditor(
                    text: model.transcript,
                    onEdit: { model.updateTranscript($0) },
                    onSelection: { selectedText = $0; scheduleAutoSearch($0) },
                    onInteraction: { model.setInteracting($0) }
                )
                .font(.body)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(4)
                .overlay {
                    RoundedRectangle(cornerRadius: 6)
                        .strokeBorder(.quaternary, lineWidth: 1)
                }

                if model.transcript.isEmpty {
                    Text("Press Record and speak (ja-JP). You can edit the transcript here.")
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 12)
                        .allowsHitTesting(false)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)

            if !lastAutoSearchQuery.isEmpty {
                ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Search results")
                        .font(.headline)

                    ForEach(Array(resultSections.enumerated()), id: \.offset) { _, section in
                        if !section.results.isEmpty {
                            Text(section.title).font(.subheadline.weight(.semibold))
                            ForEach(section.results, id: \.qualifiedID) { result in
                                Button {
                                    onOpenNote(result.qualifiedID)
                                    clearSearch()
                                } label: {
                                    VStack(alignment: .leading, spacing: 3) {
                                        HStack(spacing: 6) {
                                            highlighted(result.ref.title).font(.body.weight(.medium))
                                            Image(systemName: "arrow.up.right")
                                                .font(.caption2).foregroundStyle(.tertiary)
                                        }
                                        if let snippet = result.snippet, !snippet.isEmpty {
                                            highlighted(snippet).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                                        }
                                    }
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.vertical, 4)
                                }
                                .buttonStyle(.plain)
                                .contentShape(Rectangle())
                            }
                        }
                    }
                    if searchResults.isEmpty { Text(isSearching ? "Searching…" : "No matching note").foregroundStyle(.secondary) }
                    Button {
                        createNoteFromTranscript()
                    } label: {
                        Label(
                            isCreatingNote ? "Creating…" : createTaken ? "\"\(displayCreateTitle)\" already exists" : "Create \"\(lastAutoSearchQuery)\"",
                            systemImage: "plus"
                        )
                    }
                    .buttonStyle(.bordered)
                    .disabled(createTaken || isCreatingNote || lastAutoSearchQuery.isEmpty || isSearching)
                }
                .padding(.top, 4)
                }.frame(maxHeight: 240)
            }
        }
        .padding(16)
        .onDisappear {
            autoSearchTask?.cancel()
            if model.isRecording || model.isStarting { stopAndSave() }
        }
    }

    private var recordingStartedAt: Date? { model.isRecording ? (recordingStart ?? Date()) : nil }
    private var displayCreateTitle: String {
        let title = lastAutoSearchQuery
        let short = String(title.prefix(12))
        return title.count > 12 ? "\(short)…" : short
    }
    private var resultSections: [(title: String, results: [SearchResult])] {
        [
            ("Titles", searchResults.filter { $0.match != "body" && $0.match != "path" }),
            ("Full text", searchResults.filter { $0.match == "body" }),
            ("File name", searchResults.filter { $0.match == "path" })
        ]
    }

    private func copyAll() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(model.transcript, forType: .string)
        copied = true
        Task {
            try? await Task.sleep(for: .milliseconds(1200))
            copied = false
        }
    }

    private func clearSearch() {
        autoSearchTask?.cancel()
        autoSearchTask = nil
        searchGeneration = UUID()
        pendingSearchTerm = ""
        lastAutoSearchQuery = ""
        searchResults = []
        searchError = nil
        createTaken = false
        isSearching = false
    }

    private func clearTranscript() {
        clearedTranscript = model.transcript
        clearedCheckpoint = writer.savedTranscript
        writer.reset()
        model.clear()
        selectedText = ""
        clearSearch()
    }

    private func highlighted(_ text: String) -> Text {
        var value = AttributedString(text)
        for match in SearchPresentation.highlightRanges(in: text, query: lastAutoSearchQuery) {
            guard let source = Range(match, in: text), let range = Range(source, in: value) else { continue }
            value[range].backgroundColor = TrackTheme.palette(for: colorScheme).panelSoft
            value[range].inlinePresentationIntent = .stronglyEmphasized
        }
        return Text(value)
    }

    private static func elapsedString(since date: Date) -> String {
        let seconds = max(0, Int(Date().timeIntervalSince(date)))
        return String(format: "%02d:%02d", seconds / 60, seconds % 60)
    }

    /// Like Web: a selection searches; a caret or empty selection clears it.
    private func searchTranscript(query: String) {
        guard !query.isEmpty, query == selectedText else { return }
        let generation = UUID()
        searchGeneration = generation
        lastAutoSearchQuery = query
        isSearching = true
        searchError = nil
        searchResults = []
        createTaken = false
        let vault = vaultScope?.scope ?? ""
        Task {
            defer { if generation == searchGeneration { isSearching = false } }
            do {
                let resolved = try await client.resolveTerm(query, vault: vault)
                guard generation == searchGeneration, query == selectedText else { return }
                if resolved.found, let exact = Self.searchResult(for: resolved.note) {
                    searchResults = [exact]
                    createTaken = true
                    return
                }
                let response = try await client.searchNotes(query: query, limit: 8)
                guard generation == searchGeneration, query == selectedText else { return }
                searchResults = response.results
            } catch {
                guard generation == searchGeneration, query == selectedText else { return }
                searchError = (error as? APIError)?.message ?? error.localizedDescription
            }
        }
    }

    private func scheduleAutoSearch(_ term: String) {
        guard !term.isEmpty else { clearSearch(); return }
        guard term != lastAutoSearchQuery, term != pendingSearchTerm else { return }
        clearSearch()
        pendingSearchTerm = term
        autoSearchTask = Task {
            do { try await Task.sleep(for: .milliseconds(500)) } catch { return }
            guard !Task.isCancelled, selectedText == term else { return }
            pendingSearchTerm = ""
            searchTranscript(query: term)
        }
    }

    private func createNoteFromTranscript() {
        let noteTitle = lastAutoSearchQuery
        guard !noteTitle.isEmpty, !isCreatingNote else { return }
        isCreatingNote = true
        searchError = nil
        let vault = vaultScope?.scope ?? ""
        Task {
            do {
                let created = try await client.createNote(title: noteTitle, vault: vault)
                clearSearch()
                onOpenNote(created.noteID)
            } catch let error as APIError where error.status == 409 {
                searchError = "A note with the same title already exists"
                createTaken = true
            } catch { searchError = (error as? APIError)?.message ?? error.localizedDescription }
            isCreatingNote = false
        }
    }

    private func stopAndSave() {
        let vault = vaultScope?.scope ?? ""
        let date = Self.todayString()
        Task {
            await model.stop()
            recordingStart = nil
            await writer.save(model.transcript, vault: vault, date: date)
        }
    }

    private func appendToJournal() {
        let snapshot = model.transcript
        let vault = vaultScope?.scope ?? ""
        let date = Self.todayString()
        Task { await writer.save(snapshot, vault: vault, date: date) }
    }

    private static func todayString() -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: Date())
    }

    /// A SearchResult for an exact `resolveTerm` hit (web VoiceView's taken
    /// path): the resolved ref rendered as a Titles hit. Built through JSON
    /// because SearchResult exposes no memberwise init.
    private static func searchResult(for ref: NoteRef) -> SearchResult? {
        let object: [String: Any] = [
            "note_id": ref.noteID.raw,
            "file_kind": ref.fileKind,
            "title": ref.title,
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: object) else { return nil }
        return try? JSONDecoder().decode(SearchResult.self, from: data)
    }
}
