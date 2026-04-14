import Foundation
import CoreGraphics

// MARK: - Parameter types

struct ClickParams: Codable {
    let x: Int
    let y: Int
    let button: String?      // "left" | "right" | "middle", default "left"
    let count: Int?          // default 1
    let modifiers: [String]?
    let settle_ms: Int?      // default 50
}

struct TypeTextParams: Codable {
    let text: String
    let delay_ms: Int?       // default 0, uses 8ms minimum between keystrokes
}

struct KeyComboParams: Codable {
    let virtual_code: Int    // -1 if unknown, then use `key`
    let modifiers: [String]?
    let key: String?         // fallback if virtual_code == -1; single char
}

// MARK: - Mouse button mapping

enum InputButton: CaseIterable {
    case left, right, middle

    static func parse(_ s: String?) -> InputButton {
        switch s?.lowercased() {
        case "right": return .right
        case "middle": return .middle
        default: return .left
        }
    }

    var down: CGEventType {
        switch self {
        case .left: return .leftMouseDown
        case .right: return .rightMouseDown
        case .middle: return .otherMouseDown
        }
    }

    var up: CGEventType {
        switch self {
        case .left: return .leftMouseUp
        case .right: return .rightMouseUp
        case .middle: return .otherMouseUp
        }
    }

    var cgButton: CGMouseButton {
        switch self {
        case .left: return .left
        case .right: return .right
        case .middle: return .center
        }
    }
}

// MARK: - Errors

enum InputError: Error {
    case postFailed
    case invalidKeyCombo
}

// MARK: - Modifier flag helper

func buildCGEventFlags(_ modifiers: [String]) -> CGEventFlags {
    var flags: CGEventFlags = []
    for m in modifiers {
        switch m.lowercased() {
        case "cmd", "command": flags.insert(.maskCommand)
        case "shift": flags.insert(.maskShift)
        case "option", "alt": flags.insert(.maskAlternate)
        case "control", "ctrl": flags.insert(.maskControl)
        case "fn", "function": flags.insert(.maskSecondaryFn)
        default: break
        }
    }
    return flags
}

// MARK: - Input dispatcher

enum Input {
    /// Post a mouse click at (x, y). Supports left/right/middle button,
    /// click count (double, triple, ...), modifier flags, and a settle
    /// delay between the cursor move and first press.
    static func click(_ p: ClickParams) throws {
        let src = CGEventSource(stateID: .hidSystemState)
        let point = CGPoint(x: p.x, y: p.y)
        let btn = InputButton.parse(p.button)
        let count = max(1, p.count ?? 1)
        let settle = UInt32((p.settle_ms ?? 50) * 1000) // microseconds
        let flags = buildCGEventFlags(p.modifiers ?? [])

        // Move cursor first so the click lands on the intended target.
        if let move = CGEvent(
            mouseEventSource: src,
            mouseType: .mouseMoved,
            mouseCursorPosition: point,
            mouseButton: btn.cgButton
        ) {
            move.flags = flags
            move.post(tap: .cghidEventTap)
        }
        if settle > 0 { usleep(settle) }

        // Click count times. For double/triple clicks we set the click state
        // field so apps like Finder / text editors recognise the gesture.
        for _ in 0..<count {
            if let down = CGEvent(
                mouseEventSource: src,
                mouseType: btn.down,
                mouseCursorPosition: point,
                mouseButton: btn.cgButton
            ) {
                down.flags = flags
                down.setIntegerValueField(.mouseEventClickState, value: Int64(count))
                down.post(tap: .cghidEventTap)
            }
            if let up = CGEvent(
                mouseEventSource: src,
                mouseType: btn.up,
                mouseCursorPosition: point,
                mouseButton: btn.cgButton
            ) {
                up.flags = flags
                up.setIntegerValueField(.mouseEventClickState, value: Int64(count))
                up.post(tap: .cghidEventTap)
            }
            if count > 1 { usleep(60_000) } // inter-click gap for multi-click
        }
    }

    /// Type a UTF-8 string as a stream of keyboard events using Unicode
    /// injection (virtualKey=0 + keyboardSetUnicodeString). Minimum 8ms
    /// delay between keystrokes to give target apps time to process.
    static func typeText(_ p: TypeTextParams) throws {
        let src = CGEventSource(stateID: .hidSystemState)
        let requested = p.delay_ms ?? 0
        let delay = UInt32(max(8, requested) * 1000) // microseconds, min 8ms
        for scalar in p.text.unicodeScalars {
            try postUnicodeScalar(scalar, source: src)
            usleep(delay)
        }
    }

    /// Post a key combo. Preferred path: caller provides a real virtual
    /// key code (HIToolbox kVK_*) plus optional modifier list. Fallback
    /// path: if virtual_code == -1 and a single-character `key` string is
    /// provided, we inject it as a Unicode keystroke with modifiers held.
    static func keyCombo(_ p: KeyComboParams) throws {
        let src = CGEventSource(stateID: .hidSystemState)
        let flags = buildCGEventFlags(p.modifiers ?? [])
        let vcode = p.virtual_code

        if vcode >= 0 {
            guard let down = CGEvent(
                keyboardEventSource: src,
                virtualKey: CGKeyCode(vcode),
                keyDown: true
            ) else {
                throw InputError.postFailed
            }
            down.flags = flags
            down.post(tap: .cghidEventTap)
            usleep(2_000)
            guard let up = CGEvent(
                keyboardEventSource: src,
                virtualKey: CGKeyCode(vcode),
                keyDown: false
            ) else {
                throw InputError.postFailed
            }
            up.flags = flags
            up.post(tap: .cghidEventTap)
        } else if let key = p.key, !key.isEmpty {
            // Unknown virtual code - fall back to Unicode injection with
            // modifier flags held. Works for single-char keys on layouts
            // where the char isn't trivially mappable.
            for scalar in key.unicodeScalars {
                try postUnicodeScalar(scalar, source: src, flags: flags)
            }
        } else {
            throw InputError.invalidKeyCombo
        }
    }

    // MARK: - Unicode keystroke helper

    /// Post a single Unicode scalar as a keyboard event pair. BMP scalars
    /// (<= U+FFFF) go in one UniChar; astral plane scalars (emoji etc.)
    /// need UTF-16 surrogate pair encoding.
    private static func postUnicodeScalar(
        _ scalar: Unicode.Scalar,
        source: CGEventSource?,
        flags: CGEventFlags = []
    ) throws {
        var units: [UniChar] = []
        if scalar.value <= 0xFFFF {
            units = [UniChar(scalar.value)]
        } else {
            // UTF-16 surrogate pair for code points beyond the BMP.
            let v = scalar.value - 0x10000
            let high = UniChar(0xD800 + (v >> 10))
            let low  = UniChar(0xDC00 + (v & 0x3FF))
            units = [high, low]
        }

        guard let down = CGEvent(
            keyboardEventSource: source,
            virtualKey: 0,
            keyDown: true
        ) else {
            throw InputError.postFailed
        }
        down.flags = flags
        units.withUnsafeBufferPointer { buf in
            down.keyboardSetUnicodeString(
                stringLength: buf.count,
                unicodeString: buf.baseAddress
            )
        }
        down.post(tap: .cghidEventTap)

        guard let up = CGEvent(
            keyboardEventSource: source,
            virtualKey: 0,
            keyDown: false
        ) else {
            throw InputError.postFailed
        }
        up.flags = flags
        units.withUnsafeBufferPointer { buf in
            up.keyboardSetUnicodeString(
                stringLength: buf.count,
                unicodeString: buf.baseAddress
            )
        }
        up.post(tap: .cghidEventTap)
    }
}
