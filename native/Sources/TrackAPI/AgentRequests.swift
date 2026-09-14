import Foundation

public enum RequestIntent: String, Codable, Sendable, CaseIterable {
    case explain, research, update
    public var label: String {
        switch self { case .explain: "Explain"; case .research: "Research"; case .update: "Update" }
    }
}

public struct AgentInfo: Decodable, Sendable, Identifiable {
    public var id: String
    public var name: String?
    public var operations: [String]?
    public var agmsgAvailable: Bool
    public func supports(_ intent: RequestIntent) -> Bool {
        operations?.isEmpty != false || operations?.contains(intent.rawValue) == true
    }
    enum CodingKeys: String, CodingKey { case id, name, operations, agmsgAvailable = "agmsg_available" }
}
public struct AgentsResponse: Decodable, Sendable { public var agents: [AgentInfo] }

/// Read IDs are qualified; write IDs must be bare JSON integers for the Go gateway.
public struct RequestNoteRef: Codable, Sendable, Equatable {
    public var noteID: TrackID
    public var vault: String?
    public var title: String?
    public var body: String?
    public var etag: String?
    public var fileKind: String?

    public init(id: TrackID, title: String? = nil, body: String? = nil, etag: String? = nil, fileKind: String? = nil) {
        noteID = id
        vault = id.split().vault
        self.title = title
        self.body = body
        self.etag = etag
        self.fileKind = fileKind
    }
    enum CodingKeys: String, CodingKey {
        case noteID = "note_id", vault, title, body, etag, fileKind = "file_kind"
    }
    public func encode(to encoder: Encoder) throws {
        let parts = noteID.split()
        guard let numeric = Int64(parts.id), numeric > 0 else {
            throw EncodingError.invalidValue(noteID, .init(codingPath: encoder.codingPath, debugDescription: "Agent requests require a live numeric note ID"))
        }
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(numeric, forKey: .noteID)
        try c.encode(parts.vault.isEmpty ? (vault ?? "") : parts.vault, forKey: .vault)
        try c.encodeIfPresent(title, forKey: .title)
        try c.encodeIfPresent(body, forKey: .body)
        try c.encodeIfPresent(etag, forKey: .etag)
        try c.encodeIfPresent(fileKind, forKey: .fileKind)
    }
}

public struct PriorAnswer: Codable, Sendable, Equatable {
    public var requestID: String
    public var intent: RequestIntent?
    public var instruction: String?
    public var answerMarkdown: String?
    enum CodingKeys: String, CodingKey {
        case requestID = "request_id", intent, instruction, answerMarkdown = "answer_markdown"
    }
}
public struct RequestContext: Codable, Sendable, Equatable {
    public var quote: String?
    public var note: RequestNoteRef?
    public var priorAnswers: [PriorAnswer]?
    public init(quote: String = "", note: RequestNoteRef? = nil, parent: AgentRequest? = nil) {
        self.quote = quote
        self.note = note
        priorAnswers = parent.map { [PriorAnswer(requestID: $0.id, intent: $0.intent, instruction: $0.instruction, answerMarkdown: $0.result?.answerMarkdown)] }
    }
    enum CodingKeys: String, CodingKey { case quote, note, priorAnswers = "prior_answers" }
}
public struct CreateAgentRequestInput: Encodable, Sendable, Equatable {
    public var clientRequestID: String
    public var parentRequestID: String?
    public var intent: RequestIntent
    public var instruction: String
    public var agentID: String
    public var context: RequestContext
    public var updateTarget: RequestNoteRef?
    public init(clientRequestID: String, intent: RequestIntent, instruction: String, agentID: String, context: RequestContext, parentRequestID: String? = nil, updateTarget: RequestNoteRef? = nil) {
        self.clientRequestID = clientRequestID
        self.intent = intent
        self.instruction = instruction
        self.agentID = agentID
        self.context = context
        self.parentRequestID = parentRequestID
        self.updateTarget = updateTarget
    }
    enum CodingKeys: String, CodingKey {
        case clientRequestID = "client_request_id", parentRequestID = "parent_request_id"
        case intent, instruction, agentID = "agent_id", context, updateTarget = "update_target"
    }
}

public struct AppliedUpdate: Decodable, Sendable {
    public var beforeBody: String?
    public var beforeEtag: String?
    public var afterBody: String?
    public var afterEtag: String?
    public var reason: String?
    public var appliedAt: String?
    enum CodingKeys: String, CodingKey {
        case beforeBody = "before_body", beforeEtag = "before_etag", afterBody = "after_body", afterEtag = "after_etag", reason, appliedAt = "applied_at"
    }
}
public struct SavedRequestNote: Decodable, Sendable {
    public var noteID: TrackID
    public var vault: String?
    public var title: String
    public var status: String
    public var savedAt: String?
    enum CodingKeys: String, CodingKey { case noteID = "note_id", vault, title, status, savedAt = "saved_at" }
}
public struct RequestResult: Decodable, Sendable {
    public var answerMarkdown: String?
    public var proposedBody: String?
    public var sources: [String]?
    public var apply: AppliedUpdate?
    public var saved: SavedRequestNote?
    public var unresolved: [String]
    public var uncertain: [String]
    enum CodingKeys: String, CodingKey {
        case answerMarkdown = "answer_markdown", proposedBody = "proposed_body", sources, apply, saved
        case unresolved, uncertain, unresolvedQuestions = "unresolved_questions", uncertainPoints = "uncertain_points"
    }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        answerMarkdown = try c.decodeIfPresent(String.self, forKey: .answerMarkdown)
        proposedBody = try c.decodeIfPresent(String.self, forKey: .proposedBody)
        sources = try c.decodeIfPresent([String].self, forKey: .sources)
        apply = try c.decodeIfPresent(AppliedUpdate.self, forKey: .apply)
        saved = try c.decodeIfPresent(SavedRequestNote.self, forKey: .saved)
        func list(_ key: CodingKeys, fallback: CodingKeys) -> [String] {
            let key = c.contains(key) ? key : fallback
            if let items = try? c.decode([String].self, forKey: key) { return items }
            if let text = try? c.decode(String.self, forKey: key), !text.isEmpty { return [text] }
            return []
        }
        unresolved = list(.unresolved, fallback: .unresolvedQuestions)
        uncertain = list(.uncertain, fallback: .uncertainPoints)
    }
}
public struct RequestAttempt: Decodable, Sendable, Identifiable {
    public var id: String
    public var status: String
}
public struct AgentRequest: Decodable, Sendable, Identifiable {
    public var id: String
    public var clientRequestID: String?
    public var parentRequestID: String?
    public var vault: String?
    public var intent: RequestIntent
    public var instruction: String
    public var agentID: String
    public var status: String
    public var error: String?
    public var context: RequestContext?
    public var updateTarget: RequestNoteRef?
    public var attempts: [RequestAttempt]?
    public var result: RequestResult?
    public var canCancel: Bool { status == "queued" || status == "running" }
    public var canRetry: Bool { status == "failed" || status == "conflict" }
    public var canSave: Bool { status == "completed" && intent != .update && result?.saved == nil && result?.answerMarkdown?.isEmpty == false }
    enum CodingKeys: String, CodingKey {
        case id, clientRequestID = "client_request_id", parentRequestID = "parent_request_id", vault, intent, instruction
        case agentID = "agent_id", status, error, context, updateTarget = "update_target", attempts, result
    }
}
public struct AgentRequestsResponse: Decodable, Sendable {
    public var requests: [AgentRequest]
    public var nextCursor: String?
    enum CodingKeys: String, CodingKey { case requests, nextCursor = "next_cursor" }
}
public struct CreateAgentRequestResponse: Decodable, Sendable {
    public var request: AgentRequest
    public var reused: Bool?
}
public struct SaveAgentRequestInput: Encodable, Sendable, Equatable {
    public var clientRequestID: String
    public var title: String
    public var vault: String?
    public init(clientRequestID: String, title: String, vault: String? = nil) {
        self.clientRequestID = clientRequestID; self.title = title; self.vault = vault
    }
    enum CodingKeys: String, CodingKey { case clientRequestID = "client_request_id", title, vault }
}

extension TrackClient {
    public func listAgents() async throws -> AgentsResponse { try await get(path: "/api/agents") }
    public func listAgentRequests(vault: String = "", cursor: String? = nil) async throws -> AgentRequestsResponse {
        var query = vaultQuery(vault)
        if let cursor, !cursor.isEmpty { query.append(URLQueryItem(name: "cursor", value: cursor)) }
        return try await get(path: "/api/requests", query: query)
    }
    public func getAgentRequest(id: String, vault: String = "") async throws -> AgentRequest {
        try await get(path: requestPath(id), query: vaultQuery(vault))
    }
    public func createAgentRequest(_ input: CreateAgentRequestInput, vault: String = "") async throws -> CreateAgentRequestResponse {
        try await postEncodable(path: "/api/requests", query: vaultQuery(vault), body: input)
    }
    public func cancelAgentRequest(id: String, vault: String = "") async throws -> AgentRequest {
        try await postEncodable(path: requestPath(id) + "/cancel", query: vaultQuery(vault), body: [String: String]())
    }
    public func retryAgentRequest(id: String, vault: String = "") async throws -> AgentRequest {
        try await postEncodable(path: requestPath(id) + "/retry", query: vaultQuery(vault), body: [String: String]())
    }
    public func saveAgentRequest(id: String, input: SaveAgentRequestInput, vault: String = "") async throws -> AgentRequest {
        try await postEncodable(path: requestPath(id) + "/save", query: vaultQuery(vault), body: input)
    }
    private func requestPath(_ id: String) throws -> String {
        // IDs are server-generated req-<milliseconds>-<hex>; never interpret an ID as a path.
        guard !id.isEmpty, id.utf8.allSatisfy({ (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || $0 == 45 || $0 == 95 }) else {
            throw APIError(status: 400, message: "Invalid request ID")
        }
        return "/api/requests/" + id
    }
}
