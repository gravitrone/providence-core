import Foundation
import AppKit
import ApplicationServices

// MARK: - Wire types

struct BatchAction: Codable {
    let type: String
    let params: AnyCodable?
}

struct BatchActionResult: Codable {
    let index: Int
    let type: String
    let ok: Bool
    let result: AnyCodable?
    let error: String?
    let duration_ms: Int

    enum CodingKeys: String, CodingKey {
        case index, type, ok, result, error, duration_ms
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(index, forKey: .index)
        try c.encode(type, forKey: .type)
        try c.encode(ok, forKey: .ok)
        if let result = result { try c.encode(result, forKey: .result) }
        if let error = error { try c.encode(error, forKey: .error) }
        try c.encode(duration_ms, forKey: .duration_ms)
    }
}

struct ActionBatchParams: Codable {
    let actions: [BatchAction]
    let stop_on_error: Bool?
    let screenshot_after: Bool?
    /// If true, a mid-batch frontmost-app change aborts with focus_changed.
    /// Defaults to true.
    let abort_on_focus_change: Bool?
}

struct ActionBatchResult: Codable {
    let completed: Int
    let failed_at: Int?
    let actions: [BatchActionResult]
    let final_screenshot: String?

    enum CodingKeys: String, CodingKey {
        case completed, failed_at, actions, final_screenshot
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(completed, forKey: .completed)
        if let failed_at = failed_at { try c.encode(failed_at, forKey: .failed_at) }
        try c.encode(actions, forKey: .actions)
        if let final_screenshot = final_screenshot {
            try c.encode(final_screenshot, forKey: .final_screenshot)
        }
    }
}

// MARK: - Batch executor

enum Batch {
    static func execute(_ params: ActionBatchParams, capture: Any) async throws -> ActionBatchResult {
        let stopOnError = params.stop_on_error ?? true
        let abortOnFocus = params.abort_on_focus_change ?? true
        let startingApp = NSWorkspace.shared.frontmostApplication?.processIdentifier
        var results: [BatchActionResult] = []
        var failedAt: Int? = nil

        for (i, action) in params.actions.enumerated() {
            let t0 = DispatchTime.now()
            do {
                if abortOnFocus,
                   let current = NSWorkspace.shared.frontmostApplication?.processIdentifier,
                   let start = startingApp,
                   current != start {
                    throw BridgeError(
                        code: ErrorCode.focusChanged,
                        message: "focus changed to pid \(current); aborting batch"
                    )
                }

                let r = try await dispatch(action: action)
                let dur = Int((DispatchTime.now().uptimeNanoseconds &- t0.uptimeNanoseconds) / 1_000_000)
                results.append(BatchActionResult(
                    index: i, type: action.type, ok: true,
                    result: r, error: nil, duration_ms: dur
                ))
            } catch {
                let dur = Int((DispatchTime.now().uptimeNanoseconds &- t0.uptimeNanoseconds) / 1_000_000)
                let msg: String
                if let be = error as? BridgeError {
                    msg = "\(be.code): \(be.message)"
                } else {
                    msg = "\(error)"
                }
                results.append(BatchActionResult(
                    index: i, type: action.type, ok: false,
                    result: nil, error: msg, duration_ms: dur
                ))
                if stopOnError {
                    failedAt = i
                    break
                }
            }
        }

        var screenshotPath: String? = nil
        if params.screenshot_after ?? false {
            if #available(macOS 12.3, *), let eng = capture as? CaptureEngine {
                let t0 = IOLoop.nowNs()
                let img = try await eng.captureOnce(region: nil, startNs: t0)
                screenshotPath = img.path
            } else {
                let t0 = IOLoop.nowNs()
                let img = try LegacyCapture.captureOnce(region: nil, startNs: t0)
                screenshotPath = img.path
            }
        }

        return ActionBatchResult(
            completed: results.filter { $0.ok }.count,
            failed_at: failedAt,
            actions: results,
            final_screenshot: screenshotPath
        )
    }

    // MARK: - Action dispatch

    private static func dispatch(action: BatchAction) async throws -> AnyCodable {
        switch action.type {
        case "click":
            let p: ClickParams = try decodeAC(action.params)
            try Input.click(p)
            return AnyCodable(["ok": AnyCodable(true)])

        case "double_click":
            var p: ClickParams = try decodeAC(action.params)
            p = ClickParams(x: p.x, y: p.y, button: p.button, count: 2,
                            modifiers: p.modifiers, settle_ms: p.settle_ms)
            try Input.click(p)
            return AnyCodable(["ok": AnyCodable(true)])

        case "right_click":
            var p: ClickParams = try decodeAC(action.params)
            p = ClickParams(x: p.x, y: p.y, button: "right", count: p.count,
                            modifiers: p.modifiers, settle_ms: p.settle_ms)
            try Input.click(p)
            return AnyCodable(["ok": AnyCodable(true)])

        case "click_element":
            let raw = action.params
            let q: AXQueryParams = try decodeNested(raw, key: "query")
            let act = decodeStringNested(raw, key: "action") ?? "click"
            let find = try AXQuerier.find(q)
            guard let match = find.matches.first else {
                throw BridgeError(code: ErrorCode.elementNotFound,
                                  message: "no match for click_element query")
            }
            let axAction: String
            switch act {
            case "right_click": axAction = "AXShowMenu"
            case "double_click": axAction = "AXPress"  // AX has no native double; caller should prefer coord click
            default: axAction = "AXPress"
            }
            guard let el = AXCache.shared.lookup(match.id) else {
                throw BridgeError(code: ErrorCode.elementNotFound,
                                  message: "element evicted: \(match.id)")
            }
            AXUIElementSetMessagingTimeout(el, 1.5)
            let err = AXUIElementPerformAction(el, axAction as CFString)
            if err != .success {
                throw BridgeError(code: ErrorCode.captureFailed,
                                  message: "AXPerform failed: \(err.rawValue)")
            }
            return AnyCodable([
                "matched_id": AnyCodable(match.id),
                "method": AnyCodable("ax_perform"),
                "action": AnyCodable(axAction),
            ])

        case "type", "type_text":
            let p: TypeTextParams = try decodeAC(action.params)
            try Input.typeText(p)
            return AnyCodable(["ok": AnyCodable(true)])

        case "key", "key_combo":
            let p: KeyComboParams = try decodeAC(action.params)
            try Input.keyCombo(p)
            return AnyCodable(["ok": AnyCodable(true)])

        case "wait":
            let ms = decodeIntNested(action.params, key: "ms") ?? 0
            if ms > 0 {
                try await Task.sleep(nanoseconds: UInt64(ms) * 1_000_000)
            }
            return AnyCodable(["waited_ms": AnyCodable(ms)])

        case "verify_ax":
            let q: AXQueryParams = try decodeNested(action.params, key: "expect")
            let result = try AXQuerier.find(q)
            if result.matches.isEmpty {
                throw BridgeError(code: ErrorCode.elementNotFound,
                                  message: "verify_ax: expected element not found")
            }
            return AnyCodable(["matched": AnyCodable(result.matches.count)])

        case "read_value":
            let eid = decodeStringNested(action.params, key: "element_id") ?? ""
            guard !eid.isEmpty else {
                throw BridgeError(code: ErrorCode.badRequest,
                                  message: "read_value requires element_id")
            }
            guard let el = AXCache.shared.lookup(eid) else {
                throw BridgeError(code: ErrorCode.elementNotFound,
                                  message: "element not in cache: \(eid)")
            }
            let value = axString(el, kAXValueAttribute) ?? ""
            return AnyCodable(["value": AnyCodable(value)])

        case "focus_app":
            let name = decodeStringNested(action.params, key: "app") ?? ""
            guard !name.isEmpty else {
                throw BridgeError(code: ErrorCode.badRequest, message: "focus_app requires app")
            }
            let apps = NSWorkspace.shared.runningApplications
            let needle = name.lowercased()
            guard let found = apps.first(where: {
                $0.bundleIdentifier == name
                    || ($0.localizedName ?? "").lowercased().contains(needle)
            }) else {
                throw BridgeError(code: ErrorCode.badRequest, message: "app not found: \(name)")
            }
            found.activate(options: [])
            try await Task.sleep(nanoseconds: 150_000_000)
            return AnyCodable([
                "activated": AnyCodable(true),
                "pid": AnyCodable(Int(found.processIdentifier)),
            ])

        default:
            throw BridgeError(code: ErrorCode.badRequest,
                              message: "unknown action type: \(action.type)")
        }
    }

    // MARK: - Decoding helpers

    /// Decode an entire AnyCodable payload into a typed Codable struct by
    /// round-tripping through JSON. Matches `Dispatcher.decode`.
    static func decodeAC<T: Decodable>(_ ac: AnyCodable?) throws -> T {
        let enc = JSONEncoder()
        let dec = JSONDecoder()
        let data = try enc.encode(ac ?? AnyCodable([String: AnyCodable]()))
        return try dec.decode(T.self, from: data)
    }

    /// Decode a nested field at `key` as a typed Codable struct.
    static func decodeNested<T: Decodable>(_ ac: AnyCodable?, key: String) throws -> T {
        guard let dict = ac?.asDict, let inner = dict[key] else {
            throw BridgeError(code: ErrorCode.badRequest,
                              message: "missing field: \(key)")
        }
        return try decodeAC(inner)
    }

    static func decodeStringNested(_ ac: AnyCodable?, key: String) -> String? {
        ac?.asDict?[key]?.asString
    }

    static func decodeIntNested(_ ac: AnyCodable?, key: String) -> Int? {
        ac?.asDict?[key]?.asInt
    }
}
