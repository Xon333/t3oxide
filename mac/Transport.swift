import Foundation
import Darwin

/// A single process/channel owns one environment. No T3 credentials cross this boundary.
@MainActor final class Connection {
    private var channel: PipeChannel?
    private var generation = UUID()
    private(set) var environment: Environment?
    var onPacket: (([String: Any]) -> Void)?
    var onClosed: ((String) -> Void)?
    private struct Pending {
        let isStop: Bool
        let completion: (Result<[String: Any], Error>) -> Void
    }
    private var pending: [String: Pending] = [:]
    private var deadlines: [String: DispatchWorkItem] = [:]

    func start(_ environment: Environment) throws {
        let epoch = closeCurrent()
        guard generation == epoch else { throw OxideError(message: "Connection start was superseded by a newer connection.") }
        try environment.validate()
        let channel = try PipeChannel(environment: environment, packet: { [weak self] packet in
            guard let self = self, self.generation == epoch else { return }
            self.receive(packet)
        }, closed: { [weak self] message in
            guard let self = self, self.generation == epoch else { return }
            self.disconnected(message)
        })
        self.environment = environment
        self.channel = channel
    }

    func request(_ op: String, fields: [String: Any] = [:], id: String = UUID().uuidString,
                 completion: @escaping (Result<[String: Any], Error>) -> Void) {
        guard let env = environment, let channel = channel else {
            completion(.failure(OxideError(message: "Not connected."))); return
        }
        var packet = fields
        packet["v"] = 1; packet["id"] = id; packet["op"] = op; packet["environmentId"] = env.environmentId
        do {
            var data = try JSONSerialization.data(withJSONObject: packet)
            let isStop = op == "stop"
            let laneCount = pending.values.filter { $0.isStop == isStop }.count
            guard data.count <= 96 * 1024, laneCount < (isStop ? 1 : 8), pending[id] == nil else {
                throw OxideError(message: "Request too large or another operation is still pending.")
            }
            data.append(10)
            // Reserve before queuing: even timed-out writes cannot grow an unbounded backlog.
            guard channel.reserveWrite(isStop: isStop) else {
                throw OxideError(message: "Connection write queue is full; request was not sent.")
            }
            pending[id] = Pending(isStop: isStop, completion: completion)
            let epoch = generation
            let timeout = DispatchWorkItem { [weak self] in
                guard let self = self, self.generation == epoch else { return }
                self.deadlines.removeValue(forKey: id)
                self.pending.removeValue(forKey: id)?.completion(.failure(OxideError(message: "No acknowledgement; operation may have completed. Retry only the saved operation ID.")))
            }
            deadlines[id] = timeout
            DispatchQueue.main.asyncAfter(deadline: .now() + 50, execute: timeout)
            channel.send(data, isStop: isStop)
        } catch { completion(.failure(error)) }
    }

    private func receive(_ packet: [String: Any]) {
        if let id = packet["id"] as? String, let callback = pending.removeValue(forKey: id)?.completion {
            deadlines.removeValue(forKey: id)?.cancel()
            if let error = packet["error"] as? [String: Any] {
                // Preserve the structured definite/uncertain distinction for mutation handling.
                callback(.success(["error": error]))
            } else { callback(.success(packet["result"] as? [String: Any] ?? [:])) }
        } else if packet["event"] != nil { onPacket?(packet) }
    }

    private func disconnected(_ message: String) {
        let callbacks = detach()
        let epoch = generation
        for callback in callbacks { callback(.failure(OxideError(message: "Connection closed; operation acceptance is unknown. Retry only the saved operation ID."))) }
        // A completion is allowed to reconnect. Do not close that new UI generation.
        if generation == epoch { onClosed?(message) }
    }

    private func detach() -> [(Result<[String: Any], Error>) -> Void] {
        generation = UUID()
        channel?.cancel(); channel = nil; environment = nil
        let callbacks = pending.values.map(\.completion); pending.removeAll()
        deadlines.values.forEach { $0.cancel() }; deadlines.removeAll()
        return callbacks
    }

    func close() { _ = closeCurrent() }

    private func closeCurrent() -> UUID {
        let callbacks = detach()
        let epoch = generation
        for callback in callbacks {
            callback(.failure(OxideError(message: "Connection closed. Unacknowledged operations stay saved and are not replayed automatically.")))
        }
        return epoch
    }

    deinit { channel?.cancel() }
}

/// All descriptor access and source cancellation run on one queue. POSIX nonblocking
/// read returns the bytes available now, unlike Foundation's filling read(upToCount:).
private final class PipeChannel: @unchecked Sendable {
    private let queue = DispatchQueue(label: "t3oxide.pipe", qos: .userInitiated)
    private let child: Process
    private let input: FileHandle
    private let output: FileHandle
    private let errors: FileHandle
    private let packet: @MainActor ([String: Any]) -> Void
    private let environmentID: String
    private let closed: @MainActor (String) -> Void
    private var reader: DispatchSourceRead?
    private var errorReader: DispatchSourceRead?
    private var writer: DispatchSourceWrite?
    private var writerActive = false
    private var stopped = false
    private var lines = LineBuffer(limit: 384 * 1024)
    private let delivery = DeliveryBudget()
    private let writeLock = NSLock()
    private var ordinaryWrites = 0
    private var stopWrites = 0
    private struct Frame {
        let data: Data
        let isStop: Bool
        var offset = 0
    }
    private var writes: [Frame] = []

    init(environment: Environment, packet: @escaping @MainActor ([String: Any]) -> Void,
         closed: @escaping @MainActor (String) -> Void) throws {
        self.packet = packet; self.closed = closed; self.environmentID = environment.environmentId
        child = Process()
        let stdin = Pipe(), stdout = Pipe(), stderr = Pipe()
        input = stdin.fileHandleForWriting; output = stdout.fileHandleForReading; errors = stderr.fileHandleForReading
        child.executableURL = URL(fileURLWithPath: environment.sshDestination == nil ? environment.node : "/usr/bin/ssh")
        child.arguments = environment.arguments
        child.standardInput = stdin; child.standardOutput = stdout; child.standardError = stderr
        do {
            for handle in [input, output, errors] {
                let fd = handle.fileDescriptor
                let flags = fcntl(fd, F_GETFL)
                guard flags >= 0, fcntl(fd, F_SETFL, flags | O_NONBLOCK) >= 0 else {
                    throw OxideError(message: "Could not configure the adapter pipe.")
                }
            }
            // A disappearing stdin must report EPIPE instead of terminating the app.
            guard fcntl(input.fileDescriptor, F_SETNOSIGPIPE, 1) >= 0 else {
                throw OxideError(message: "Could not configure the adapter input.")
            }
            try child.run()
        } catch {
            for handle in [input, output, errors, stdin.fileHandleForReading, stdout.fileHandleForWriting, stderr.fileHandleForWriting] { try? handle.close() }
            throw error
        }
        // Parent copies of the child's ends would otherwise hide EOF while it is alive.
        try? stdin.fileHandleForReading.close()
        try? stdout.fileHandleForWriting.close()
        try? stderr.fileHandleForWriting.close()
        queue.async { self.installSources() }
    }

    func reserveWrite(isStop: Bool) -> Bool {
        writeLock.lock(); defer { writeLock.unlock() }
        if isStop {
            guard stopWrites < 1 else { return false }; stopWrites += 1
        } else {
            guard ordinaryWrites < 8 else { return false }; ordinaryWrites += 1
        }
        return true
    }

    private func releaseWrite(isStop: Bool) {
        writeLock.lock(); defer { writeLock.unlock() }
        if isStop { stopWrites -= 1 } else { ordinaryWrites -= 1 }
    }

    func send(_ data: Data, isStop: Bool) {
        queue.async {
            guard !self.stopped else { self.releaseWrite(isStop: isStop); return }
            let frame = Frame(data: data, isStop: isStop)
            // Never interleave a partially written NDJSON frame. Stop goes before
            // ordinary frames that have not started, in its own reserved slot.
            if isStop { self.writes.insert(frame, at: (self.writes.first?.offset ?? 0) > 0 ? 1 : 0) }
            else { self.writes.append(frame) }
            if !self.writerActive { self.writerActive = true; self.writer?.resume() }
        }
    }

    func cancel() { queue.async { self.finish(nil) } }

    private func installSources() {
        let reader = DispatchSource.makeReadSource(fileDescriptor: output.fileDescriptor, queue: queue)
        reader.setEventHandler { [weak self] in self?.readOutput() }
        reader.setCancelHandler { [output] in try? output.close() }
        self.reader = reader
        let errorReader = DispatchSource.makeReadSource(fileDescriptor: errors.fileDescriptor, queue: queue)
        errorReader.setEventHandler { [weak self] in self?.drainErrors() }
        errorReader.setCancelHandler { [errors] in try? errors.close() }
        self.errorReader = errorReader
        let writer = DispatchSource.makeWriteSource(fileDescriptor: input.fileDescriptor, queue: queue)
        writer.setEventHandler { [weak self] in self?.writeInput() }
        writer.setCancelHandler { [input] in try? input.close() }
        self.writer = writer
        reader.resume(); errorReader.resume()
    }

    private func readOutput() {
        guard !stopped else { return }
        var buffer = [UInt8](repeating: 0, count: 32 * 1024)
        // Bound each turn so a busy producer cannot starve writes/cancellation.
        for _ in 0..<8 {
            let count = Darwin.read(output.fileDescriptor, &buffer, buffer.count)
            if count > 0 {
                do {
                    for line in try lines.feed(Data(buffer.prefix(count))) {
                        guard let decoded = (try? JSONSerialization.jsonObject(with: line)) as? [String: Any],
                              decoded["v"] as? Int == 1,
                              decoded["environmentId"] as? String == environmentID else {
                            finish("Adapter protocol or environment identity does not match."); return
                        }
                        try delivery.add(line.count)
                        DispatchQueue.main.async { [packet, delivery] in
                            defer { delivery.remove(line.count) }
                            packet(decoded)
                        }
                    }
                } catch { finish("Adapter output was invalid or exceeded its bounded queue."); return }
            } else if count == 0 {
                finish(lines.data.isEmpty ? "Host connection ended. Reconnect uses existing SSH access." : "Adapter output ended with an incomplete frame.")
                return
            } else if errno == EINTR { continue }
            else if errno == EAGAIN || errno == EWOULDBLOCK { return }
            else { finish("Could not read adapter output."); return }
        }
    }

    private func drainErrors() {
        guard !stopped else { return }
        var buffer = [UInt8](repeating: 0, count: 32 * 1024)
        for _ in 0..<8 {
            let count = Darwin.read(errors.fileDescriptor, &buffer, buffer.count)
            if count > 0 { continue }
            if count < 0 && errno == EINTR { continue }
            if count == 0 || (errno != EAGAIN && errno != EWOULDBLOCK) {
                errorReader?.cancel(); errorReader = nil
            }
            return
        }
    }

    private func writeInput() {
        guard !stopped else { return }
        for _ in 0..<8 {
            guard let frame = writes.first else {
                writer?.suspend(); writerActive = false; return
            }
            let count = frame.data.withUnsafeBytes { bytes in
                Darwin.write(input.fileDescriptor, bytes.baseAddress!.advanced(by: frame.offset), frame.data.count - frame.offset)
            }
            if count > 0 {
                writes[0].offset += count
                if writes[0].offset == frame.data.count { writes.removeFirst(); releaseWrite(isStop: frame.isStop) }
            } else if count < 0 && errno == EINTR { continue }
            else if count < 0 && (errno == EAGAIN || errno == EWOULDBLOCK) { return }
            else { finish("Connection input closed; operation acceptance is unknown."); return }
        }
    }

    private func finish(_ message: String?) {
        guard !stopped else { return }
        stopped = true
        reader?.cancel(); reader = nil
        errorReader?.cancel(); errorReader = nil
        writer?.cancel()
        if !writerActive { writer?.resume() }
        writer = nil
        for frame in writes { releaseWrite(isStop: frame.isStop) }
        writes.removeAll()
        // Cancellation handlers close descriptors after their last I/O handler.
        // Let stdin EOF stop the helper, then escalate only this owned process.
        let child = child
        queue.asyncAfter(deadline: .now() + 1) {
            if child.isRunning { child.terminate() }
        }
        queue.asyncAfter(deadline: .now() + 2) {
            if child.isRunning { _ = Darwin.kill(child.processIdentifier, SIGKILL) }
        }
        if let message = message { DispatchQueue.main.async { [closed] in closed(message) } }
    }
}

private final class DeliveryBudget: @unchecked Sendable {
    private let lock = NSLock()
    private var bytes = 0
    private var frames = 0
    func add(_ n: Int) throws {
        lock.lock(); defer { lock.unlock() }
        guard bytes + n <= 2 * 1024 * 1024, frames < 1024 else { throw OxideError(message: "Consumer stalled.") }
        bytes += n; frames += 1
    }
    func remove(_ n: Int) { lock.lock(); bytes -= n; frames -= 1; lock.unlock() }
}
