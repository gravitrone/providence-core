import Foundation

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
    static let bridgeVersion = "0.1.0-phase1"

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
