import AVFoundation
import Foundation
import Observation
import Speech
import SwiftUI

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

        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        self.request = request

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
        task = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor in
                guard let self, self.sessionID == session else { return }
                if let result {
                    self.transcript = result.bestTranscription.formattedString
                    if result.isFinal {
                        self.isRecording = false
                        self.request = nil
                        self.task = nil
                    }
                } else {
                    // An error right after stop() is normal ("no speech
                    // detected"); keep the transcript and only surface the
                    // failure while recording was still active.
                    if self.isRecording {
                        self.error = error?.localizedDescription ?? "音声認識に失敗しました。"
                    }
                    self.isRecording = false
                    self.request = nil
                    self.task = nil
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
    }
}

// MARK: - View

/// The record-and-transcript surface. The record button toggles the session
/// and the transcript (live while speaking, final after stop) is shown as
/// selectable text; the error line surfaces permission/mic/recognizer issues.
/// Copy-to-clipboard and journal append are out of scope here.
public struct VoiceView: View {
    @State private var model = VoiceInputModel()

    public init() {}

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                Button {
                    if model.isRecording {
                        model.stop()
                    } else {
                        Task { await model.start() }
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
                    Text("Recording…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }

            if let error = model.error {
                Text(error)
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
                    Text(model.transcript)
                        .font(.body)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .padding(16)
    }
}