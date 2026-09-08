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
// Scope: this is the record-and-transcribe surface only. Copy-to-clipboard and
// journal append live outside it; VoiceView renders the transcript so a
// follow-up can wire it to the vault.

// MARK: - Model

@MainActor
@Observable
public final class VoiceInputModel {
    /// The live (interim) and final transcript of the session.
    public private(set) var transcript = ""
    /// The portion confirmed by a final recognition result. The remainder is
    /// kept separate so the view can render recognition in progress softly.
    public private(set) var interimTranscript = ""
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
    /// Guards start() across the async authorization wait.
    private var isStarting = false
    /// Bumped per session so a finished session never tears down a newer one
    /// that started before its final result arrived.
    private var sessionID = 0
    private var finalizedTranscript = ""
    private var retryCount = 0
    private var restartTask: Task<Void, Never>?

    public init() {
        recognizer = SFSpeechRecognizer(locale: Locale(identifier: "ja-JP"))
    }

    /// Starts a recording session: requests permission if needed, then wires
    /// the mic tap to a fresh recognition task. Failures land in `error`.
    public func start() async {
        guard !isRecording, !isStarting else { return }
        isStarting = true
        defer { isStarting = false }
        error = nil
        interimTranscript = ""
        transcript = ""
        finalizedTranscript = ""
        retryCount = 0
        restartTask?.cancel()

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

        sessionID += 1
        let session = sessionID

        let node = audioEngine.inputNode
        let format = node.outputFormat(forBus: 0)
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
        task = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor in
                guard let self, self.sessionID == session else { return }
                if let result {
                    let text = result.bestTranscription.formattedString
                    let prefix = self.finalizedTranscript.isEmpty ? "" : self.finalizedTranscript + " "
                    self.transcript = prefix + text
                    self.interimTranscript = result.isFinal ? "" : text
                    if result.isFinal {
                        self.finalizedTranscript = self.transcript
                        self.request = nil
                        self.task = nil
                        self.scheduleRecognitionRestart(session: session)
                    }
                } else {
                    // An error right after stop() is normal ("no speech
                    // detected"); keep the transcript and only surface the
                    // failure while recording was still active.
                    guard self.isRecording else { return }
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

    /// Ends the session: stops the engine and signals end-of-audio so the
    /// recognizer delivers its final result (the transcript settles on the
    /// finalized text).
    public func stop() {
        guard isRecording else { return }
        isRecording = false
        if audioEngine.isRunning {
            audioEngine.stop()
            audioEngine.inputNode.removeTap(onBus: 0)
        }
        request?.endAudio()
        restartTask?.cancel()
        restartTask = nil
    }

    public func clear() {
        transcript = ""
        interimTranscript = ""
        finalizedTranscript = ""
    }
}

// MARK: - View

/// The record-and-transcript surface. The record button toggles the session
/// and the transcript (live while speaking, final after stop) is shown as
/// selectable text; the error line surfaces permission/mic/recognizer issues.
/// "Append to today's journal" opens (or creates) today's journal and writes
/// the transcript onto the end of its body.
public struct VoiceView: View {
    @State private var model = VoiceInputModel()

    private let client: TrackClient
    @State private var appendError: String?
    @State private var appendNote: String?
    @State private var isAppending = false
    @State private var searchResults: [SearchResult] = []
    @State private var searchError: String?
    @State private var isSearching = false
    @State private var copied = false
    @State private var lastSavedTranscript = ""
    @State private var recordingStart: Date?
    @State private var openedNoteID: TrackID?
    @State private var isShowingNote = false
    @State private var isCreatingNote = false

    public init(client: TrackClient) {
        self.client = client
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                Button {
                    if model.isRecording {
                        model.stop()
                        recordingStart = nil
                        autoSaveAfterStop()
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
                .disabled(model.transcript.isEmpty)

                Button {
                    appendToJournal()
                } label: {
                    Label(
                        isAppending ? "Appending…" : "Append to today's journal",
                        systemImage: "square.and.pencil"
                    )
                }
                .buttonStyle(.bordered)
                .disabled(model.transcript.isEmpty || isAppending)
                .help("Append the transcript to today's journal")

                Button {
                    searchTranscript()
                } label: {
                    Label(
                        isSearching ? "Searching…" : "Search notes",
                        systemImage: "magnifyingglass"
                    )
                }
                .buttonStyle(.bordered)
                .disabled(model.transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSearching)
                .help("Search notes with the transcript")

                Spacer()
            }

            if let error = model.error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            if let appendError {
                Text(appendError)
                    .font(.caption)
                    .foregroundStyle(.red)
            } else if let appendNote {
                Text(appendNote)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if let searchError {
                Text(searchError)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            if model.transcript.isEmpty {
                ContentUnavailableView(
                    "Voice input",
                    systemImage: "waveform",
                    description: Text("Press Record and speak (ja-JP). The transcript appears here.")
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    Text(model.interimTranscript.isEmpty ? model.transcript : String(model.transcript.dropFirst(model.transcript.count - model.interimTranscript.count)))
                        .foregroundStyle(model.interimTranscript.isEmpty ? .primary : .secondary)
                        .font(.body)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            if !searchResults.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Search results")
                        .font(.headline)

                    ForEach(Array(resultSections.enumerated()), id: \.offset) { _, section in
                        if !section.results.isEmpty {
                            Text(section.title).font(.subheadline.weight(.semibold))
                            ForEach(section.results, id: \.qualifiedID) { result in
                                Button {
                                    openedNoteID = result.qualifiedID
                                    isShowingNote = true
                                } label: {
                                    VStack(alignment: .leading, spacing: 3) {
                                        HStack(spacing: 6) {
                                            Text(result.ref.title).font(.body.weight(.medium))
                                            Image(systemName: "arrow.up.right")
                                                .font(.caption2).foregroundStyle(.tertiary)
                                        }
                                        if let snippet = result.snippet, !snippet.isEmpty {
                                            Text(snippet).font(.caption).foregroundStyle(.secondary).lineLimit(2)
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
                    Button {
                        createNoteFromTranscript()
                    } label: {
                        Label(
                            isCreatingNote ? "Creating…" : "Create a new note from this transcript",
                            systemImage: "plus"
                        )
                    }
                    .buttonStyle(.bordered)
                    .disabled(isCreatingNote || model.transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                .padding(.top, 4)
            }
        }
        .padding(16)
        .sheet(isPresented: $isShowingNote) {
            if let openedNoteID {
                VoiceNotePreviewView(client: client, noteID: openedNoteID)
            }
        }
    }

    private var recordingStartedAt: Date? { model.isRecording ? (recordingStart ?? Date()) : nil }
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

    private func clearTranscript() {
        model.clear()
        searchResults = []
        searchError = nil
    }

    private func autoSaveAfterStop() {
        Task {
            try? await Task.sleep(for: .milliseconds(400))
            autoSaveTranscript(model.transcript)
        }
    }

    private func autoSaveTranscript(_ snapshot: String) {
        let previous = lastSavedTranscript
        let tail = snapshot.hasPrefix(previous) ? String(snapshot.dropFirst(previous.count)) : snapshot
        guard !tail.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, !isAppending else { return }
        isAppending = true
        appendError = nil
        appendNote = nil
        Task {
            do {
                let journal = try await client.openJournal(date: Self.todayString())
                let note = try await client.getNote(journal.noteID)
                let base = note.note.body.trimmingCharacters(in: .newlines)
                let body = base.isEmpty ? tail : base + "\n\n" + tail
                _ = try await client.saveNote(id: journal.noteID, body: body, etag: note.note.etag)
                lastSavedTranscript = snapshot
                appendNote = "Saved to today’s journal"
            } catch {
                appendError = error.localizedDescription
            }
            isAppending = false
        }
    }

    private static func elapsedString(since date: Date) -> String {
        let seconds = max(0, Int(Date().timeIntervalSince(date)))
        return String(format: "%02d:%02d", seconds / 60, seconds % 60)
    }

    /// Search the finalized or currently visible transcript without changing
    /// the existing journal append flow. The server remains responsible for
    /// matching titles, paths, and note bodies.
    private func searchTranscript() {
        let query = model.transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else { return }
        isSearching = true
        searchError = nil
        searchResults = []
        Task {
            do {
                let response = try await client.searchNotes(query: query, limit: 8)
                searchResults = response.results
            } catch {
                searchError = error.localizedDescription
            }
            isSearching = false
        }
    }

    private func createNoteFromTranscript() {
        let title = model.transcript
            .split(whereSeparator: \.isNewline)
            .first.map(String.init)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let noteTitle = String((title?.prefix(80) ?? "Voice note"))
        guard !noteTitle.isEmpty else { return }
        isCreatingNote = true
        searchError = nil
        Task {
            do {
                let created = try await client.createNote(title: noteTitle)
                openedNoteID = created.noteID
                isShowingNote = true
            } catch {
                searchError = error.localizedDescription
            }
            isCreatingNote = false
        }
    }

    /// Open (or create) today's journal, read its current body, and save the
    /// transcript onto the end. The read's etag is echoed back so a stale view
    /// refuses the save; failures land in `appendError`.
    private func appendToJournal() {
        guard !model.transcript.isEmpty else { return }
        isAppending = true
        appendError = nil
        appendNote = nil
        Task {
            do {
                let journal = try await client.openJournal(date: Self.todayString())
                let note = try await client.getNote(journal.noteID)
                var body = note.note.body
                if !body.isEmpty && !body.hasSuffix("\n") { body += "\n" }
                body += model.transcript + "\n"
                _ = try await client.saveNote(id: journal.noteID, body: body, etag: note.note.etag)
                lastSavedTranscript = model.transcript
                appendNote = "Appended to \(Self.todayString()) journal"
            } catch {
                appendError = error.localizedDescription
            }
            isAppending = false
        }
    }

    private static func todayString() -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: Date())
    }
}

private struct VoiceNotePreviewView: View {
    let client: TrackClient
    let noteID: TrackID
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var bodyText = ""
    @State private var error: String?
    @State private var isLoading = true

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(title.isEmpty ? "Note" : title).font(.headline)
                Spacer()
                Button("Done") { dismiss() }
            }
            Divider()
            if isLoading {
                ProgressView()
            } else if let error {
                Text(error).foregroundStyle(.red)
            } else {
                ScrollView {
                    Text(bodyText).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .padding(20)
        .frame(minWidth: 420, minHeight: 280)
        .task {
            do {
                let response = try await client.getNote(noteID)
                title = response.note.summary.ref.title
                bodyText = response.note.body
            } catch {
                self.error = error.localizedDescription
            }
            isLoading = false
        }
    }
}
