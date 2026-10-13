import Foundation
import CoreFoundation

struct OxideError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

struct Environment: Codable {
    let id: String
    let label: String
    let environmentId: String
    let sshDestination: String?
    let node: String
    let adapter: String
    let hostConfig: String
    let webURL: String?
    let enabled: Bool

    func validate() throws {
        guard !id.isEmpty, !environmentId.isEmpty,
              [node, adapter, hostConfig].allSatisfy({ $0.hasPrefix("/") && !$0.contains("\n") && !$0.contains("\0") }) else {
            throw OxideError(message: "Environment \(label) needs an identity and absolute host-local paths.")
        }
        if let host = sshDestination {
            guard !host.hasPrefix("-"), !host.isEmpty,
                  host.range(of: "^[A-Za-z0-9_.@-]+$", options: .regularExpression) != nil else {
                throw OxideError(message: "Use a saved SSH host alias for \(label).")
            }
        }
        if let link = webURL {
            guard let url = URL(string: link), ["https", "http"].contains(url.scheme ?? ""), url.host != nil, url.user == nil, url.password == nil, url.query == nil, url.fragment == nil else {
                throw OxideError(message: "Full T3 URL must be HTTP(S), without credentials.")
            }
        }
    }
    var arguments: [String] {
        if let host = sshDestination {
            let command = "exec \(shellQuote(node)) \(shellQuote(adapter)) --config \(shellQuote(hostConfig))"
            return ["-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=8",
                    "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=2", "--", host, command]
        }
        return [adapter, "--config", hostConfig]
    }
}
func shellQuote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }

struct EnvironmentsFile: Codable {
    let version: Int
    let environments: [Environment]
    static let directory = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".config/t3oxide")
    static let url = directory.appendingPathComponent("environments.json")
    static func load() throws -> [Environment] {
        let data = try Data(contentsOf: url)
        guard data.count <= 64 * 1024 else { throw OxideError(message: "Environment configuration is too large.") }
        let config = try JSONDecoder().decode(Self.self, from: data)
        guard config.version == 1, config.environments.count <= 8,
              Set(config.environments.map(\.id)).count == config.environments.count,
              Set(config.environments.map(\.environmentId)).count == config.environments.count else {
            throw OxideError(message: "Expected version 1 and unique environment identities (maximum eight).")
        }
        for environment in config.environments { try environment.validate() }
        return config.environments.filter(\.enabled)
    }
}

/// No transcripts or tokens. Only bounded drafts and unacknowledged user operations.
final class LocalState {
    private let url: URL
    private(set) var loadError: String?
    var drafts: [String: String] = [:]
    var pending: [String: [String: Any]] = [:]
    var pendingStops: [String: [String: Any]] = [:]
    init(url: URL = EnvironmentsFile.directory.appendingPathComponent("drafts.json")) {
        self.url = url
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= 2 * 1024 * 1024,
                  let saved = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let loadedDrafts = saved["drafts"] as? [String: String],
                  let loadedPending = saved["pending"] as? [String: [String: Any]] else {
                throw OxideError(message: "Unsupported saved state.")
            }
            let loadedStops: [String: [String: Any]]
            if let value = saved["pendingStops"] {
                guard let stops = value as? [String: [String: Any]] else {
                    throw OxideError(message: "Unsupported saved Stop state.")
                }
                loadedStops = stops
            } else { loadedStops = [:] }
            guard loadedDrafts.count <= 40, loadedPending.count <= 8, loadedStops.count <= 8,
                  loadedDrafts.values.allSatisfy({ $0.utf8.count <= 64 * 1024 }) else {
                throw OxideError(message: "Saved state exceeds the local limits.")
            }
            drafts = loadedDrafts; pending = loadedPending; pendingStops = loadedStops
        } catch { loadError = "Saved drafts/actions could not be loaded. Preserve and repair ~/.config/t3oxide/drafts.json before sending new work." }
    }
    func save() throws {
        if let error = loadError { throw OxideError(message: error) }
        guard drafts.count <= 40, pending.count <= 8, pendingStops.count <= 8,
              drafts.values.allSatisfy({ $0.utf8.count <= 64 * 1024 }) else {
            throw OxideError(message: "Local draft limit reached. Shorten or finish an existing draft.")
        }
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true,
                                               attributes: [.posixPermissions: 0o700])
        let data = try JSONSerialization.data(withJSONObject: ["drafts": drafts, "pending": pending, "pendingStops": pendingStops], options: [.sortedKeys])
        guard data.count <= 2 * 1024 * 1024 else { throw OxideError(message: "Saved drafts/actions exceed the local byte budget.") }
        try data.write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
    func setDraft(_ value: String, key: String) throws {
        guard value.utf8.count <= 64 * 1024 else { throw OxideError(message: "Draft exceeds 64 KiB.") }
        if value.isEmpty { drafts.removeValue(forKey: key) } else { drafts[key] = value }
        try save()
    }
}

// Adapter v1 budgets. Reject oversized input rather than silently dropping rows,
// which would make the next revision's base and history anchor ambiguous.
enum TranscriptLimits {
    static let frameBytes = 384 * 1024
    static let rowBytes = 144 * 1024
    static let textBytes = 24 * 1024
    static let rows = 80
    static let identifierBytes = 4096
    static let keyBytes = 2 * identifierBytes + 1
    static let maxInteger = 9_007_199_254_740_991
}

enum ViewState: String {
    case synchronizing, live, historical, unavailable
}

/// Only request bodies remain polymorphic; the transcript and its state are typed.
struct ViewMetadata {
    let threadId: String
    let title: String
    let projectId: String?
    let model: String
    let mode: String?
    let status: String
    let activeRunId: String?
    let requests: [[String: Any]]
    let hasMore: Bool
    let historyCursor: String?
    let limited: Bool
    let sequence: Int?
    let observedAt: String?
    let state: ViewState
    let live: Bool
    let historical: Bool

    fileprivate var wire: [String: Any] {
        var value: [String: Any] = ["threadId": threadId, "title": title, "model": model, "status": status,
                                    "requests": requests, "hasMore": hasMore, "limited": limited,
                                    "state": state.rawValue, "live": live, "historical": historical]
        value["projectId"] = projectId; value["mode"] = mode; value["activeRunId"] = activeRunId
        value["historyCursor"] = historyCursor; value["sequence"] = sequence; value["observedAt"] = observedAt
        return value
    }

    static func decode(_ data: [String: Any]) throws -> Self {
        try TranscriptWire.checkFrame(data)
        let fields = TranscriptWire(data)
        let rawState = try fields.string("state")
        guard let state = ViewState(rawValue: rawState) else {
            throw OxideError(message: "Unknown transcript state: \(rawState).")
        }
        let live = try fields.boolean("live"), historical = try fields.boolean("historical")
        // A disconnected or synchronizing view may still display a historical page.
        guard live == (state == .live), !(live && historical), state != .historical || historical else {
            throw OxideError(message: "Inconsistent transcript state flags.")
        }
        guard let requests = data["requests"] as? [[String: Any]], requests.count <= 8 else {
            throw OxideError(message: "Invalid transcript requests.")
        }
        return try Self(threadId: fields.identifier("threadId"), title: fields.string("title", limit: 512),
                        projectId: fields.optionalIdentifier("projectId"), model: fields.string("model", limit: 256),
                        mode: fields.optionalString("mode", limit: 256), status: fields.string("status", limit: 256),
                        activeRunId: fields.optionalIdentifier("activeRunId"), requests: requests,
                        hasMore: fields.boolean("hasMore"), historyCursor: fields.optionalString("historyCursor", limit: 64 * 1024),
                        limited: fields.boolean("limited"), sequence: fields.optionalInteger("sequence"),
                        observedAt: fields.optionalString("observedAt", limit: 256), state: state, live: live, historical: historical)
    }
}

struct TranscriptRow: Equatable {
    let key: String
    let sourceThreadId: String
    let itemId: String
    let messageId: String?
    let kind: String
    let text: String
    let truncated: Bool
    let streaming: Bool
    let attachmentCount: Int
    let position: Int?
    // Immutable row fields make this cost reusable across every retained revision.
    // Directly constructed rows are validated once when admitted to the store.
    private var encodedByteCount: Int?

    init(key: String, sourceThreadId: String, itemId: String, messageId: String?, kind: String,
         text: String, truncated: Bool, streaming: Bool, attachmentCount: Int, position: Int?) {
        self.key = key; self.sourceThreadId = sourceThreadId; self.itemId = itemId; self.messageId = messageId
        self.kind = kind; self.text = text; self.truncated = truncated; self.streaming = streaming
        self.attachmentCount = attachmentCount; self.position = position
    }

    static func decode(_ data: [String: Any]) throws -> Self {
        let fields = TranscriptWire(data)
        var row = try Self(key: fields.identifier("key", limit: TranscriptLimits.keyBytes),
                           sourceThreadId: fields.identifier("sourceThreadId"), itemId: fields.identifier("itemId"),
                           messageId: fields.optionalIdentifier("messageId"), kind: fields.string("kind", limit: 256),
                           text: fields.string("text", limit: TranscriptLimits.textBytes), truncated: fields.boolean("truncated"),
                           streaming: fields.boolean("streaming"), attachmentCount: fields.integer("attachmentCount"),
                           position: fields.optionalInteger("position"))
        guard row.key == "\(row.sourceThreadId)/\(row.itemId)" else {
            throw OxideError(message: "Transcript row key does not match its identity.")
        }
        row.encodedByteCount = try TranscriptWire.encodedSize(row.wire)
        return row
    }

    fileprivate func validated() throws -> Self {
        encodedByteCount == nil ? try Self.decode(wire) : self
    }

    fileprivate var validatedByteCount: Int {
        // Only used after validated(); the cache is not exposed to callers.
        encodedByteCount!
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.key == rhs.key && lhs.sourceThreadId == rhs.sourceThreadId && lhs.itemId == rhs.itemId &&
        lhs.messageId == rhs.messageId && lhs.kind == rhs.kind && lhs.text == rhs.text &&
        lhs.truncated == rhs.truncated && lhs.streaming == rhs.streaming &&
        lhs.attachmentCount == rhs.attachmentCount && lhs.position == rhs.position
    }

    fileprivate var wire: [String: Any] {
        var value: [String: Any] = ["key": key, "sourceThreadId": sourceThreadId, "itemId": itemId,
                                    "messageId": messageId as Any? ?? NSNull(), "kind": kind, "text": text,
                                    "truncated": truncated, "streaming": streaming, "attachmentCount": attachmentCount]
        if let position = position { value["position"] = position }
        return value
    }
}

struct ViewSnapshot {
    let revision: Int
    let items: [TranscriptRow]
    let metadata: ViewMetadata

    fileprivate func validated() throws -> Self {
        guard revision >= 0, revision <= TranscriptLimits.maxInteger else {
            throw OxideError(message: "Invalid transcript revision.")
        }
        let checked = try TranscriptWire.checkRows(items)
        _ = try ViewMetadata.decode(metadata.wire)
        var wire = metadata.wire
        wire["revision"] = revision; wire["items"] = []
        // The empty array already contributes its two brackets to the header.
        try TranscriptWire.checkFrameBytes(TranscriptWire.encodedSize(wire) + checked.bytes - 2)
        return Self(revision: revision, items: checked.rows, metadata: metadata)
    }

    static func decode(_ data: [String: Any]) throws -> Self {
        try TranscriptWire.checkFrame(data)
        let fields = TranscriptWire(data)
        let rows = try fields.rows("items")
        _ = try TranscriptWire.checkRows(rows)
        return try Self(revision: fields.integer("revision"), items: rows, metadata: ViewMetadata.decode(data))
    }
}

struct ViewDelta {
    let threadId: String
    let baseRevision: Int
    let revision: Int
    let upserts: [TranscriptRow]
    let removed: [String]
    let order: [String]
    let metadata: ViewMetadata

    static func decode(_ data: [String: Any]) throws -> Self {
        try TranscriptWire.checkFrame(data)
        let fields = TranscriptWire(data)
        guard let metadata = data["metadata"] as? [String: Any], metadata["items"] == nil, metadata["revision"] == nil else {
            throw OxideError(message: "Invalid transcript delta metadata.")
        }
        let delta = try Self(threadId: fields.identifier("threadId"), baseRevision: fields.integer("baseRevision"),
                             revision: fields.integer("revision"), upserts: fields.rows("upserts"),
                             removed: fields.keys("removed"), order: fields.keys("order"), metadata: ViewMetadata.decode(metadata))
        return try delta.validated()
    }

    fileprivate func validated() throws -> Self {
        guard threadId == metadata.threadId, baseRevision >= 0, revision > baseRevision,
              revision <= TranscriptLimits.maxInteger,
              removed.count <= TranscriptLimits.rows, order.count <= TranscriptLimits.rows,
              Set(removed).count == removed.count, Set(order).count == order.count,
              Set(upserts.map(\.key)).isDisjoint(with: removed) else {
            throw OxideError(message: "Invalid transcript delta revision, identity or keys.")
        }
        let checked = try TranscriptWire.checkRows(upserts)
        _ = try ViewMetadata.decode(metadata.wire)
        _ = try TranscriptWire(["removed": removed, "order": order]).keys("removed")
        _ = try TranscriptWire(["removed": removed, "order": order]).keys("order")
        let envelopeBytes = try TranscriptWire.encodedSize(["threadId": threadId, "baseRevision": baseRevision, "revision": revision,
                                       "upserts": [], "removed": removed, "order": order, "metadata": metadata.wire])
        try TranscriptWire.checkFrameBytes(envelopeBytes + checked.bytes - 2)
        return Self(threadId: threadId, baseRevision: baseRevision, revision: revision,
                    upserts: checked.rows, removed: removed, order: order, metadata: metadata)
    }
}

/// Apply on the owning UI queue. Build and validate the entire candidate before
/// publishing it, so a failed delta cannot partially mutate the displayed page.
struct TranscriptStore {
    private(set) var snapshot: ViewSnapshot?
    private(set) var needsResnapshot = false

    mutating func reset() { snapshot = nil; needsResnapshot = false }

    mutating func apply(event: String, data: [String: Any]) throws {
        do {
            switch event {
            case "view": try apply(ViewSnapshot.decode(data))
            case "delta": try apply(ViewDelta.decode(data))
            default: throw OxideError(message: "Unsupported transcript event.")
            }
        } catch { needsResnapshot = true; throw error }
    }

    mutating func apply(_ view: ViewSnapshot) throws {
        do {
            // Full views establish a new baseline, including after helper reconnect.
            snapshot = try view.validated(); needsResnapshot = false
        } catch { needsResnapshot = true; throw error }
    }

    mutating func apply(_ update: ViewDelta) throws {
        do {
            guard !needsResnapshot, let previous = snapshot,
                  previous.metadata.threadId == update.threadId, previous.revision == update.baseRevision else {
                throw OxideError(message: "Transcript revision mismatch; request Latest to resynchronize.")
            }
            let delta = try update.validated()
            var rows = Dictionary(uniqueKeysWithValues: previous.items.map { ($0.key, $0) })
            for key in delta.removed {
                guard rows.removeValue(forKey: key) != nil else {
                    throw OxideError(message: "Delta removes a missing transcript row.")
                }
            }
            for row in delta.upserts { rows[row.key] = row }
            guard Set(delta.order) == Set(rows.keys) else {
                throw OxideError(message: "Delta order must name every retained row exactly once.")
            }
            let items = delta.order.compactMap { rows[$0] }
            let candidate = ViewSnapshot(revision: delta.revision, items: items, metadata: delta.metadata)
            snapshot = try candidate.validated()
        } catch { needsResnapshot = true; throw error }
    }
}

private struct TranscriptWire {
    let data: [String: Any]
    init(_ data: [String: Any]) { self.data = data }

    static func checkFrame(_ data: [String: Any]) throws {
        try checkFrameBytes(encodedSize(data))
    }

    static func encodedSize(_ data: [String: Any]) throws -> Int {
        guard JSONSerialization.isValidJSONObject(data) else {
            throw OxideError(message: "Transcript frame is not JSON.")
        }
        return try JSONSerialization.data(withJSONObject: data, options: [.withoutEscapingSlashes]).count
    }

    static func checkFrameBytes(_ bytes: Int) throws {
        guard bytes <= TranscriptLimits.frameBytes else {
            throw OxideError(message: "Transcript frame exceeds its byte budget or is not JSON.")
        }
    }

    static func checkRows(_ rows: [TranscriptRow]) throws -> (rows: [TranscriptRow], bytes: Int) {
        guard rows.count <= TranscriptLimits.rows, Set(rows.map(\.key)).count == rows.count else {
            throw OxideError(message: "Transcript row count or duplicate keys exceed the contract.")
        }
        let checked = try rows.map { try $0.validated() }
        let bytes = 2 + max(0, checked.count - 1) + checked.reduce(0) { $0 + $1.validatedByteCount }
        guard bytes <= TranscriptLimits.rowBytes else {
            throw OxideError(message: "Transcript rows exceed their byte budget.")
        }
        return (checked, bytes)
    }

    func string(_ key: String, limit: Int = TranscriptLimits.identifierBytes) throws -> String {
        guard let value = data[key] as? String, value.utf8.count <= limit else {
            throw OxideError(message: "Invalid transcript field: \(key).")
        }
        return value
    }
    func optionalString(_ key: String, limit: Int) throws -> String? {
        if data[key] == nil || data[key] is NSNull { return nil }
        return try string(key, limit: limit)
    }
    func identifier(_ key: String, limit: Int = TranscriptLimits.identifierBytes) throws -> String {
        let value = try string(key, limit: limit)
        guard !value.isEmpty, !value.unicodeScalars.contains(where: { $0.value < 32 }) else {
            throw OxideError(message: "Invalid transcript identifier: \(key).")
        }
        return value
    }
    func optionalIdentifier(_ key: String) throws -> String? {
        if data[key] == nil || data[key] is NSNull { return nil }
        return try identifier(key)
    }
    func boolean(_ key: String) throws -> Bool {
        guard let value = data[key] as? NSNumber, CFGetTypeID(value) == CFBooleanGetTypeID() else {
            throw OxideError(message: "Invalid transcript boolean: \(key).")
        }
        return value.boolValue
    }
    func integer(_ key: String) throws -> Int {
        guard let value = data[key] as? NSNumber, CFGetTypeID(value) != CFBooleanGetTypeID(),
              value.doubleValue.isFinite, value.doubleValue >= 0, value.doubleValue <= Double(TranscriptLimits.maxInteger),
              value.doubleValue.rounded(.towardZero) == value.doubleValue else {
            throw OxideError(message: "Invalid transcript integer: \(key).")
        }
        return value.intValue
    }
    func optionalInteger(_ key: String) throws -> Int? {
        if data[key] == nil || data[key] is NSNull { return nil }
        return try integer(key)
    }
    func rows(_ key: String) throws -> [TranscriptRow] {
        guard let values = data[key] as? [[String: Any]], values.count <= TranscriptLimits.rows else {
            throw OxideError(message: "Invalid transcript rows: \(key).")
        }
        return try values.map(TranscriptRow.decode)
    }
    func keys(_ key: String) throws -> [String] {
        guard let values = data[key] as? [String], values.count <= TranscriptLimits.rows else {
            throw OxideError(message: "Invalid transcript keys: \(key).")
        }
        return try values.map { try TranscriptWire(["key": $0]).identifier("key", limit: TranscriptLimits.keyBytes) }
    }
}

/// Bounded line assembly. The wire is NDJSON, not an unbounded read-to-end operation.
struct LineBuffer {
    private(set) var data = Data()
    let limit: Int
    mutating func feed(_ chunk: Data) throws -> [Data] {
        data.append(chunk)
        var lines: [Data] = []
        while let end = data.firstIndex(of: 10) {
            let count = data.distance(from: data.startIndex, to: end)
            guard count <= limit else { throw OxideError(message: "Oversized adapter frame.") }
            let line = Data(data[..<end])
            data.removeSubrange(data.startIndex...end)
            if !line.isEmpty { lines.append(line) }
        }
        guard data.count <= limit else { throw OxideError(message: "Unterminated adapter frame exceeded its limit.") }
        return lines
    }
}
