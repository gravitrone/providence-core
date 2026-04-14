import Foundation

// MARK: - Wire types

struct Request: Codable {
    let id: String
    let method: String
    let params: AnyCodable?
}

struct Response: Codable {
    let id: String
    let ok: Bool
    let result: AnyCodable?
    let error: ProtocolError?

    init(id: String, ok: Bool, result: AnyCodable? = nil, error: ProtocolError? = nil) {
        self.id = id
        self.ok = ok
        self.result = result
        self.error = error
    }

    // Custom encoder to keep key order stable and omit nil fields.
    enum CodingKeys: String, CodingKey {
        case id, ok, result, error
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(ok, forKey: .ok)
        if let result = result { try c.encode(result, forKey: .result) }
        if let error = error { try c.encode(error, forKey: .error) }
    }
}

struct ProtocolError: Codable {
    let code: String
    let message: String
    let url: String?
    let remediable: Bool?

    init(code: String, message: String, url: String? = nil, remediable: Bool? = nil) {
        self.code = code
        self.message = message
        self.url = url
        self.remediable = remediable
    }

    enum CodingKeys: String, CodingKey {
        case code, message, url, remediable
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(code, forKey: .code)
        try c.encode(message, forKey: .message)
        if let url = url { try c.encode(url, forKey: .url) }
        if let remediable = remediable { try c.encode(remediable, forKey: .remediable) }
    }
}

struct Event: Codable {
    let event: String
    let data: AnyCodable?
    let ts_ns: UInt64?

    init(event: String, data: AnyCodable? = nil, ts_ns: UInt64? = nil) {
        self.event = event
        self.data = data
        self.ts_ns = ts_ns
    }

    enum CodingKeys: String, CodingKey {
        case event, data, ts_ns
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(event, forKey: .event)
        if let data = data { try c.encode(data, forKey: .data) }
        if let ts_ns = ts_ns { try c.encode(ts_ns, forKey: .ts_ns) }
    }
}

// MARK: - Error codes

enum ErrorCode {
    static let permissionDenied = "permission_denied"
    static let unsupportedOS = "unsupported_os"
    static let badRequest = "bad_request"
    static let elementNotFound = "element_not_found"
    static let timeout = "timeout"
    static let captureFailed = "capture_failed"
    static let focusChanged = "focus_changed"
    static let `internal` = "internal"
}

// MARK: - Configure params

struct ConfigureParams: Codable {
    let warm_stream_fps: Int?
    let burst_stream_fps: Int?
    let ax_max_depth: Int?
    let ax_max_nodes: Int?
}

// MARK: - BridgeConfig (mutable runtime config)

/// Mutable runtime configuration for the bridge, set via the `configure` RPC.
enum BridgeConfig {
    static var warmStreamFPS: Int = 2
    static var burstStreamFPS: Int = 30
}

// MARK: - AnyCodable

/// Type-erased JSON value wrapper.
/// Supports: null, Bool, Int, Double, String, [AnyCodable], [String: AnyCodable].
struct AnyCodable: Codable {
    let value: Any?

    init(_ value: Any?) {
        self.value = AnyCodable.normalize(value)
    }

    private static func normalize(_ value: Any?) -> Any? {
        guard let value = value else { return nil }
        if value is NSNull { return nil }
        if let v = value as? AnyCodable { return v.value }
        if let arr = value as? [Any?] {
            return arr.map { AnyCodable(normalize($0)) }
        }
        if let dict = value as? [String: Any?] {
            var out: [String: AnyCodable] = [:]
            for (k, v) in dict { out[k] = AnyCodable(normalize(v)) }
            return out
        }
        if let arr = value as? [AnyCodable] { return arr }
        if let dict = value as? [String: AnyCodable] { return dict }
        return value
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            self.value = nil
            return
        }
        if let b = try? c.decode(Bool.self) { self.value = b; return }
        if let i = try? c.decode(Int64.self) { self.value = i; return }
        if let d = try? c.decode(Double.self) { self.value = d; return }
        if let s = try? c.decode(String.self) { self.value = s; return }
        if let arr = try? c.decode([AnyCodable].self) { self.value = arr; return }
        if let dict = try? c.decode([String: AnyCodable].self) { self.value = dict; return }
        throw DecodingError.dataCorruptedError(
            in: c,
            debugDescription: "AnyCodable: unsupported JSON value"
        )
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        guard let v = value else { try c.encodeNil(); return }

        switch v {
        case let b as Bool: try c.encode(b)
        case let i as Int: try c.encode(i)
        case let i as Int64: try c.encode(i)
        case let i as UInt64:
            // UInt64 may overflow Int64; encode as string fallback or keep as Double if in range.
            if i <= UInt64(Int64.max) {
                try c.encode(Int64(i))
            } else {
                try c.encode(Double(i))
            }
        case let d as Double: try c.encode(d)
        case let f as Float: try c.encode(Double(f))
        case let s as String: try c.encode(s)
        case let arr as [AnyCodable]: try c.encode(arr)
        case let dict as [String: AnyCodable]: try c.encode(dict)
        case let arr as [Any?]: try c.encode(arr.map { AnyCodable($0) })
        case let arr as [Any]: try c.encode(arr.map { AnyCodable($0) })
        case let dict as [String: Any?]:
            var out: [String: AnyCodable] = [:]
            for (k, vv) in dict { out[k] = AnyCodable(vv) }
            try c.encode(out)
        case let dict as [String: Any]:
            var out: [String: AnyCodable] = [:]
            for (k, vv) in dict { out[k] = AnyCodable(vv) }
            try c.encode(out)
        case is NSNull:
            try c.encodeNil()
        default:
            // Last-ditch: try bridging through NSNumber or toString.
            if let n = v as? NSNumber {
                // NSNumber could be Bool or numeric; CFBoolean vs CFNumber.
                let typeID = CFGetTypeID(n as CFTypeRef)
                if typeID == CFBooleanGetTypeID() {
                    try c.encode(n.boolValue)
                } else {
                    try c.encode(n.doubleValue)
                }
            } else {
                throw EncodingError.invalidValue(
                    v,
                    EncodingError.Context(codingPath: encoder.codingPath,
                                          debugDescription: "AnyCodable: cannot encode \(type(of: v))")
                )
            }
        }
    }

    // Convenience subscripts / accessors for dispatcher params.

    var asDict: [String: AnyCodable]? { value as? [String: AnyCodable] }
    var asArray: [AnyCodable]? { value as? [AnyCodable] }
    var asString: String? { value as? String }
    var asBool: Bool? { value as? Bool }
    var asInt: Int? {
        if let i = value as? Int { return i }
        if let i = value as? Int64 { return Int(i) }
        if let d = value as? Double { return Int(d) }
        return nil
    }
    var asDouble: Double? {
        if let d = value as? Double { return d }
        if let i = value as? Int { return Double(i) }
        if let i = value as? Int64 { return Double(i) }
        return nil
    }
}
