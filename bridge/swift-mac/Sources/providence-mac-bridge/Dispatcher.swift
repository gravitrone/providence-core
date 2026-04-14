import Foundation
import ApplicationServices

/// Routes RPC method names to handlers. Per-capability serial queues so that,
/// for example, two screenshot requests serialize but a screenshot and an
/// AX query run concurrently.
final class Dispatcher {
    private let captureQueue = DispatchQueue(label: "bridge.capture", qos: .userInitiated)
    private let inputQueue = DispatchQueue(label: "bridge.input", qos: .userInitiated)
    private let axQueue = DispatchQueue(label: "bridge.ax", qos: .userInitiated)
    private let appQueue = DispatchQueue(label: "bridge.app", qos: .userInitiated)
    private let metaQueue = DispatchQueue(label: "bridge.meta", qos: .userInitiated)

    weak var ioLoop: IOLoop?

    private let captureEngine: Any?  // typed as Any because CaptureEngine is gated on macOS 12.3+.

    static let protocolVersion = "1"
    static let bridgeVersion = "0.1.0-phase5"

    private let batchQueue = DispatchQueue(label: "bridge.batch", qos: .userInitiated)

    init() {
        if #available(macOS 12.3, *) {
            self.captureEngine = CaptureEngine()
        } else {
            self.captureEngine = nil
        }
    }

    func dispatch(_ req: Request) {
        // Register the request as in-flight so the IO loop's EOF-wait drains it.
        ioLoop?.workDidStart()

        switch req.method {
        case "ping":
            metaQueue.async { [weak self] in
                self?.handlePing(req)
                self?.ioLoop?.workDidFinish()
            }
        case "preflight":
            metaQueue.async { [weak self] in
                self?.handlePreflight(req)
                self?.ioLoop?.workDidFinish()
            }
        case "configure":
            metaQueue.async { [weak self] in
                self?.handleConfigure(req)
                self?.ioLoop?.workDidFinish()
            }
        case "shutdown":
            metaQueue.async { [weak self] in
                self?.handleShutdown(req)
                // handleShutdown exits before returning; no workDidFinish.
            }
        case "screenshot":
            captureQueue.async { [weak self] in
                self?.handleScreenshot(req, region: nil)
            }
        case "screenshot_region":
            captureQueue.async { [weak self] in
                guard let self = self else { return }
                let region = Dispatcher.parseRegion(req.params)
                self.handleScreenshot(req, region: region)
            }
        case "click":
            handleOnInput(req) { params -> AnyCodable in
                let p: ClickParams = try Dispatcher.decode(params)
                try Input.click(p)
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "double_click":
            handleOnInput(req) { params -> AnyCodable in
                var p: ClickParams = try Dispatcher.decode(params)
                // Force count=2 regardless of caller.
                p = ClickParams(
                    x: p.x, y: p.y,
                    button: p.button,
                    count: 2,
                    modifiers: p.modifiers,
                    settle_ms: p.settle_ms
                )
                try Input.click(p)
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "right_click":
            handleOnInput(req) { params -> AnyCodable in
                var p: ClickParams = try Dispatcher.decode(params)
                // Force button to "right".
                p = ClickParams(
                    x: p.x, y: p.y,
                    button: "right",
                    count: p.count,
                    modifiers: p.modifiers,
                    settle_ms: p.settle_ms
                )
                try Input.click(p)
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "type_text":
            handleOnInput(req) { params -> AnyCodable in
                let p: TypeTextParams = try Dispatcher.decode(params)
                try Input.typeText(p)
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "key_combo":
            handleOnInput(req) { params -> AnyCodable in
                let p: KeyComboParams = try Dispatcher.decode(params)
                try Input.keyCombo(p)
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "ax_tree":
            handleOnAX(req) { params -> AnyCodable in
                let p: AXTreeParams = try Dispatcher.decode(params)
                let result = try AXTreeWalker.walk(p)
                return try Dispatcher.encodeToAnyCodable(result)
            }
        case "ax_find":
            handleOnAX(req) { params -> AnyCodable in
                let q: AXQueryParams = try Dispatcher.decode(params)
                let result = try AXQuerier.find(q)
                return try Dispatcher.encodeToAnyCodable(result)
            }
        case "ax_perform":
            handleOnAX(req) { params -> AnyCodable in
                let p: AXPerformParams = try Dispatcher.decode(params)
                guard let el = AXCache.shared.lookup(p.element_id) else {
                    throw BridgeError(
                        code: ErrorCode.elementNotFound,
                        message: "no element: \(p.element_id) (possibly stale after cache invalidation)"
                    )
                }
                AXUIElementSetMessagingTimeout(el, 1.5)
                let err = AXUIElementPerformAction(el, p.action as CFString)
                if err != .success {
                    throw BridgeError(
                        code: ErrorCode.captureFailed,
                        message: "AXPerform failed: \(err.rawValue)"
                    )
                }
                return AnyCodable(["ok": AnyCodable(true)])
            }
        case "screen_diff":
            captureQueue.async { [weak self] in
                guard let self = self else { return }
                let engine = self.captureEngine
                let sem = DispatchSemaphore(value: 0)
                Task {
                    defer {
                        self.ioLoop?.workDidFinish()
                        sem.signal()
                    }
                    do {
                        let p: ScreenDiffParams = try Dispatcher.decode(req.params)
                        let result = try await Diff.compute(params: p, capture: engine ?? ())
                        let ac = try Dispatcher.encodeToAnyCodable(result)
                        self.ioLoop?.emitResponse(Response(id: req.id, ok: true, result: ac))
                    } catch let err as BridgeError {
                        self.respondError(id: req.id, code: err.code, message: err.message,
                                          url: err.url, remediable: err.remediable)
                    } catch {
                        self.respondError(id: req.id, code: ErrorCode.captureFailed,
                                          message: error.localizedDescription)
                    }
                }
                // Block captureQueue until this diff completes so successive
                // screen_diff calls see each other's FrameMemory updates.
                sem.wait()
            }
        case "action_batch":
            batchQueue.async { [weak self] in
                guard let self = self else { return }
                let engine = self.captureEngine
                Task {
                    defer { self.ioLoop?.workDidFinish() }
                    do {
                        let p: ActionBatchParams = try Dispatcher.decode(req.params)
                        let result = try await Batch.execute(p, capture: engine ?? ())
                        let ac = try Dispatcher.encodeToAnyCodable(result)
                        self.ioLoop?.emitResponse(Response(id: req.id, ok: true, result: ac))
                    } catch let err as BridgeError {
                        self.respondError(id: req.id, code: err.code, message: err.message,
                                          url: err.url, remediable: err.remediable)
                    } catch {
                        self.respondError(id: req.id, code: ErrorCode.badRequest,
                                          message: "decode failed: \(error.localizedDescription)")
                    }
                }
            }
        default:
            // Not implemented in Phase 1.
            metaQueue.async { [weak self] in
                self?.respondError(
                    id: req.id,
                    code: ErrorCode.badRequest,
                    message: "method not implemented yet: \(req.method)"
                )
                self?.ioLoop?.workDidFinish()
            }
        }
    }

    // MARK: - Handlers

    private func handlePing(_ req: Request) {
        let result: [String: AnyCodable] = [
            "pong": AnyCodable(true),
            "version": AnyCodable(Dispatcher.bridgeVersion),
            "protocol_version": AnyCodable(Dispatcher.protocolVersion),
        ]
        ioLoop?.emitResponse(Response(id: req.id, ok: true, result: AnyCodable(result)))
    }

    private func handlePreflight(_ req: Request) {
        let statuses = Permissions.check()
        var encoded: [AnyCodable] = []
        for s in statuses {
            let d: [String: AnyCodable] = [
                "permission": AnyCodable(s.permission),
                "granted": AnyCodable(s.granted),
                "settings_url": AnyCodable(s.settingsURL),
                "hint": AnyCodable(s.hint),
            ]
            encoded.append(AnyCodable(d))
        }
        ioLoop?.emitResponse(Response(
            id: req.id,
            ok: true,
            result: AnyCodable(["permissions": AnyCodable(encoded)])
        ))
    }

    private func handleConfigure(_ req: Request) {
        do {
            let p: ConfigureParams = try Dispatcher.decode(req.params)
            if let depth = p.ax_max_depth {
                AXTreeWalker.configuredMaxDepth = max(1, depth)
            }
            if let nodes = p.ax_max_nodes {
                AXTreeWalker.configuredMaxNodes = max(1, nodes)
            }
            // CaptureEngine FPS configuration is advisory; the stream is
            // re-started on next capture if FPS changes.
            if let fps = p.warm_stream_fps {
                BridgeConfig.warmStreamFPS = max(1, min(60, fps))
            }
            if let fps = p.burst_stream_fps {
                BridgeConfig.burstStreamFPS = max(1, min(60, fps))
            }
            ioLoop?.emitResponse(Response(
                id: req.id, ok: true, result: AnyCodable(["ok": AnyCodable(true)])
            ))
        } catch {
            respondError(
                id: req.id,
                code: ErrorCode.badRequest,
                message: "configure: decode failed: \(error.localizedDescription)"
            )
        }
    }

    private func handleShutdown(_ req: Request) {
        // Ack first, flush, then exit.
        let result: [String: AnyCodable] = [
            "bye": AnyCodable(true),
        ]
        ioLoop?.emitResponse(Response(id: req.id, ok: true, result: AnyCodable(result)))
        ioLoop?.flush()
        exit(0)
    }

    private func handleScreenshot(_ req: Request, region: CGRect?) {
        if #available(macOS 12.3, *) {
            guard let engine = captureEngine as? CaptureEngine else {
                respondError(id: req.id, code: ErrorCode.internal,
                             message: "capture engine unavailable")
                ioLoop?.workDidFinish()
                return
            }
            let t0 = IOLoop.nowNs()
            Task {
                defer { self.ioLoop?.workDidFinish() }
                do {
                    let img = try await engine.captureOnce(region: region, startNs: t0)
                    let result: [String: AnyCodable] = [
                        "path": AnyCodable(img.path),
                        "w": AnyCodable(img.width),
                        "h": AnyCodable(img.height),
                        "capture_ns": AnyCodable(img.captureNs),
                        "sha1": AnyCodable(img.sha1),
                    ]
                    self.ioLoop?.emitResponse(Response(
                        id: req.id, ok: true, result: AnyCodable(result)
                    ))
                } catch let err as BridgeError {
                    self.respondError(
                        id: req.id,
                        code: err.code,
                        message: err.message,
                        url: err.url,
                        remediable: err.remediable
                    )
                } catch {
                    self.respondError(
                        id: req.id,
                        code: ErrorCode.captureFailed,
                        message: error.localizedDescription
                    )
                }
            }
        } else {
            // macOS 12.0-12.2 fallback: CGWindowListCreateImage.
            defer { ioLoop?.workDidFinish() }
            do {
                let t0 = IOLoop.nowNs()
                let img = try LegacyCapture.captureOnce(region: region, startNs: t0)
                let result: [String: AnyCodable] = [
                    "path": AnyCodable(img.path),
                    "w": AnyCodable(img.width),
                    "h": AnyCodable(img.height),
                    "capture_ns": AnyCodable(img.captureNs),
                    "sha1": AnyCodable(img.sha1),
                ]
                ioLoop?.emitResponse(Response(
                    id: req.id, ok: true, result: AnyCodable(result)
                ))
            } catch let err as BridgeError {
                respondError(
                    id: req.id,
                    code: err.code,
                    message: err.message,
                    url: err.url,
                    remediable: err.remediable
                )
            } catch {
                respondError(
                    id: req.id,
                    code: ErrorCode.captureFailed,
                    message: error.localizedDescription
                )
            }
        }
    }

    // MARK: - Helpers

    private func respondError(id: String, code: String, message: String,
                              url: String? = nil, remediable: Bool? = nil) {
        let err = ProtocolError(code: code, message: message, url: url, remediable: remediable)
        ioLoop?.emitResponse(Response(id: id, ok: false, result: nil, error: err))
    }

    /// Run `body` on the input serial queue, emit its result as a success
    /// response, and map any thrown error to the appropriate error code.
    /// Used by click / type_text / key_combo handlers.
    private func handleOnInput(_ req: Request,
                               body: @escaping (AnyCodable?) throws -> AnyCodable) {
        inputQueue.async { [weak self] in
            guard let self = self else { return }
            defer { self.ioLoop?.workDidFinish() }
            do {
                let result = try body(req.params)
                self.ioLoop?.emitResponse(Response(
                    id: req.id, ok: true, result: result
                ))
            } catch let err as InputError {
                switch err {
                case .postFailed:
                    self.respondError(
                        id: req.id,
                        code: ErrorCode.captureFailed,
                        message: "CGEvent post failed (is Accessibility granted?)"
                    )
                case .invalidKeyCombo:
                    self.respondError(
                        id: req.id,
                        code: ErrorCode.badRequest,
                        message: "key_combo requires virtual_code >= 0 or a non-empty key"
                    )
                }
            } catch let err as BridgeError {
                self.respondError(
                    id: req.id,
                    code: err.code,
                    message: err.message,
                    url: err.url,
                    remediable: err.remediable
                )
            } catch {
                self.respondError(
                    id: req.id,
                    code: ErrorCode.badRequest,
                    message: "decode failed: \(error.localizedDescription)"
                )
            }
        }
    }

    /// Run `body` on the AX serial queue. Same error-mapping contract as
    /// `handleOnInput`, but routes BridgeError codes straight through so
    /// element_not_found / bad_request stay distinct.
    private func handleOnAX(_ req: Request,
                            body: @escaping (AnyCodable?) throws -> AnyCodable) {
        axQueue.async { [weak self] in
            guard let self = self else { return }
            defer { self.ioLoop?.workDidFinish() }
            do {
                let result = try body(req.params)
                self.ioLoop?.emitResponse(Response(
                    id: req.id, ok: true, result: result
                ))
            } catch let err as BridgeError {
                self.respondError(
                    id: req.id,
                    code: err.code,
                    message: err.message,
                    url: err.url,
                    remediable: err.remediable
                )
            } catch {
                self.respondError(
                    id: req.id,
                    code: ErrorCode.badRequest,
                    message: "decode failed: \(error.localizedDescription)"
                )
            }
        }
    }

    /// Decode a typed params object out of the request's AnyCodable params.
    /// Re-encodes to JSON then decodes to T; good enough for the small
    /// param shapes we use and keeps the call sites one-liner clean.
    static func decode<T: Decodable>(_ params: AnyCodable?) throws -> T {
        let enc = JSONEncoder()
        let dec = JSONDecoder()
        let data = try enc.encode(params ?? AnyCodable([String: AnyCodable]()))
        return try dec.decode(T.self, from: data)
    }

    /// Round-trip a Codable value through JSON to produce an AnyCodable
    /// wrapper. Used by handlers that return typed structs (AX tree / find
    /// results) since AnyCodable can't encode arbitrary Codable types
    /// directly - it handles JSON-primitive shapes only.
    static func encodeToAnyCodable<T: Encodable>(_ value: T) throws -> AnyCodable {
        let enc = JSONEncoder()
        let data = try enc.encode(value)
        let dec = JSONDecoder()
        return try dec.decode(AnyCodable.self, from: data)
    }

    /// Parse a CGRect from `params.region` with shape:
    /// `{region: {x: number, y: number, w: number, h: number}}`
    static func parseRegion(_ params: AnyCodable?) -> CGRect? {
        guard let dict = params?.asDict,
              let regionVal = dict["region"],
              let r = regionVal.asDict else {
            return nil
        }
        guard let x = r["x"]?.asDouble,
              let y = r["y"]?.asDouble,
              let w = r["w"]?.asDouble,
              let h = r["h"]?.asDouble else {
            return nil
        }
        return CGRect(x: x, y: y, width: w, height: h)
    }
}
