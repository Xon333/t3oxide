import Foundation

@main struct ModelTests {
    static func main() throws {
        var lines = LineBuffer(limit: 12)
        let first = try lines.feed(Data("{\"a\":".utf8)); precondition(first.isEmpty)
        let second = try lines.feed(Data("1}\n{}\n".utf8)); precondition(second.count == 2)
        do { _ = try lines.feed(Data(repeating: 65, count: 13)); fatalError("Missing bound") } catch {}
        precondition(shellQuote("a'b") == "'a'\\''b'")
        let env = Environment(id: "remote", label: "Remote", environmentId: "fixture", sshDestination: "remote-fixture", node: "/opt/homebrew/bin/node", adapter: "/tmp/a b/main.mjs", hostConfig: "/tmp/host.json", webURL: "https://example.invalid", enabled: true)
        try env.validate()
        precondition(env.arguments.contains("BatchMode=yes")); precondition(env.arguments.last!.contains("'/tmp/a b/main.mjs'"))
        try transcriptTransitions()
        try transcriptRejections()
        try transcriptBounds()
        try incrementalByteBudgets()
        try stopPersistence()
        print("Model checks passed: transport input, typed views/deltas, atomic rejection, state/history/order, bounds, Stop persistence")
    }

    static func row(_ id: String, text: String = "A streamed reply", position: Int? = 17) -> [String: Any] {
        var value: [String: Any] = ["key": "thread-a/\(id)", "sourceThreadId": "thread-a", "itemId": id,
                                    "messageId": NSNull(), "kind": "assistant_message", "text": text,
                                    "truncated": false, "streaming": true, "attachmentCount": 0]
        value["position"] = position
        return value
    }

    static func metadata(_ state: String = "live", historical: Bool = false) -> [String: Any] {
        ["threadId": "thread-a", "title": "Native integration", "projectId": "project-a", "model": "gpt-6-astra",
         "mode": "full-access", "status": "running", "activeRunId": "run-a",
         "requests": [["id": "request-a", "revision": "abc", "kind": "approval_request", "prompt": "Run checks?",
                        "supported": true, "options": [["decision": "accept", "label": "Approve once"]], "questions": []]],
         "hasMore": true, "historyCursor": "opaque:/cursor?with=padding==", "limited": false, "sequence": 51,
         "observedAt": "2026-10-13T10:00:00.000Z", "state": state, "live": state == "live",
         "historical": historical || state == "historical"]
    }

    static func view(_ rows: [[String: Any]] = [row("a"), row("b")], revision: Int = 10,
                     state: String = "live") -> [String: Any] {
        var value = metadata(state)
        value["items"] = rows; value["revision"] = revision
        return value
    }

    static func delta(upserts: [[String: Any]] = [], removed: [String] = [], order: [String] = ["thread-a/a", "thread-a/b"],
                      base: Int = 10, revision: Int = 11, meta: [String: Any] = metadata()) -> [String: Any] {
        ["threadId": "thread-a", "baseRevision": base, "revision": revision, "upserts": upserts,
         "removed": removed, "order": order, "metadata": meta]
    }

    // Roundtrip through the same Foundation JSON boundary used by Transport.
    static func packet(_ value: [String: Any]) throws -> [String: Any] {
        let data = try JSONSerialization.data(withJSONObject: value)
        return try JSONSerialization.jsonObject(with: data) as! [String: Any]
    }

    static func expectFailure(_ body: () throws -> Void) {
        do { try body(); fatalError("Expected rejected input") } catch {}
    }

    static func transcriptTransitions() throws {
        var store = TranscriptStore()
        try store.apply(event: "view", data: packet(view(state: "synchronizing")))
        precondition(store.snapshot?.metadata.state == .synchronizing && !store.needsResnapshot)
        try store.apply(event: "delta", data: packet(delta(upserts: [row("b", text: "Complete replacement")],
                                                          order: ["thread-a/b", "thread-a/a"])))
        precondition(store.snapshot?.revision == 11 && store.snapshot?.metadata.state == .live)
        precondition(store.snapshot?.items.map(\.itemId) == ["b", "a"])
        precondition(store.snapshot?.items.first?.text == "Complete replacement")
        precondition(store.snapshot?.items.first?.position == 17)
        precondition(store.snapshot?.metadata.requests.first?["kind"] as? String == "approval_request")

        var history = metadata("historical")
        for key in ["activeRunId", "projectId", "mode", "historyCursor", "sequence", "observedAt"] { history.removeValue(forKey: key) }
        history["requests"] = []; history["hasMore"] = false
        try store.apply(event: "delta", data: packet(delta(upserts: [row("older", position: nil)],
                    removed: ["thread-a/a", "thread-a/b"], order: ["thread-a/older"], base: 11, revision: 12, meta: history)))
        let historical = store.snapshot!.metadata
        precondition(historical.state == .historical && historical.historical && !historical.live)
        precondition(historical.activeRunId == nil && historical.projectId == nil && historical.mode == nil)
        precondition(historical.historyCursor == nil && historical.sequence == nil && historical.observedAt == nil)
        precondition(historical.requests.isEmpty && store.snapshot?.items.first?.position == nil)

        // Disconnecting or fetching another page preserves the fact that visible rows are historical.
        for (index, state) in ["unavailable", "synchronizing", "historical"].enumerated() {
            try store.apply(event: "delta", data: packet(delta(order: ["thread-a/older"], base: 12 + index,
                           revision: 13 + index, meta: metadata(state, historical: true))))
            precondition(store.snapshot?.metadata.state.rawValue == state)
            precondition(store.snapshot?.items.first?.itemId == "older" && store.snapshot!.metadata.historical)
        }
        // Full view is the watch/Latest/reconnect baseline, even after helper revisions reset.
        try store.apply(ViewSnapshot.decode(packet(view(revision: 1, state: "synchronizing"))))
        precondition(store.snapshot?.items.count == 2 && !store.snapshot!.metadata.historical)
        precondition(store.snapshot?.metadata.historyCursor == "opaque:/cursor?with=padding==")
        try store.apply(ViewDelta.decode(packet(delta(base: 1, revision: 4)))) // Monotonic need not be consecutive.
        precondition(store.snapshot?.revision == 4 && store.snapshot?.metadata.state == .live)
        store.reset(); precondition(store.snapshot == nil && !store.needsResnapshot)
    }

    static func rejected(_ input: [String: Any], event: String = "delta") throws {
        var store = TranscriptStore()
        try store.apply(event: "view", data: packet(view()))
        let previous = store.snapshot!
        expectFailure { try store.apply(event: event, data: packet(input)) }
        precondition(store.needsResnapshot)
        precondition(store.snapshot?.revision == previous.revision && store.snapshot?.items == previous.items)
        precondition(store.snapshot?.metadata.activeRunId == previous.metadata.activeRunId)
        // A plausible later delta must not recover a view whose continuity was lost.
        expectFailure { try store.apply(event: "delta", data: packet(delta())) }
        precondition(store.snapshot?.revision == previous.revision)
        try store.apply(event: "view", data: packet(view(revision: 20)))
        precondition(!store.needsResnapshot && store.snapshot?.revision == 20)
    }

    static func transcriptRejections() throws {
        var empty = TranscriptStore()
        expectFailure { try empty.apply(event: "delta", data: delta()) }
        precondition(empty.snapshot == nil && empty.needsResnapshot)
        try rejected(delta(base: 9))
        try rejected(delta(revision: 10))
        try rejected(delta(revision: 9))
        try rejected(delta(order: ["thread-a/a"]))
        try rejected(delta(order: ["thread-a/a", "thread-a/a"]))
        try rejected(delta(order: ["thread-a/a", "missing"]))
        try rejected(delta(removed: ["missing"]))
        try rejected(delta(removed: ["thread-a/a", "thread-a/a"], order: ["thread-a/b"]))
        try rejected(delta(upserts: [row("a")], removed: ["thread-a/a"]))
        try rejected(delta(upserts: [row("a"), row("a")]))
        var incomplete = row("a"); incomplete.removeValue(forKey: "text")
        try rejected(delta(upserts: [incomplete]))
        var wrongKey = row("a"); wrongKey["key"] = "thread-a/wrong"
        try rejected(delta(upserts: [wrongKey]))
        var changedThread = metadata(); changedThread["threadId"] = "thread-b"
        try rejected(delta(meta: changedThread))
        var wrongEnvelope = delta(); wrongEnvelope["threadId"] = "thread-b"
        try rejected(wrongEnvelope)
        for state in ["connected", "unknown", ""] { try rejected(delta(meta: metadata(state))) }
        var flags = metadata(); flags["live"] = false
        try rejected(delta(meta: flags))
        flags = metadata(); flags["historical"] = true
        try rejected(delta(meta: flags))
        flags = metadata("historical"); flags["historical"] = false
        try rejected(delta(meta: flags))
        var nested = metadata(); nested["items"] = []
        try rejected(delta(meta: nested))
        var invalid = delta(); invalid["baseRevision"] = true
        try rejected(invalid)
        invalid = delta(); invalid["revision"] = 11.5
        try rejected(invalid)
        invalid = delta(); invalid["revision"] = 9_007_199_254_740_992 as Int
        try rejected(invalid)
        flags = metadata(); flags["live"] = 1
        try rejected(delta(meta: flags))
        try rejected(view([row("a"), row("a")]), event: "view")
        try rejected(view(state: "unknown"), event: "view")
    }

    static func transcriptBounds() throws {
        let longID = String(repeating: "i", count: 285)
        let maxID = String(repeating: "t", count: 4096)
        var largestIdentity = row(maxID)
        largestIdentity["sourceThreadId"] = maxID; largestIdentity["key"] = "\(maxID)/\(maxID)"
        let snapshot = try ViewSnapshot.decode(packet(view([row(longID), largestIdentity, row("without-position", position: nil)])))
        precondition(snapshot.items[0].itemId == longID && snapshot.items[1].key.utf8.count == 8193)
        let text = String(repeating: "é", count: 12 * 1024)
        _ = try ViewSnapshot.decode(packet(view([row("utf8-bound", text: text)])))
        try rejected(delta(upserts: [row("a", text: text + "é")]))
        try rejected(delta(upserts: [row(String(repeating: "i", count: 4097))]))
        var negative = row("a"); negative["position"] = -1
        try rejected(delta(upserts: [negative]))
        negative = row("a"); negative["attachmentCount"] = -1
        try rejected(delta(upserts: [negative]))
        var many = view((0..<80).map { row("r\($0)", text: "") })
        _ = try ViewSnapshot.decode(packet(many))
        many["items"] = (0..<81).map { row("r\($0)", text: "") }
        try rejected(many, event: "view")
        try rejected(view((0..<7).map { row("r\($0)", text: String(repeating: "x", count: 24 * 1024)) }), event: "view")
        // Each upsert fits, but accumulated retained rows would exceed the budget.
        var store = TranscriptStore()
        let initial = (0..<5).map { row("r\($0)", text: String(repeating: "x", count: 24 * 1024)) }
        try store.apply(event: "view", data: packet(view(initial)))
        expectFailure {
            try store.apply(event: "delta", data: packet(delta(upserts: [row("extra", text: String(repeating: "y", count: 24 * 1024))],
                         order: (0..<5).map { "thread-a/r\($0)" } + ["thread-a/extra"])))
        }
        precondition(store.needsResnapshot && store.snapshot?.items.count == 5 && store.snapshot?.revision == 10)
        var huge = view(); huge["ignored"] = String(repeating: "x", count: 384 * 1024)
        try rejected(huge, event: "view")
        var requests = metadata(); requests["requests"] = (0..<9).map { ["id": "q\($0)"] }
        try rejected(delta(meta: requests))
        // Escaped text counts against the serialized row budget, not just UTF-8 text size.
        try rejected(view([row("escaped", text: String(repeating: "\u{0001}", count: 24 * 1024))]), event: "view")
    }

    static func stopPersistence() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("t3oxide-model-\(UUID().uuidString)")
        let url = directory.appendingPathComponent("drafts.json")
        defer { try? FileManager.default.removeItem(at: directory) }
        let state = LocalState(url: url)
        for index in 0..<8 {
            state.pending["env\(index)"] = ["id": "send\(index)", "threadId": "thread-a"]
            state.pendingStops["env\(index)"] = ["id": "stop\(index)", "threadId": "thread-a", "runId": "run-a"]
        }
        state.drafts["env0/thread-a"] = "Keep my draft"
        try state.save()
        let reloaded = LocalState(url: url)
        precondition(reloaded.loadError == nil && reloaded.pending.count == 8 && reloaded.pendingStops.count == 8)
        precondition(reloaded.pendingStops["env0"]?["runId"] as? String == "run-a")
        precondition(reloaded.drafts["env0/thread-a"] == "Keep my draft")
        state.pendingStops["ninth"] = ["id": "extra"]
        expectFailure { try state.save() }
        state.pendingStops.removeValue(forKey: "ninth")
        state.pending["ninth"] = ["id": "extra"]
        expectFailure { try state.save() }
        state.pending.removeValue(forKey: "ninth")
        state.pending["env0"] = ["large": String(repeating: "x", count: 1100 * 1024)]
        state.pendingStops["env0"] = ["large": String(repeating: "x", count: 1100 * 1024)]
        expectFailure { try state.save() }
        precondition(LocalState(url: url).pendingStops["env0"]?["runId"] as? String == "run-a")
        // Existing installations have no pendingStops field.
        try JSONSerialization.data(withJSONObject: ["drafts": ["a": "draft"], "pending": ["env": ["id": "old-send"]]]).write(to: url)
        let legacy = LocalState(url: url)
        precondition(legacy.loadError == nil && legacy.pendingStops.isEmpty && legacy.pending.count == 1)
        try JSONSerialization.data(withJSONObject: ["drafts": [:], "pending": [:], "pendingStops": "corrupt"]).write(to: url)
        precondition(LocalState(url: url).loadError != nil)
    }

    static func incrementalByteBudgets() throws {
        // Fill exactly the serialized array budget, including braces, commas,
        // Unicode, escaped controls, and optional position metadata.
        var rows = (0..<5).map { row("r\($0)", text: String(repeating: "é", count: 12 * 1024)) }
        rows.append(row("last", text: "\u{0001}\"\\", position: nil))
        let size = try JSONSerialization.data(withJSONObject: rows, options: [.withoutEscapingSlashes]).count
        let remaining = TranscriptLimits.rowBytes - size
        let lastText = rows[5]["text"] as! String
        rows[5]["text"] = lastText + String(repeating: "x", count: remaining)
        let exact = try JSONSerialization.data(withJSONObject: rows, options: [.withoutEscapingSlashes]).count
        precondition(exact == TranscriptLimits.rowBytes)
        let order = rows.map { $0["key"] as! String }
        var store = TranscriptStore()
        try store.apply(event: "view", data: packet(view(rows)))
        // A metadata-only revision preserves every cached row at the exact limit.
        try store.apply(event: "delta", data: packet(delta(order: order)))
        precondition(store.snapshot?.revision == 11)
        var larger = rows[5]; larger["text"] = (larger["text"] as! String) + "x"
        expectFailure {
            try store.apply(event: "delta", data: packet(delta(upserts: [larger], order: order, base: 11, revision: 12)))
        }
        precondition(store.needsResnapshot && store.snapshot?.revision == 11 && store.snapshot?.items.count == 6)

        try store.apply(event: "view", data: packet(view(rows, revision: 20)))
        var smaller = rows[0]
        smaller["text"] = String((smaller["text"] as! String).dropLast()) // Frees two UTF-8 bytes.
        try store.apply(event: "delta", data: packet(delta(upserts: [smaller, larger], order: order, base: 20, revision: 21)))
        precondition(store.snapshot?.revision == 21 && store.snapshot?.items.last?.text == larger["text"] as? String)
        // Removing a retained row releases its whole cached cost before insertion.
        let replacement = row("n1", text: String(repeating: "x", count: 24 * 1024))
        try store.apply(event: "delta", data: packet(delta(upserts: [replacement], removed: [order[1]],
                     order: order.filter { $0 != order[1] } + ["thread-a/n1"], base: 21, revision: 22)))
        precondition(store.snapshot?.revision == 22 && store.snapshot?.items.last?.itemId == "n1")

        // A small delta can introduce metadata which overflows the full candidate
        // frame once retained rows are included; reject without re-encoding them.
        var largeMetadata = metadata()
        largeMetadata["requests"] = [["prompt": String(repeating: "p", count: 250 * 1024)]]
        let currentOrder = store.snapshot!.items.map(\.key)
        expectFailure {
            try store.apply(event: "delta", data: packet(delta(order: currentOrder, base: 22, revision: 23, meta: largeMetadata)))
        }
        precondition(store.needsResnapshot && store.snapshot?.revision == 22)

        // Existing memberwise construction remains supported, but cannot bypass
        // row validation or poison the cached costs used by later revisions.
        let direct = TranscriptRow(key: "thread-a/direct", sourceThreadId: "thread-a", itemId: "direct", messageId: nil,
                                   kind: "assistant_message", text: "manual", truncated: false, streaming: false,
                                   attachmentCount: 0, position: nil)
        let meta = try ViewMetadata.decode(metadata())
        try store.apply(ViewSnapshot(revision: 30, items: [direct], metadata: meta))
        precondition(store.snapshot?.items == [direct])
        try store.apply(event: "delta", data: packet(delta(order: [direct.key], base: 30, revision: 31)))
        let invalid = TranscriptRow(key: "thread-a/wrong", sourceThreadId: "thread-a", itemId: "direct", messageId: nil,
                                    kind: "assistant_message", text: "manual", truncated: false, streaming: false,
                                    attachmentCount: 0, position: nil)
        expectFailure { try store.apply(ViewSnapshot(revision: 32, items: [invalid], metadata: meta)) }
        precondition(store.snapshot?.revision == 31 && store.needsResnapshot)
    }
}
