import Foundation
import Darwin

/// A real subprocess fixture is built into this test executable: no Node/Python or
/// installed adapter is needed, and every test owns only the process it launches.
@main struct TransportTests {
    static let fixtureArgument = "/t3oxide-transport-fixture"

    @MainActor static func main() async throws {
        if CommandLine.arguments.dropFirst().first == fixtureArgument {
            fixture(CommandLine.arguments.last!); return
        }
        try await smallFramesAndSplitLines()
        try await eofWithLiveChild()
        try await finalAckBeforeExit()
        try await reservedStopAndSameID()
        try await closeWhileWriteBlocked()
        try await brokenInputAndDeallocation()
        try await reentrantReconnect()
        try await staleGenerationAndCleanup()
        try await invalidAndBoundedOutput()
        print("Transport subprocess checks passed: immediate/split frames, live-child EOF, final acknowledgement, reserved Stop, same-ID uncertainty/no replay, blocked-write cancellation, stale generations, descriptor/process cleanup, bounded/invalid output.")
    }

    @MainActor static func environment(_ mode: String) -> Environment {
        Environment(id: "fixture", label: "Fixture", environmentId: "fixture", sshDestination: nil,
                    node: URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL.path,
                    adapter: fixtureArgument, hostConfig: "/" + mode, webURL: nil, enabled: true)
    }
    @MainActor static func wait(_ description: String, seconds: Double = 4,
                                until predicate: () -> Bool) async throws {
        let end = Date().addingTimeInterval(seconds)
        while !predicate() && Date() < end { try await Task.sleep(nanoseconds: 10_000_000) }
        precondition(predicate(), "Timed out: \(description)")
    }
    static func alive(_ pid: Int32) -> Bool { Darwin.kill(pid, 0) == 0 }
    @MainActor static func smallFramesAndSplitLines() async throws {
        for mode in ["hello", "split"] {
            let connection = Connection()
            var packets: [[String: Any]] = [], closures = 0
            connection.onPacket = { packets.append($0) }; connection.onClosed = { _ in closures += 1 }
            let start = Date()
            try connection.start(environment(mode))
            try await wait("small hello while producer remains open", seconds: 1) { packets.count == 1 }
            let pid = (packets[0]["pid"] as! NSNumber).int32Value
            precondition(alive(pid) && closures == 0 && Date().timeIntervalSince(start) < 1)
            connection.close()
            try await wait("hello helper exit") { !alive(pid) }
            precondition(closures == 0, "Explicit close must not produce a late onClosed")
        }
    }
    @MainActor static func eofWithLiveChild() async throws {
        let connection = Connection()
        var pid: Int32 = 0, closed = 0, failed = 0
        connection.onPacket = { pid = ($0["pid"] as! NSNumber).int32Value }
        connection.onClosed = { _ in closed += 1 }
        try connection.start(environment("live-eof"))
        try await wait("live-eof hello") { pid != 0 }
        connection.request("send", id: "uncertain") { result in
            if case .failure(let error) = result {
                precondition(error.localizedDescription.contains("acceptance is unknown")); failed += 1
            } else { preconditionFailure("EOF must fail pending action") }
        }
        try await wait("EOF without process termination", seconds: 0.8) { closed == 1 && failed == 1 }
        precondition(alive(pid), "Fixture must still be alive when EOF is delivered")
        precondition(connection.environment == nil)
        try await wait("SIGTERM-resistant helper is killed") { !alive(pid) }
        precondition(closed == 1 && failed == 1)
    }
    @MainActor static func finalAckBeforeExit() async throws {
        let connection = Connection()
        var order: [String] = []
        connection.onClosed = { _ in order.append("closed") }
        try connection.start(environment("final-ack"))
        connection.request("send", id: "final") { result in
            guard case .success(let ack) = result, ack["accepted"] as? Bool == true else {
                preconditionFailure("Final packet was lost before EOF")
            }
            order.append("ack")
        }
        try await wait("final acknowledgement then EOF") { order.count == 2 }
        precondition(order == ["ack", "closed"])
    }
    @MainActor static func reservedStopAndSameID() async throws {
        let connection = Connection()
        var rejected = 0, failures = 0, stop = false, receivedID = ""
        try connection.start(environment("stop"))
        for index in 0..<8 {
            connection.request("watch", id: "normal-\(index)") { result in
                if case .failure = result { failures += 1 }
                else { preconditionFailure("Held request unexpectedly acknowledged") }
            }
        }
        connection.request("latest", id: "overflow") { if case .failure = $0 { rejected += 1 } }
        connection.request("send", id: "normal-0") { if case .failure = $0 { rejected += 1 } }
        connection.request("stop", id: "saved-stop-id") { result in
            guard case .success(let value) = result else { preconditionFailure("Stop was blocked by normal lane") }
            receivedID = value["id"] as? String ?? ""; stop = true
        }
        try await wait("reserved Stop acknowledgement") { stop }
        precondition(rejected == 2 && failures == 0 && receivedID == "saved-stop-id")
        connection.close(); precondition(failures == 8)
        // Reconnect never replays old actions; only an explicit same-ID request is sent.
        try connection.start(environment("echo"))
        var ack = false
        connection.request("send", id: "normal-0") { result in
            guard case .success(let value) = result else { preconditionFailure("Explicit same-ID retry failed") }
            precondition(value["id"] as? String == "normal-0" && value["count"] as? Int == 1)
            ack = true
        }
        try await wait("explicit same-ID retry") { ack }
        connection.close()
    }
    @MainActor static func closeWhileWriteBlocked() async throws {
        let connection = Connection()
        var pid: Int32 = 0, failures = 0
        connection.onPacket = { pid = ($0["pid"] as! NSNumber).int32Value }
        try connection.start(environment("blocked"))
        try await wait("blocked helper ready") { pid != 0 }
        for index in 0..<8 {
            connection.request("send", fields: ["text": String(repeating: "x", count: 90 * 1024)], id: "blocked-\(index)") {
                if case .failure = $0 { failures += 1 }
            }
        }
        connection.request("stop") { if case .failure = $0 { failures += 1 } }
        try await Task.sleep(nanoseconds: 50_000_000)
        precondition(failures == 0, "All eight writes and Stop must be admitted before close")
        let start = Date(); connection.close()
        precondition(Date().timeIntervalSince(start) < 0.1 && failures == 9)
        try await wait("blocked helper cleanup") { !alive(pid) }
    }
    @MainActor static func brokenInputAndDeallocation() async throws {
        for mode in ["broken-input", "blocked"] {
            var connection: Connection? = Connection()
            var pid: Int32 = 0, failed = 0, closed = 0
            connection?.onPacket = { pid = ($0["pid"] as! NSNumber).int32Value }
            connection?.onClosed = { _ in closed += 1 }
            try connection?.start(environment(mode))
            try await wait("cleanup fixture ready") { pid != 0 }
            if mode == "broken-input" {
                connection?.request("send") { if case .failure = $0 { failed += 1 } }
                try await wait("EPIPE reports uncertainty without SIGPIPE") { failed == 1 && closed == 1 }
            }
            connection = nil
            try await wait("deallocation helper cleanup") { !alive(pid) }
        }
    }
    @MainActor static func reentrantReconnect() async throws {
        let connection = Connection()
        var newPacket = false, closures = 0
        connection.onPacket = { if $0["mode"] as? String == "/new" { newPacket = true } }
        connection.onClosed = { _ in closures += 1 }
        try connection.start(environment("old"))
        connection.request("watch") { result in
            guard case .failure = result else { preconditionFailure("Expected old generation cancellation") }
            try! connection.start(environment("new"))
        }
        do {
            try connection.start(environment("superseded"))
            preconditionFailure("Reentrant start must supersede the outer start")
        } catch { precondition(error.localizedDescription.contains("superseded")) }
        try await wait("reentrant connection survives old cleanup") { newPacket }
        precondition(closures == 0)
        connection.close()
    }
    @MainActor static func staleGenerationAndCleanup() async throws {
        // Prior tests have warmed Foundation's process-monitor infrastructure.
        try await Task.sleep(nanoseconds: 2_200_000_000)
        let baseline = descriptorCount()
        let connection = Connection()
        var oldPackets = 0, newPackets = 0, closed = 0, pids: [Int32] = []
        connection.onPacket = {
            if $0["mode"] as? String == "/old" { oldPackets += 1 } else { newPackets += 1 }
            pids.append(($0["pid"] as! NSNumber).int32Value)
        }
        connection.onClosed = { _ in closed += 1 }
        for _ in 0..<12 {
            try connection.start(environment("old"))
            // Deliberately stall the main consumer so old delivery blocks are queued.
            usleep(40_000)
            connection.close()
            try connection.start(environment("new"))
            let expected = newPackets + 1
            try await wait("new generation hello") { newPackets == expected }
            connection.close()
        }
        try await Task.sleep(nanoseconds: 2_200_000_000)
        precondition(oldPackets == 0 && closed == 0, "Old generation delivery escaped cancellation")
        precondition(pids.allSatisfy { !alive($0) }, "Owned helper survived cancellation")
        precondition(descriptorCount() <= baseline, "Pipe descriptor leak: \(baseline) -> \(descriptorCount())")
        // A failed launch must also close all six pipe ends.
        let invalid = Environment(id: "fixture", label: "Missing", environmentId: "fixture", sshDestination: nil,
                                  node: "/does-not-exist/t3oxide", adapter: fixtureArgument, hostConfig: "/none", webURL: nil, enabled: true)
        for _ in 0..<8 {
            do { try connection.start(invalid); preconditionFailure("Expected launch failure") } catch {}
        }
        precondition(descriptorCount() <= baseline, "Failed launch leaked descriptors")
    }
    static func descriptorCount() -> Int { (0..<1024).filter { fcntl(Int32($0), F_GETFD) != -1 }.count }
    @MainActor static func invalidAndBoundedOutput() async throws {
        for mode in ["invalid", "incomplete", "oversize", "flood", "stderr"] {
            let connection = Connection()
            var closed = 0, packets = 0
            connection.onClosed = { _ in closed += 1 }
            connection.onPacket = { _ in packets += 1 }
            try connection.start(environment(mode))
            if mode == "flood" { usleep(300_000) }
            if mode == "stderr" {
                try await wait("stderr drained without blocking stdout") { packets == 1 }
                connection.close()
            } else {
                try await wait("bounded/invalid \(mode) disconnect") { closed == 1 }
                precondition(connection.environment == nil)
                if mode == "flood" { precondition(packets <= 1024) }
                else { precondition(packets == 0) }
            }
        }
    }

    static func emit(_ object: [String: Any]) {
        var data = try! JSONSerialization.data(withJSONObject: object); data.append(10)
        writeAll(data, fd: STDOUT_FILENO)
    }
    static func writeAll(_ data: Data, fd: Int32) {
        data.withUnsafeBytes { bytes in
            var offset = 0
            while offset < bytes.count {
                let count = Darwin.write(fd, bytes.baseAddress!.advanced(by: offset), bytes.count - offset)
                if count < 0 && errno == EINTR { continue }
                if count <= 0 { return }; offset += count
            }
        }
    }
    static func fixture(_ mode: String) {
        let hello: [String: Any] = ["v": 1, "environmentId": "fixture", "event": "hello", "pid": getpid(), "mode": mode]
        if ["/blocked", "/live-eof"].contains(mode) { signal(SIGTERM, SIG_IGN) }
        if mode == "/broken-input" { Darwin.close(STDIN_FILENO) }
        switch mode {
        case "/split":
            var data = try! JSONSerialization.data(withJSONObject: hello); data.append(10)
            let midpoint = data.count / 2
            writeAll(Data(data.prefix(midpoint)), fd: STDOUT_FILENO); usleep(30_000)
            writeAll(Data(data.dropFirst(midpoint)), fd: STDOUT_FILENO)
        case "/incomplete": writeAll(Data("{\"v\":".utf8), fd: STDOUT_FILENO); return
        case "/invalid": emit(["v": 1, "environmentId": "wrong", "event": "hello"])
        case "/oversize": writeAll(Data(repeating: 65, count: 400 * 1024), fd: STDOUT_FILENO)
        case "/flood":
            let line = try! JSONSerialization.data(withJSONObject: hello) + Data([10])
            writeAll((0..<3000).reduce(into: Data()) { data, _ in data.append(line) }, fd: STDOUT_FILENO)
        case "/stderr": writeAll(Data(repeating: 69, count: 512 * 1024), fd: STDERR_FILENO); emit(hello)
        default: emit(hello)
        }
        if mode == "/blocked" || mode == "/broken-input" { while true { pause() } }
        var lines = LineBuffer(limit: 128 * 1024), buffer = [UInt8](repeating: 0, count: 4096), requests = 0
        while true {
            let count = Darwin.read(STDIN_FILENO, &buffer, buffer.count)
            if count <= 0 { return }
            for line in try! lines.feed(Data(buffer.prefix(count))) {
                let request = try! JSONSerialization.jsonObject(with: line) as! [String: Any]
                requests += 1
                if mode == "/live-eof" { Darwin.close(STDOUT_FILENO); while true { pause() } }
                if mode == "/echo" || mode == "/final-ack" || (mode == "/stop" && request["op"] as? String == "stop") {
                    emit(["v": 1, "environmentId": "fixture", "id": request["id"]!,
                          "result": ["accepted": true, "id": request["id"]!, "count": requests]])
                    if mode == "/final-ack" { return }
                }
            }
        }
    }
}
