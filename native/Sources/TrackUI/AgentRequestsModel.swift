import Foundation
import Observation
import TrackAPI

public struct AgentRequestTarget: Identifiable, Sendable {
    public let id = UUID()
    public var vault: String
    public var title: String
    public var quote: String
    public var note: RequestNoteRef?
    public var intent: RequestIntent

    public init(vault: String = "", title: String = "Agent requests", quote: String = "", note: RequestNoteRef? = nil, intent: RequestIntent = .explain) {
        self.vault = vault; self.title = title; self.quote = quote; self.note = note; self.intent = intent
    }
    public init(note: NoteResponse, id: TrackID, quote: String = "", intent: RequestIntent = .explain) {
        let detail = note.note
        self.init(vault: id.split().vault, title: detail.summary.ref.title, quote: quote,
                  note: RequestNoteRef(id: id, title: detail.summary.ref.title, body: detail.body, etag: detail.etag, fileKind: detail.summary.ref.fileKind), intent: intent)
    }
}

@MainActor
@Observable
public final class AgentRequestsModel {
    public let client: TrackClient
    public private(set) var target: AgentRequestTarget
    public private(set) var agents: [AgentInfo] = []
    public private(set) var requests: [AgentRequest] = []
    public private(set) var selected: AgentRequest?
    public private(set) var parent: AgentRequest?
    public private(set) var isRefreshing = false
    public private(set) var isWorking = false
    public private(set) var agentsError: String?
    public private(set) var refreshError: String?
    public private(set) var actionError: String?
    public private(set) var nextCursor: String?
    public var intent: RequestIntent
    public var instruction: String
    public var agentID = ""
    public var saveTitle = ""
    public var saveVault = ""
    private var generation = UUID()
    private var loadedMore = false
    private var pendingCreate: CreateAgentRequestInput?
    private var pendingSave: (requestID: String, input: SaveAgentRequestInput)?

    public init(client: TrackClient, target: AgentRequestTarget = AgentRequestTarget()) {
        self.client = client
        self.target = target
        intent = target.intent
        instruction = target.note == nil && target.quote.isEmpty ? "" : target.quote.isEmpty ? "Explain this note" : "Explain this: \(target.quote)"
    }

    public var availableAgents: [AgentInfo] { agents.filter { $0.supports(intent) } }
    public var canSend: Bool {
        !isWorking && !instruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && availableAgents.contains { $0.id == agentID && $0.agmsgAvailable }
            && (intent != .update || (target.note?.etag?.isEmpty == false && (target.note?.vault ?? target.note?.noteID.split().vault ?? "") == target.vault))
    }

    public func chooseAgent() {
        if !availableAgents.contains(where: { $0.id == agentID && $0.agmsgAvailable }) {
            agentID = availableAgents.first(where: \.agmsgAvailable)?.id ?? ""
        }
    }

    public func refresh(loadMore: Bool = false) async {
        guard !isRefreshing, !isWorking else { return }
        isRefreshing = true
        let current = generation
        defer { isRefreshing = false }
        do {
            let list = try await client.listAgentRequests(vault: target.vault, cursor: loadMore ? nextCursor : nil)
            guard current == generation, !Task.isCancelled else { return }
            if loadMore {
                let known = Set(requests.map(\.id))
                requests += list.requests.filter { !known.contains($0.id) }
                loadedMore = true
            } else {
                let fresh = Set(list.requests.map(\.id))
                requests = list.requests + requests.filter { !fresh.contains($0.id) }
            }
            if loadMore || !loadedMore { nextCursor = list.nextCursor }
            if let selectedID = selected?.id {
                // The selection can live beyond page one; fetch its current state explicitly.
                let detail = try await client.getAgentRequest(id: selectedID, vault: target.vault)
                guard current == generation, !Task.isCancelled else { return }
                adopt(detail)
            }
            refreshError = nil
        } catch {
            guard current == generation, !Task.isCancelled else { return }
            refreshError = Self.message(error)
        }
    }

    public func loadAgents() async {
        do {
            agents = try await client.listAgents().agents
            agentsError = nil
            chooseAgent()
        } catch { agentsError = Self.message(error) }
    }

    public func select(_ request: AgentRequest) {
        guard !isWorking else { return }
        generation = UUID()
        selected = request
        saveTitle = request.result?.saved?.title ?? ""
        saveVault = ""
        pendingSave = nil
        actionError = nil
    }

    public func followUp(_ request: AgentRequest) {
        guard !isWorking else { return }
        generation = UUID()
        parent = request
        selected = nil
        intent = request.intent == .update ? .explain : request.intent
        target.note = request.context?.note ?? request.updateTarget ?? target.note
        target.quote = request.context?.quote ?? ""
        if let title = target.note?.title { target.title = title }
        instruction = ""
        pendingCreate = nil
        actionError = nil
        chooseAgent()
    }

    public func clearFollowUp() { parent = nil }

    public func send() async {
        guard canSend else { return }
        isWorking = true
        generation = UUID()
        actionError = nil
        defer { isWorking = false }
        var input = CreateAgentRequestInput(
            clientRequestID: pendingCreate?.clientRequestID ?? UUID().uuidString,
            intent: intent, instruction: instruction.trimmingCharacters(in: .whitespacesAndNewlines), agentID: agentID,
            context: RequestContext(quote: target.quote, note: target.note, parent: parent),
            parentRequestID: parent?.id, updateTarget: intent == .update ? target.note : nil
        )
        // A lost response retries the identical request/key. Editing the input starts a new request.
        if let pendingCreate, pendingCreate != input { input.clientRequestID = UUID().uuidString }
        pendingCreate = input
        do {
            let result = try await client.createAgentRequest(input, vault: target.vault)
            adopt(result.request)
            pendingCreate = nil
            parent = nil
            instruction = ""
        } catch { actionError = Self.message(error) }
    }

    public func cancel(_ request: AgentRequest) async {
        guard request.canCancel, !isWorking else { return }
        isWorking = true
        generation = UUID()
        actionError = nil
        defer { isWorking = false }
        do { adopt(try await client.cancelAgentRequest(id: request.id, vault: target.vault)) }
        catch { actionError = Self.message(error) }
    }

    public func retry(_ request: AgentRequest) async {
        guard request.canRetry, !isWorking else { return }
        isWorking = true
        generation = UUID()
        actionError = nil
        defer { isWorking = false }
        do {
            // Retry creates an attempt and has no client key. After a lost response,
            // read the request first so another click cannot dispatch a second attempt.
            let fresh = try await client.getAgentRequest(id: request.id, vault: target.vault)
            guard fresh.canRetry else { adopt(fresh); return }
            adopt(try await client.retryAgentRequest(id: request.id, vault: target.vault))
        } catch { actionError = Self.message(error) }
    }

    public func saveAnswer() async {
        guard let selected, selected.canSave, !isWorking else { return }
        let title = saveTitle.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty else { actionError = "Enter a title for the saved answer."; return }
        isWorking = true
        generation = UUID()
        actionError = nil
        defer { isWorking = false }
        let vault = saveVault.trimmingCharacters(in: .whitespacesAndNewlines)
        var input = SaveAgentRequestInput(clientRequestID: pendingSave?.input.clientRequestID ?? UUID().uuidString, title: title, vault: vault.isEmpty ? nil : vault)
        if let pendingSave, pendingSave.requestID != selected.id || pendingSave.input != input { input.clientRequestID = UUID().uuidString }
        pendingSave = (selected.id, input)
        do {
            adopt(try await client.saveAgentRequest(id: selected.id, input: input, vault: target.vault))
            pendingSave = nil
            NotificationCenter.default.post(name: .trackVaultChanged, object: nil)
        } catch { actionError = Self.message(error) }
    }

    private func adopt(_ request: AgentRequest) {
        if let index = requests.firstIndex(where: { $0.id == request.id }) { requests[index] = request }
        else { requests.insert(request, at: 0) }
        selected = request
    }
    private static func message(_ error: Error) -> String {
        (error as? APIError)?.message ?? error.localizedDescription
    }
}
