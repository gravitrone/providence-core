import Foundation

/// Line-delimited JSON reader/writer.
/// - Reads requests from stdin, dispatches, and writes responses/events to stdout.
/// - A single serial write queue guarantees ordered, non-interleaved output.
/// - On stdin EOF: run() drains in-flight work, then returns so main can exit(0).
final class IOLoop {
    private let decoder = JSONDecoder()
    private let encoder: JSONEncoder
    private let writeQueue = DispatchQueue(label: "bridge.write", qos: .userInitiated)
    private let dispatcher: Dispatcher
    private let inflight = DispatchGroup()

    init(dispatcher: Dispatcher) {
        self.dispatcher = dispatcher
        let enc = JSONEncoder()
        enc.outputFormatting = []
        self.encoder = enc
    }

    func run() {
        // readLine reads from stdin, stripping trailing newline.
        // Returns nil on EOF.
        while let line = readLine(strippingNewline: true) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.isEmpty { continue }
            processLine(trimmed)
        }
        // stdin closed. Wait for any dispatcher handlers still running (or
        // queued) so we don't exit before their response flushes.
        inflight.wait()
        // Then drain the write queue.
        flush()
    }

    /// Called by the dispatcher to register in-flight work. Caller must invoke
    /// `workDidFinish` exactly once when the handler completes (including async
    /// completion paths).
    func workDidStart() { inflight.enter() }
    func workDidFinish() { inflight.leave() }

    func processLine(_ line: String) {
        guard let data = line.data(using: .utf8) else {
            emitError(id: nil, code: ErrorCode.badRequest, message: "invalid utf-8 on stdin")
            return
        }

        let req: Request
        do {
            req = try decoder.decode(Request.self, from: data)
        } catch {
            // Try to salvage an id if present in the raw payload, so the Go side
            // can still correlate the error response.
            let salvagedID = extractID(from: data)
            if let rid = salvagedID {
                let resp = Response(
                    id: rid,
                    ok: false,
                    result: nil,
                    error: ProtocolError(
                        code: ErrorCode.badRequest,
                        message: "decode failed: \(error.localizedDescription)"
                    )
                )
                emitResponse(resp)
            } else {
                emitError(id: nil, code: ErrorCode.badRequest,
                          message: "decode failed: \(error.localizedDescription)")
            }
            return
        }

        dispatcher.dispatch(req)
    }

    private func extractID(from data: Data) -> String? {
        guard let obj = try? JSONSerialization.jsonObject(with: data, options: []),
              let dict = obj as? [String: Any],
              let id = dict["id"] as? String else {
            return nil
        }
        return id
    }

    func emitResponse(_ response: Response) {
        writeQueue.async { [encoder] in
            do {
                let data = try encoder.encode(response)
                IOLoop.writeLine(data)
            } catch {
                // Last-resort: emit a bareword error event so the Go side
                // has something to log.
                let fallback = #"{"event":"encode_error","data":{"id":"\#(response.id)"}}"#
                if let fb = fallback.data(using: .utf8) {
                    IOLoop.writeLine(fb)
                }
            }
        }
    }

    func emitEvent(_ event: Event) {
        writeQueue.async { [encoder] in
            do {
                let data = try encoder.encode(event)
                IOLoop.writeLine(data)
            } catch {
                // Ignore; events are best-effort.
            }
        }
    }

    func emitError(id: String?, code: String, message: String) {
        if let id = id {
            let resp = Response(
                id: id,
                ok: false,
                result: nil,
                error: ProtocolError(code: code, message: message)
            )
            emitResponse(resp)
        } else {
            let event = Event(
                event: "error",
                data: AnyCodable(["code": code, "message": message]),
                ts_ns: IOLoop.nowNs()
            )
            emitEvent(event)
        }
    }

    /// Block the caller until the write queue drains. Used by shutdown so the
    /// ack response is flushed before we exit.
    func flush() {
        writeQueue.sync {}
    }

    // MARK: - Low-level stdout writer

    private static let stdoutHandle = FileHandle.standardOutput
    private static let newline: Data = Data([0x0A])

    private static func writeLine(_ data: Data) {
        // FileHandle.write can throw on broken pipe on modern SDKs.
        // We ignore the error - if stdout is gone, we can't do anything useful.
        do {
            try stdoutHandle.write(contentsOf: data)
            try stdoutHandle.write(contentsOf: newline)
        } catch {
            // Pipe closed etc. Silent.
        }
    }

    static func nowNs() -> UInt64 {
        var ts = timespec()
        clock_gettime(CLOCK_MONOTONIC, &ts)
        return UInt64(ts.tv_sec) * 1_000_000_000 + UInt64(ts.tv_nsec)
    }
}
