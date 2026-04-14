import Foundation
import ApplicationServices
import AppKit

// MARK: - Wire types

struct AXQueryParams: Codable {
    let app: String?
    let pid: Int?
    let role: String?
    let title: String?
    let text: String?
    let contains_text: String?
    let descendant_of: String?
    let max_results: Int?
    let mode: String?
    let max_depth: Int?
    let include_invisible: Bool?
}

struct AXFindResult: Codable {
    let matches: [AXNode]
}

// MARK: - Match mode

enum MatchMode {
    case exact, substring, fuzzy

    init(_ s: String?) {
        switch s?.lowercased() {
        case "exact": self = .exact
        case "substring": self = .substring
        default: self = .fuzzy
        }
    }
}

// MARK: - Querier

enum AXQuerier {
    /// Walk the AX tree from a start element (app root or a cached
    /// descendant) and return the top-N matches ranked by score.
    /// Short-circuits when enough high-confidence matches are found.
    static func find(_ q: AXQueryParams) throws -> AXFindResult {
        let startEl: AXUIElement
        var resolvedPID: Int?

        if let did = q.descendant_of, let el = AXCache.shared.lookup(did) {
            startEl = el
        } else if q.descendant_of != nil {
            throw BridgeError(
                code: ErrorCode.elementNotFound,
                message: "no element: \(q.descendant_of ?? "") (possibly stale after cache invalidation)"
            )
        } else {
            let pid = try resolveTargetPID(app: q.app, pid: q.pid)
            resolvedPID = pid
            startEl = AXUIElementCreateApplication(pid_t(pid))
            AXUIElementSetMessagingTimeout(startEl, 1.5)
        }
        _ = resolvedPID // suppress unused warning when descendant_of path taken

        let maxResults = max(1, q.max_results ?? 1)
        let mode = MatchMode(q.mode)
        let maxDepth = max(1, q.max_depth ?? 15)
        let includeInvisible = q.include_invisible ?? false
        let deadline = Date().addingTimeInterval(1.5)
        let generation = AXCache.shared.currentGeneration

        var matches: [(node: AXNode, score: Double)] = []
        walkForMatches(
            startEl,
            depth: 0,
            maxDepth: maxDepth,
            q: q,
            mode: mode,
            includeInvisible: includeInvisible,
            generation: generation,
            deadline: deadline,
            matches: &matches,
            target: maxResults
        )

        matches.sort { $0.score > $1.score }
        let top = matches.prefix(maxResults).map { result -> AXNode in
            var n = result.node
            n.score = result.score
            return n
        }
        return AXFindResult(matches: Array(top))
    }

    // MARK: - Walker

    private static func walkForMatches(
        _ el: AXUIElement,
        depth: Int,
        maxDepth: Int,
        q: AXQueryParams,
        mode: MatchMode,
        includeInvisible: Bool,
        generation: UInt64,
        deadline: Date,
        matches: inout [(node: AXNode, score: Double)],
        target: Int
    ) {
        if Date() >= deadline { return }

        // Read attributes once per node - we reuse them for both scoring
        // and constructing the returned AXNode on match.
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
            x: Int(pos.x), y: Int(pos.y),
            w: Int(size.width), h: Int(size.height)
        )

        let isZeroFrame = (frame.w == 0 || frame.h == 0)
        let visible = includeInvisible || !(hidden || (depth > 0 && isZeroFrame))

        if visible {
            let score = scoreNode(
                q: q, mode: mode,
                role: role, subrole: subrole,
                title: title, label: label,
                value: value, placeholder: placeholder
            )
            if score > 0 {
                let id = AXCache.shared.store(el, generation: generation)
                let actions = axActionNames(el)
                let node = AXNode(
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
                    actions: actions.isEmpty ? nil : actions,
                    children: nil,
                    score: nil
                )
                matches.append((node: node, score: score))

                // Short-circuit: enough high-confidence matches.
                let strong = matches.filter { $0.score >= 0.9 }.count
                if strong >= target { return }
            }
        }

        // Recurse into children.
        if depth < maxDepth {
            for child in axChildren(el) {
                if Date() >= deadline { return }
                walkForMatches(
                    child,
                    depth: depth + 1,
                    maxDepth: maxDepth,
                    q: q,
                    mode: mode,
                    includeInvisible: includeInvisible,
                    generation: generation,
                    deadline: deadline,
                    matches: &matches,
                    target: target
                )
                let strong = matches.filter { $0.score >= 0.9 }.count
                if strong >= target { return }
            }
        }
    }

    // MARK: - Scoring

    /// Combine role / title / text / contains_text signals into a single
    /// score in [0, 1.2]. A node with score > 0 is a candidate; sorting by
    /// score surfaces the best match first.
    ///
    /// Breakdown:
    /// - exact title/label/value match: 1.0
    /// - word-boundary substring match: 0.7
    /// - fuzzy (Levenshtein <= 2):      max(0, 0.5 - d*0.1)
    /// - role match alone:              +0.2
    /// - contains_text substring:       +0.1
    ///
    /// If no query criteria are supplied, every visible node scores 0.1
    /// so callers can list-walk without specifying filters.
    static func scoreNode(
        q: AXQueryParams,
        mode: MatchMode,
        role: String,
        subrole: String?,
        title: String?,
        label: String?,
        value: String?,
        placeholder: String?
    ) -> Double {
        var score: Double = 0
        var anyCriterion = false

        // Role filter: if q.role is set, must match role or subrole.
        if let r = q.role, !r.isEmpty {
            anyCriterion = true
            let rl = r.lowercased()
            let matched = role.lowercased() == rl
                || role.lowercased() == ("ax" + rl)
                || (subrole?.lowercased() == rl)
            if !matched { return 0 }
            score += 0.2
        }

        // Text match against title / label / value. q.text is the catch-all;
        // q.title is title-specific.
        let haystacks: [String] = [title, label, value, placeholder].compactMap { $0 }

        if let needle = q.text, !needle.isEmpty {
            anyCriterion = true
            let s = bestTextScore(needle: needle, haystacks: haystacks, mode: mode)
            if s <= 0 { return 0 }
            score += s
        }
        if let needle = q.title, !needle.isEmpty {
            anyCriterion = true
            let s = bestTextScore(needle: needle, haystacks: [title ?? ""], mode: mode)
            if s <= 0 { return 0 }
            score += s
        }
        if let needle = q.contains_text, !needle.isEmpty {
            anyCriterion = true
            let nl = needle.lowercased()
            let hit = haystacks.contains { $0.lowercased().contains(nl) }
            if !hit { return 0 }
            score += 0.1
        }

        if !anyCriterion {
            // No filter - every visible node is a weak candidate.
            return 0.1
        }
        return score
    }

    /// Max scoring across all haystacks under the chosen mode.
    private static func bestTextScore(
        needle: String,
        haystacks: [String],
        mode: MatchMode
    ) -> Double {
        var best: Double = 0
        let nl = needle.lowercased()
        for h in haystacks {
            let hl = h.lowercased()
            if hl == nl {
                best = max(best, 1.0)
                continue
            }
            switch mode {
            case .exact:
                if hl == nl { best = max(best, 1.0) }
            case .substring:
                if hl.contains(nl) {
                    // Word-boundary substring beats middle-of-word.
                    if isWordBoundaryMatch(haystack: hl, needle: nl) {
                        best = max(best, 0.7)
                    } else {
                        best = max(best, 0.5)
                    }
                }
            case .fuzzy:
                if hl.contains(nl) {
                    if isWordBoundaryMatch(haystack: hl, needle: nl) {
                        best = max(best, 0.7)
                    } else {
                        best = max(best, 0.5)
                    }
                } else {
                    let d = levenshtein(nl, hl)
                    if d <= 2 {
                        best = max(best, max(0, 0.5 - Double(d) * 0.1))
                    }
                }
            }
        }
        return best
    }

    /// True if `needle` appears at a word boundary in `haystack`.
    private static func isWordBoundaryMatch(haystack: String, needle: String) -> Bool {
        if haystack.hasPrefix(needle) { return true }
        // Any space-delimited token starts with needle.
        for token in haystack.split(whereSeparator: { !$0.isLetter && !$0.isNumber }) {
            if String(token).hasPrefix(needle) { return true }
        }
        return false
    }

    /// Classic Levenshtein edit distance. Bounded - we only care about
    /// distances <= 2, so we bail early on longer strings.
    private static func levenshtein(_ a: String, _ b: String) -> Int {
        let diff = abs(a.count - b.count)
        if diff > 2 { return diff }
        let ac = Array(a)
        let bc = Array(b)
        if ac.isEmpty { return bc.count }
        if bc.isEmpty { return ac.count }

        var prev = [Int](0...bc.count)
        var curr = [Int](repeating: 0, count: bc.count + 1)
        for i in 1...ac.count {
            curr[0] = i
            var rowMin = i
            for j in 1...bc.count {
                let cost = ac[i - 1] == bc[j - 1] ? 0 : 1
                curr[j] = min(
                    prev[j] + 1,        // deletion
                    curr[j - 1] + 1,    // insertion
                    prev[j - 1] + cost  // substitution
                )
                if curr[j] < rowMin { rowMin = curr[j] }
            }
            // Early exit: if every cell in this row exceeds 2, no path <=2
            // exists through subsequent rows.
            if rowMin > 2 { return rowMin }
            swap(&prev, &curr)
        }
        return prev[bc.count]
    }
}
