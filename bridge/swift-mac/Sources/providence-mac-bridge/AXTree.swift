import Foundation
import ApplicationServices
import AppKit

// MARK: - Wire types

struct AXFrame: Codable {
    let x: Int
    let y: Int
    let w: Int
    let h: Int

    static let zero = AXFrame(x: 0, y: 0, w: 0, h: 0)
}

/// Serialized accessibility node. `score` is only populated by AXQuery
/// results. Fields are optional so the encoded JSON stays small for nodes
/// that don't carry a given attribute.
struct AXNode: Codable {
    let id: String
    let role: String
    let subrole: String?
    let title: String?
    let label: String?
    let value: String?
    let placeholder: String?
    let frame: AXFrame
    let enabled: Bool
    let focused: Bool?
    let selected: Bool?
    let actions: [String]?
    let children: [AXNode]?
    var score: Double?
}

struct AXTreeParams: Codable {
    let app: String?
    let pid: Int?
    let max_depth: Int?
    let max_nodes: Int?
    let include_invisible: Bool?
    let format: String?  // "json" (default) or "flat"
}

struct AXTreeResult: Codable {
    let root: AXNode?
    let flat: String?
    let truncated: Bool
    let app: String?
    let pid: Int?

    enum CodingKeys: String, CodingKey {
        case root, flat, truncated, app, pid
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if let root = root { try c.encode(root, forKey: .root) }
        if let flat = flat { try c.encode(flat, forKey: .flat) }
        try c.encode(truncated, forKey: .truncated)
        if let app = app { try c.encode(app, forKey: .app) }
        if let pid = pid { try c.encode(pid, forKey: .pid) }
    }
}

struct AXPerformParams: Codable {
    let element_id: String
    let action: String
}

// MARK: - PID resolution

/// Resolve the target PID for an AX call.
/// Priority: explicit `pid` > `app` lookup (bundle id, then localized name
/// case-insensitive substring) > frontmost application.
func resolveTargetPID(app: String?, pid: Int?) throws -> Int {
    if let p = pid, p > 0 { return p }
    if let a = app, !a.isEmpty {
        let apps = NSWorkspace.shared.runningApplications
        if let byBundle = apps.first(where: { $0.bundleIdentifier == a }) {
            return Int(byBundle.processIdentifier)
        }
        let needle = a.lowercased()
        if let byName = apps.first(where: {
            ($0.localizedName ?? "").lowercased().contains(needle)
        }) {
            return Int(byName.processIdentifier)
        }
        throw BridgeError(code: ErrorCode.badRequest, message: "app not found: \(a)")
    }
    if let front = NSWorkspace.shared.frontmostApplication {
        return Int(front.processIdentifier)
    }
    throw BridgeError(code: ErrorCode.internal, message: "no frontmost app")
}

// MARK: - AX attribute helpers

/// Safe wrapper around `AXUIElementCopyAttributeValue`. Returns `nil` if the
/// call fails for any reason (attribute unsupported, no value, timeout, app
/// unresponsive, ...). Callers must be prepared for nil.
func axCopyAttr(_ el: AXUIElement, _ attr: String) -> CFTypeRef? {
    var value: CFTypeRef?
    let err = AXUIElementCopyAttributeValue(el, attr as CFString, &value)
    if err != .success { return nil }
    return value
}

func axString(_ el: AXUIElement, _ attr: String) -> String? {
    guard let v = axCopyAttr(el, attr) else { return nil }
    if CFGetTypeID(v) == CFStringGetTypeID() {
        return v as? String
    }
    // Value attributes sometimes return numbers or booleans; coerce.
    if CFGetTypeID(v) == CFNumberGetTypeID() {
        return (v as? NSNumber)?.stringValue
    }
    if CFGetTypeID(v) == CFBooleanGetTypeID() {
        return (v as? Bool).map { $0 ? "true" : "false" }
    }
    return nil
}

func axBool(_ el: AXUIElement, _ attr: String) -> Bool? {
    guard let v = axCopyAttr(el, attr) else { return nil }
    if CFGetTypeID(v) == CFBooleanGetTypeID() {
        return (v as? NSNumber)?.boolValue
    }
    return nil
}

func axPoint(_ el: AXUIElement, _ attr: String) -> CGPoint? {
    guard let v = axCopyAttr(el, attr) else { return nil }
    let axValue = v as! AXValue
    if AXValueGetType(axValue) != .cgPoint { return nil }
    var p = CGPoint.zero
    if AXValueGetValue(axValue, .cgPoint, &p) { return p }
    return nil
}

func axSize(_ el: AXUIElement, _ attr: String) -> CGSize? {
    guard let v = axCopyAttr(el, attr) else { return nil }
    let axValue = v as! AXValue
    if AXValueGetType(axValue) != .cgSize { return nil }
    var s = CGSize.zero
    if AXValueGetValue(axValue, .cgSize, &s) { return s }
    return nil
}

func axChildren(_ el: AXUIElement) -> [AXUIElement] {
    guard let v = axCopyAttr(el, kAXChildrenAttribute) else { return [] }
    if CFGetTypeID(v) == CFArrayGetTypeID() {
        return (v as? [AXUIElement]) ?? []
    }
    return []
}

func axActionNames(_ el: AXUIElement) -> [String] {
    var actions: CFArray?
    let err = AXUIElementCopyActionNames(el, &actions)
    if err != .success { return [] }
    guard let arr = actions as? [String] else { return [] }
    return arr
}

// MARK: - AX walker

enum AXTreeWalker {
    /// Runtime-configurable defaults, set via the `configure` RPC.
    static var configuredMaxDepth: Int = 12
    static var configuredMaxNodes: Int = 2000

    /// Walk the AX tree for the target app. Respects depth / node budget /
    /// visibility filters. Returns a serialized tree plus a `truncated` flag.
    static func walk(_ params: AXTreeParams) throws -> AXTreeResult {
        let targetPID = try resolveTargetPID(app: params.app, pid: params.pid)
        let appName = NSRunningApplication(processIdentifier: pid_t(targetPID))?.localizedName
        let maxDepth = max(0, params.max_depth ?? configuredMaxDepth)
        let maxNodes = max(1, params.max_nodes ?? configuredMaxNodes)
        let includeInvisible = params.include_invisible ?? false
        let rootEl = AXUIElementCreateApplication(pid_t(targetPID))

        // Per-call AX timeout so an unresponsive app doesn't hang the bridge.
        AXUIElementSetMessagingTimeout(rootEl, 1.5)

        var nodeCount = 0
        var truncated = false
        let gen = AXCache.shared.currentGeneration
        let deadline = Date().addingTimeInterval(1.5)

        let root = serialize(
            rootEl,
            depth: 0,
            maxDepth: maxDepth,
            nodeBudget: &nodeCount,
            nodeLimit: maxNodes,
            includeInvisible: includeInvisible,
            generation: gen,
            deadline: deadline,
            truncated: &truncated
        )

        if params.format == "flat" {
            return AXTreeResult(
                root: nil,
                flat: flatten(root),
                truncated: truncated,
                app: appName,
                pid: targetPID
            )
        }
        return AXTreeResult(
            root: root,
            flat: nil,
            truncated: truncated,
            app: appName,
            pid: targetPID
        )
    }

    /// Recursively serialize `el` up to `maxDepth`. Node budget is tracked
    /// via `nodeBudget` inout: when the budget runs out the walker stops
    /// recursing and the caller sets `truncated = true`.
    static func serialize(
        _ el: AXUIElement,
        depth: Int,
        maxDepth: Int,
        nodeBudget: inout Int,
        nodeLimit: Int,
        includeInvisible: Bool,
        generation: UInt64,
        deadline: Date,
        truncated: inout Bool
    ) -> AXNode {
        nodeBudget += 1

        // Attribute reads. Protect each behind axCopyAttr which returns nil
        // on failure rather than crashing.
        let role = axString(el, kAXRoleAttribute) ?? "AXUnknown"
        let subrole = axString(el, kAXSubroleAttribute)
        let title = axString(el, kAXTitleAttribute)
        let label = axString(el, kAXDescriptionAttribute)
        let value = axString(el, kAXValueAttribute)
        let placeholder = axString(el, kAXPlaceholderValueAttribute)
        let enabled = axBool(el, kAXEnabledAttribute) ?? true
        let focused = axBool(el, kAXFocusedAttribute)
        let selected = axBool(el, kAXSelectedAttribute)
        let hidden = axBool(el, "AXHidden") ?? false

        let pos = axPoint(el, kAXPositionAttribute) ?? .zero
        let size = axSize(el, kAXSizeAttribute) ?? .zero
        let frame = AXFrame(
            x: Int(pos.x),
            y: Int(pos.y),
            w: Int(size.width),
            h: Int(size.height)
        )

        // Skip zero-frame / hidden nodes when includeInvisible is false.
        // The root application element frequently has a zero frame, so we
        // never skip depth 0 - we need it to carry the tree.
        let isZeroFrame = (frame.w == 0 || frame.h == 0)
        let shouldSkip = depth > 0 && !includeInvisible && (hidden || isZeroFrame)

        let actions = axActionNames(el)
        let actionsOut: [String]? = actions.isEmpty ? nil : actions

        let id = AXCache.shared.store(el, generation: generation)

        // Recurse into children while we still have budget and depth, and
        // we're not past the per-call deadline.
        var kids: [AXNode]? = nil
        if depth < maxDepth && nodeBudget < nodeLimit && Date() < deadline {
            let childEls = axChildren(el)
            if !childEls.isEmpty {
                var collected: [AXNode] = []
                collected.reserveCapacity(childEls.count)
                for child in childEls {
                    if nodeBudget >= nodeLimit {
                        truncated = true
                        break
                    }
                    if Date() >= deadline {
                        truncated = true
                        break
                    }
                    let childNode = serialize(
                        child,
                        depth: depth + 1,
                        maxDepth: maxDepth,
                        nodeBudget: &nodeBudget,
                        nodeLimit: nodeLimit,
                        includeInvisible: includeInvisible,
                        generation: generation,
                        deadline: deadline,
                        truncated: &truncated
                    )
                    if shouldIncludeChild(childNode, includeInvisible: includeInvisible) {
                        collected.append(childNode)
                    }
                }
                kids = collected.isEmpty ? nil : collected
            }
        } else if depth >= maxDepth {
            // Out of depth but children may exist - flag truncation.
            if !axChildren(el).isEmpty { truncated = true }
        }

        // If this node itself should be skipped we still return it (parent
        // handles filtering via shouldIncludeChild). Roots are never skipped.
        _ = shouldSkip

        return AXNode(
            id: id,
            role: role,
            subrole: subrole,
            title: title,
            label: label,
            value: value,
            placeholder: placeholder,
            frame: frame,
            enabled: enabled,
            focused: focused,
            selected: selected,
            actions: actionsOut,
            children: kids,
            score: nil
        )
    }

    /// Decide whether to include a child in a parent's `children` list.
    /// Root is always included by the caller; this only filters descendants.
    private static func shouldIncludeChild(_ node: AXNode, includeInvisible: Bool) -> Bool {
        if includeInvisible { return true }
        let isZero = node.frame.w == 0 || node.frame.h == 0
        // Keep containers with children even if their own frame is zero;
        // drop true leaves with zero area.
        if isZero && (node.children?.isEmpty ?? true) {
            return false
        }
        return true
    }

    /// Render a tree as a flat text block, one line per node with indent.
    /// Example: `  [3] AXButton "Back" frame=(220,115,30,30) enabled=false`.
    static func flatten(_ node: AXNode?) -> String {
        guard let node = node else { return "" }
        var out = ""
        var counter = 0
        flattenRecurse(node, indent: 0, counter: &counter, out: &out)
        return out
    }

    private static func flattenRecurse(
        _ node: AXNode,
        indent: Int,
        counter: inout Int,
        out: inout String
    ) {
        let pad = String(repeating: "  ", count: indent)
        let idx = counter
        counter += 1

        var line = "\(pad)[\(idx)] \(node.role)"
        if let title = node.title, !title.isEmpty {
            line += " \"\(escapeFlat(title))\""
        } else if let label = node.label, !label.isEmpty {
            line += " \"\(escapeFlat(label))\""
        } else if let value = node.value, !value.isEmpty {
            let trimmed = value.count > 40 ? String(value.prefix(40)) + "..." : value
            line += " =\"\(escapeFlat(trimmed))\""
        }
        line += " frame=(\(node.frame.x),\(node.frame.y),\(node.frame.w),\(node.frame.h))"
        line += " enabled=\(node.enabled)"
        if node.focused == true { line += " focused=true" }
        if node.selected == true { line += " selected=true" }
        out += line + "\n"

        if let kids = node.children {
            for k in kids {
                flattenRecurse(k, indent: indent + 1, counter: &counter, out: &out)
            }
        }
    }

    private static func escapeFlat(_ s: String) -> String {
        s.replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: " ")
    }
}
