import AVFoundation
import Foundation
import Speech

public enum SpeechUpdate: Sendable, Equatable {
    case partial(String)
    case final(String)
    case failure(String)
}

@MainActor
public protocol SpeechDriver: AnyObject {
    func authorize() async -> String?
    func start(locale: String, receive: @escaping @MainActor (SpeechUpdate) -> Void) throws
    func stopAudio()
    func finishAudio()
    func cancel()
}

/// One owner, one task. Generation checks also invalidate pending permission replies.
@MainActor
public final class NativeSpeechSession {
    private let driver: any SpeechDriver
    private let captureLimit: Duration
    private let finishLimit: Duration
    private var deadline: Task<Void, Never>?
    private var generation = 0
    private var id: String?
    private var finishing = false
    private var capturing = false
    private var authorization: Task<Void, Never>?
    public var emit: ((String, String, String) -> Void)?

    public init(driver: any SpeechDriver, captureLimit: Duration = .seconds(55), finishLimit: Duration = .seconds(3)) {
        self.driver = driver
        self.captureLimit = captureLimit
        self.finishLimit = finishLimit
    }

    @discardableResult
    public func start(id: String, locale: String) -> Task<Void, Never> {
        guard self.id == nil else {
            emit?(id, "error", "Voice input is already active. Stop it before starting again.")
            return Task {}
        }
        generation += 1
        let token = generation
        self.id = id
        let startup = Task { [weak self] in
            guard let self, !Task.isCancelled else { return }
            let error = await self.driver.authorize()
            guard self.generation == token else { return }
            self.authorization = nil
            if let error { self.end(error: error); return }
            do {
                try self.driver.start(locale: locale) { [weak self] update in
                    guard let self, self.generation == token, let id = self.id else { return }
                    switch update {
                    case .partial(let text):
                        if !self.finishing { self.emit?(id, "partial", text) }
                    case .final(let text):
                        self.emit?(id, "final", text)
                        self.end()
                    case .failure(let message): self.end(error: message)
                    }
                }
                guard self.generation == token, self.id != nil else { return }
                self.capturing = true
                self.emit?(id, "started", "")
                self.deadline = Task { [weak self, captureLimit] in
                    do { try await Task.sleep(for: captureLimit) } catch { return }
                    self?.stop(id: id)
                }
            } catch {
                self.end(error: error.localizedDescription)
            }
        }
        authorization = startup
        return startup
    }

    public func stop(id: String) {
        guard self.id == id, !finishing else { return }
        guard capturing else { end(); return }
        finishing = true
        driver.stopAudio()
        deadline?.cancel()
        let token = generation
        deadline = Task { [weak self, finishLimit] in
            do { try await Task.sleep(for: finishLimit) } catch { return }
            guard let self, self.generation == token else { return }
            self.end(error: "macOS speech recognition did not finish. Unconfirmed text was discarded; try again.")
        }
        driver.finishAudio()
    }

    public func cancel(id: String? = nil) {
        guard id == nil || self.id == id else { return }
        end()
    }

    private func end(error: String? = nil) {
        let previous = id
        generation += 1
        id = nil
        finishing = false
        capturing = false
        authorization?.cancel()
        authorization = nil
        deadline?.cancel()
        deadline = nil
        driver.stopAudio()
        driver.cancel()
        guard let previous else { return }
        if let error { emit?(previous, "error", error) }
        emit?(previous, "ended", "")
    }
}

/// SFSpeechRecognizer is available throughout Track's macOS 14+ deployment range.
@MainActor
public final class AppleSpeechDriver: NSObject, SpeechDriver, SFSpeechRecognizerDelegate {
    private var engine: AVAudioEngine?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var recognizer: SFSpeechRecognizer?
    private var hasTap = false
    private var receive: (@MainActor (SpeechUpdate) -> Void)?
    private var configurationObserver: NSObjectProtocol?

    public override init() { super.init() }

    public func authorize() async -> String? {
        let speech = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
        }
        guard !Task.isCancelled else { return "Voice input was cancelled." }
        switch speech {
        case .authorized: break
        case .denied: return "Speech Recognition permission was denied. Allow Track in System Settings > Privacy & Security > Speech Recognition, then try again."
        case .restricted: return "Speech Recognition is restricted on this Mac. Contact your administrator or choose Browser speech."
        default: return "Speech Recognition permission is unavailable. Try again or choose Browser speech."
        }
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized: return nil
        case .restricted: return "Microphone access is restricted on this Mac. Contact your administrator."
        case .denied: return Self.microphoneDenied
        case .notDetermined:
            return await AVCaptureDevice.requestAccess(for: .audio) ? nil : Self.microphoneDenied
        @unknown default: return "Microphone permission is unavailable on this Mac."
        }
    }

    private static let microphoneDenied = "Microphone permission was denied. Allow Track in System Settings > Privacy & Security > Microphone, then try again."

    public func start(locale: String, receive: @escaping @MainActor (SpeechUpdate) -> Void) throws {
        cancel()
        let language = Locale(identifier: locale)
        guard SFSpeechRecognizer.supportedLocales().contains(language),
              let recognizer = SFSpeechRecognizer(locale: language) else {
            throw failure("macOS speech recognition does not support \(locale) on this Mac. Choose Browser speech if available.")
        }
        guard recognizer.isAvailable else {
            throw failure("macOS speech recognition is unavailable for \(locale). Check your connection and Dictation availability, or choose Browser speech.")
        }
        self.receive = receive
        self.recognizer = recognizer
        recognizer.delegate = self
        recognizer.queue = .main
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        // OS speech may use Apple servers. Do not promise on-device processing.
        request.requiresOnDeviceRecognition = false
        self.request = request
        let engine = AVAudioEngine()
        self.engine = engine
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            throw failure("No usable microphone is available. Connect an audio input and try again.")
        }
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            request.append(buffer)
        }
        hasTap = true
        configurationObserver = NotificationCenter.default.addObserver(
            forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main
        ) { _ in
            Task { @MainActor in receive(.failure("The audio input changed. Voice input stopped; try again.")) }
        }
        task = recognizer.recognitionTask(with: request) { result, error in
            // Only immutable data crosses into the main actor; late callbacks are gated by the session.
            let text = result?.bestTranscription.formattedString
            let final = result?.isFinal == true
            let message = error?.localizedDescription
            Task { @MainActor in
                if let text { receive(final ? .final(text) : .partial(text)) }
                if let message, !final {
                    receive(.failure("macOS speech recognition failed: \(message). Try again or choose Browser speech."))
                }
            }
        }
        engine.prepare()
        try engine.start()
    }

    public func stopAudio() {
        if let configurationObserver {
            NotificationCenter.default.removeObserver(configurationObserver)
            self.configurationObserver = nil
        }
        engine?.stop()
        if hasTap { engine?.inputNode.removeTap(onBus: 0); hasTap = false }
        engine = nil
    }

    public func finishAudio() { request?.endAudio(); task?.finish() }

    public func cancel() {
        stopAudio()
        recognizer?.delegate = nil
        task?.cancel()
        task = nil
        request = nil
        recognizer = nil
        receive = nil
    }

    nonisolated public func speechRecognizer(_ speechRecognizer: SFSpeechRecognizer, availabilityDidChange available: Bool) {
        guard !available else { return }
        let identity = ObjectIdentifier(speechRecognizer)
        Task { @MainActor [weak self] in
            // Ignore a delegate callback from a task that has since been cancelled.
            guard let current = self?.recognizer, ObjectIdentifier(current) == identity else { return }
            self?.receive?(.failure("macOS speech recognition became unavailable. Check your connection and try again."))
        }
    }

    private func failure(_ message: String) -> NSError {
        NSError(domain: "TrackSpeech", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
    }
}
