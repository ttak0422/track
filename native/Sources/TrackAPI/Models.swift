import Foundation

// Mirrors web/src/types.ts + web/src/vaultId.ts. The server sends numeric
// note ids; the client treats them as opaque strings end to end.

// MARK: - Identity

/// A note id as the client keys it. Unqualified (`"1785024015000"`) means the
/// launch vault; cross-vault ids qualify with the registry name and `~`
/// (`"blog~1786439635000"`). `~` (not `:`) because the id travels in URLs.
public struct TrackID: Hashable, Sendable, CustomStringConvertible {
    public let raw: String

    public init(_ raw: String) { self.raw = raw }

    /// `<vault>~<id>` when the vault is named, else the bare id.
    public static func qualify(vault: String, id: String) -> TrackID {
        TrackID(vault.isEmpty ? id : "\(vault)~\(id)")
    }

    /// Split for requests: `{id, vault}` query params (`vaultId.ts: split/idParams`).
    public func split() -> (id: String, vault: String) {
        guard let at = raw.firstIndex(of: "~") else { return (raw, "") }
        return (String(raw[raw.index(after: at)...]), String(raw[..<at]))
    }

    public var description: String { raw }
}

extension TrackID: Codable {
    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let s = try? c.decode(String.self) { self.init(s); return }
        // The live server marshals ids as JSON numbers (api.ts: stringifyIDs).
        self.init(String(try c.decode(Int64.self)))
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        try c.encode(raw)
    }
}

// MARK: - Notes

public struct NoteRef: Codable, Sendable {
    public var noteID: TrackID
    public var fileKind: String
    public var seenAt: Int?
    public var readAt: Int?
    public var path: String?
    public var title: String
    public var flags: [String]?

    enum CodingKeys: String, CodingKey {
        case noteID = "note_id", fileKind = "file_kind"
        case seenAt = "seen_at", readAt = "read_at"
        case path, title, flags
    }
}

public struct SearchResult: Codable, Sendable {
    public var ref: NoteRef
    public var tags: [String]?
    public var days: [String]?
    public var icon: String?
    public var line: Int?
    public var snippet: String?
    public var match: String?
    /// Registry name riding alongside the bare id (live search only).
    public var vault: String?

    /// The id the router and caches key by (vaultId.ts: qualify).
    public var qualifiedID: TrackID {
        TrackID.qualify(vault: vault ?? "", id: ref.noteID.raw)
    }

    // SearchResult extends NoteRef in types.ts; flatten by decoding both layers.
    public init(from decoder: Decoder) throws {
        ref = try NoteRef(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tags = try c.decodeIfPresent([String].self, forKey: .tags)
        days = try c.decodeIfPresent([String].self, forKey: .days)
        icon = try c.decodeIfPresent(String.self, forKey: .icon)
        line = try c.decodeIfPresent(Int.self, forKey: .line)
        snippet = try c.decodeIfPresent(String.self, forKey: .snippet)
        match = try c.decodeIfPresent(String.self, forKey: .match)
        vault = try c.decodeIfPresent(String.self, forKey: .vault)
    }

    public func encode(to encoder: Encoder) throws {
        try ref.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(tags, forKey: .tags)
        try c.encodeIfPresent(days, forKey: .days)
        try c.encodeIfPresent(icon, forKey: .icon)
        try c.encodeIfPresent(line, forKey: .line)
        try c.encodeIfPresent(snippet, forKey: .snippet)
        try c.encodeIfPresent(match, forKey: .match)
        try c.encodeIfPresent(vault, forKey: .vault)
    }

    enum CodingKeys: String, CodingKey {
        case tags, days, icon, line, snippet, match, vault
    }
}

public struct UnavailableVault: Codable, Sendable {
    public var name: String
    public var path: String
    public var error: String?
}

public struct SearchResponse: Codable, Sendable {
    public var results: [SearchResult]
    public var unavailable: [UnavailableVault]?
}

public struct ResolveResponse: Codable, Sendable {
    public var found: Bool
    public var note: NoteRef
}

public struct TaskItem: Codable, Sendable {
    public var line: Int
    public var state: String
    public var done: Bool
    public var priority: String?
    public var scheduled: String?
    public var due: String?
    public var completed: String?
    public var text: String
}

/// A note's parsed tasks (`NoteTasks` in types.ts). Shared by the note detail,
/// `/api/tasks`, and the task write response.
public struct NoteTasks: Codable, Sendable {
    public var items: [TaskItem]
}

/// One flattened typed note property as the engine indexes it (`NoteProp` in
/// types.ts): a sidecar props entry (line 0) or an inline "key:: value" body
/// field (1-based body line). A list value arrives as one entry per item under
/// the same key; link values carry the resolution key.
public struct NoteProp: Codable, Sendable {
    public var key: String
    public var value: String
    public var type: String
    public var line: Int
}

/// One row of the vault-wide dated listing (`/api/tasks` → TaskListResponse):
/// the task plus the note it lives in.
public struct TaskRow: Codable, Sendable {
    public var item: TaskItem
    public var noteID: TrackID
    public var fileKind: String
    public var title: String

    public init(from decoder: Decoder) throws {
        item = try TaskItem(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        noteID = try c.decode(TrackID.self, forKey: .noteID)
        fileKind = try c.decode(String.self, forKey: .fileKind)
        title = try c.decode(String.self, forKey: .title)
    }

    public func encode(to encoder: Encoder) throws {
        try item.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(noteID, forKey: .noteID)
        try c.encode(fileKind, forKey: .fileKind)
        try c.encode(title, forKey: .title)
    }

    enum CodingKeys: String, CodingKey {
        case noteID = "note_id", fileKind = "file_kind", title
    }
}

public struct TaskListResponse: Codable, Sendable {
    public var tasks: [TaskRow]
}

/// Write-path response (`POST /api/task` → TasksResponse): the note's
/// refreshed tasks plus the etag the next write must echo back.
public struct TasksResponse: Codable, Sendable {
    public var items: [TaskItem]
    public var etag: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        etag = try c.decode(String.self, forKey: .etag)
        let nested = try c.decodeIfPresent(NoteTasks.self, forKey: .tasks)
        items = nested?.items ?? []
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(NoteTasks(items: items), forKey: .tasks)
        try c.encode(etag, forKey: .etag)
    }

    enum CodingKeys: String, CodingKey {
        case tasks, etag
    }
}

public struct ExternalRef: Codable, Sendable {
    public var vault: String
    public var noteID: TrackID
    public var fileKind: String
    public var title: String
    public var path: String?

    enum CodingKeys: String, CodingKey {
        case vault, noteID = "note_id", fileKind = "file_kind", title, path
    }
}

public struct NoteDetail: Codable, Sendable {
    public var summary: SearchResult
    public var copyPath: String?
    public var body: String
    public var etag: String
    /// The note's creation date as the sidecar stores it (the vault's configured
    /// date format, day precision) and its last modification as the file mtime
    /// in unix seconds. Absent when unknown (`NoteDetail.created/updated`).
    public var created: String?
    public var updated: Int?
    public var tasks: NoteTasks?
    public var props: [NoteProp]?

    public init(from decoder: Decoder) throws {
        summary = try SearchResult(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        copyPath = try c.decodeIfPresent(String.self, forKey: .copyPath)
        body = try c.decode(String.self, forKey: .body)
        etag = try c.decode(String.self, forKey: .etag)
        created = try c.decodeIfPresent(String.self, forKey: .created)
        updated = try c.decodeIfPresent(Int.self, forKey: .updated)
        tasks = try c.decodeIfPresent(NoteTasks.self, forKey: .tasks)
        props = try c.decodeIfPresent([NoteProp].self, forKey: .props)
    }

    public func encode(to encoder: Encoder) throws {
        try summary.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(copyPath, forKey: .copyPath)
        try c.encode(body, forKey: .body)
        try c.encode(etag, forKey: .etag)
        try c.encodeIfPresent(created, forKey: .created)
        try c.encodeIfPresent(updated, forKey: .updated)
        try c.encodeIfPresent(tasks, forKey: .tasks)
        try c.encodeIfPresent(props, forKey: .props)
    }

    enum CodingKeys: String, CodingKey {
        case copyPath = "copy_path", body, etag
        case created, updated, tasks, props
    }
}

public struct NoteResponse: Codable, Sendable {
    public var note: NoteDetail
    public var backlinks: [NoteRef]
    public var external: [ExternalRef]?
    public var unavailable: [UnavailableVault]?
    public var trail: [NoteRef]?
    public var children: [NoteRef]?
}

// MARK: - Notes listing

/// `GET /api/notes` (`NotesResponse` in types.ts): the vault's notes,
/// recently-updated first, with activity days riding along.
public struct NotesResponse: Codable, Sendable {
    public var notes: [SearchResult]
}

// MARK: - Reading state

/// The shared reading milestones a `POST /api/note/read` recorded
/// (`ReadResponse`; ADR 0072). Both timestamps are monotonic firsts in unix
/// seconds and are present only once reached.
public struct ReadResponse: Codable, Sendable {
    public var vault: String?
    public var noteID: TrackID?
    public var seenAt: Int?
    public var readAt: Int?

    enum CodingKeys: String, CodingKey {
        case vault, noteID = "note_id", seenAt = "seen_at", readAt = "read_at"
    }
}

// MARK: - Note metadata

/// A note's editable sidecar metadata (`NoteMetaResponse` in types.ts): title,
/// tags, description, cover image, icon, flags, and typed props. `props` is the
/// free-form YAML "key: value" block the engine parses and validates.
public struct NoteMetaResponse: Codable, Sendable {
    public var title: String
    public var kind: String
    public var tags: [String]
    public var description: String
    public var image: String
    public var icon: String
    public var flags: [String]
    public var props: String
}

/// A save request replacing the whole editable metadata (`SaveNoteMetaRequest`
/// in types.ts). A rejected edit changes nothing.
public struct SaveNoteMetaRequest: Codable, Sendable {
    public var title: String
    public var tags: [String]
    public var description: String
    public var image: String
    public var icon: String
    public var flags: [String]
    public var props: String
}

// MARK: - Hierarchy

/// One note in the vault-wide "up" tree (`HierarchyNode` in types.ts): a note
/// reference plus the notes that name it as their parent. Only notes the
/// hierarchy places are in it.
public struct HierarchyNode: Codable, Sendable {
    public var ref: NoteRef
    public var children: [HierarchyNode]?

    public init(from decoder: Decoder) throws {
        ref = try NoteRef(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        children = try c.decodeIfPresent([HierarchyNode].self, forKey: .children)
    }

    public func encode(to encoder: Encoder) throws {
        try ref.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(children, forKey: .children)
    }

    enum CodingKeys: String, CodingKey {
        case children
    }
}

/// `GET /api/hierarchy` (`HierarchyResponse` in types.ts): roots first, every
/// level by title.
public struct HierarchyResponse: Codable, Sendable {
    public var hierarchy: [HierarchyNode]
}

// MARK: - Graph

/// One note shown in a graph (`GraphNode` in types.ts). `center` marks the
/// local graph's focus; `size` is the precomputed grade (1–5), absent when the
/// client should fall back to its own degree-based sizing.
public struct GraphNode: Codable, Sendable {
    public var noteID: TrackID
    public var fileKind: String
    /// Registry name of the vault the node lives in, when more than one vault
    /// is addressed; lets a rendered graph tell two same-numbered notes apart.
    public var vault: String?
    public var path: String?
    public var title: String
    public var center: Bool?
    public var size: Int?

    enum CodingKeys: String, CodingKey {
        case noteID = "note_id", fileKind = "file_kind"
        case vault, path, title, center, size
    }
}

/// One directed link between graph nodes (`GraphEdge` in types.ts).
public struct GraphEdge: Codable, Sendable {
    public var sourceID: TrackID
    public var targetID: TrackID

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id", targetID = "target_id"
    }
}

/// The link graph around one note (`Graph` in types.ts).
public struct Graph: Codable, Sendable {
    public var centerID: TrackID
    public var nodes: [GraphNode]
    public var edges: [GraphEdge]

    enum CodingKeys: String, CodingKey {
        case centerID = "center_id", nodes, edges
    }
}

/// `GET /api/graph` / `/api/graph/local` (`GraphResponse` in types.ts).
public struct GraphResponse: Codable, Sendable {
    public var graph: Graph
}

// MARK: - Activity

/// One day's note activity (`ActivityDay` in types.ts): date as YYYY-MM-DD.
public struct ActivityDay: Codable, Sendable {
    public var date: String
    public var count: Int
}

/// `GET /api/activity` summary (`ActivitySummary` in types.ts), counted from
/// note days within the inclusive [since, until] window.
public struct ActivitySummary: Codable, Sendable {
    public var since: String
    public var until: String
    public var total: Int
    public var counts: [ActivityDay]
}

/// `GET /api/activity` (`ActivityResponse` in types.ts).
public struct ActivityResponse: Codable, Sendable {
    public var activity: ActivitySummary
}
