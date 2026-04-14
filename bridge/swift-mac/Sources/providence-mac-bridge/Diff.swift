import Foundation
import CoreGraphics
import Accelerate
import AppKit

// MARK: - Wire types

struct ScreenDiffParams: Codable {
    let since_ts_ns: Int64?
    let max_regions: Int?
    let min_magnitude: Double?
}

struct ScreenDiffRegion: Codable {
    let x: Int
    let y: Int
    let w: Int
    let h: Int
    let magnitude: Double
}

struct ScreenDiffResult: Codable {
    let changed: Bool
    let hamming: Int
    let regions: [ScreenDiffRegion]?
    let full_hash: String
    let capture_ns: Int64

    enum CodingKeys: String, CodingKey {
        case changed, hamming, regions, full_hash, capture_ns
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(changed, forKey: .changed)
        try c.encode(hamming, forKey: .hamming)
        if let regions = regions { try c.encode(regions, forKey: .regions) }
        try c.encode(full_hash, forKey: .full_hash)
        try c.encode(capture_ns, forKey: .capture_ns)
    }
}

// MARK: - Frame memory

/// Rolling one-slot memory of the last hashed frame. Shared process-wide so
/// successive `screen_diff` calls compare against the previous snapshot.
final class FrameMemory: @unchecked Sendable {
    static let shared = FrameMemory()
    private let q = DispatchQueue(label: "bridge.frame.memory")
    private var lastHash: UInt64 = 0
    private var lastPixels: [UInt8] = []
    private var lastTS: Int64 = 0
    private var lastDims: (Int, Int) = (0, 0)
    private var hasBaseline: Bool = false

    func update(hash: UInt64, pixels: [UInt8], dims: (Int, Int), ts: Int64) {
        q.sync {
            self.lastHash = hash
            self.lastPixels = pixels
            self.lastDims = dims
            self.lastTS = ts
            self.hasBaseline = true
        }
    }

    struct Snapshot {
        let hash: UInt64
        let pixels: [UInt8]
        let dims: (Int, Int)
        let ts: Int64
        let hasBaseline: Bool
    }

    func snapshot() -> Snapshot {
        q.sync {
            Snapshot(
                hash: lastHash,
                pixels: lastPixels,
                dims: lastDims,
                ts: lastTS,
                hasBaseline: hasBaseline
            )
        }
    }
}

// MARK: - Diff

enum Diff {
    static let hashW = 9
    static let hashH = 8
    static let gridN = 64  // 64x64 grayscale for region diff
    static let changeThreshold: UInt8 = 20

    /// Convert a CGImage to a grayscale byte buffer of the given square size
    /// using vImage for speed. Returns a `gridN * gridN` buffer (row-major).
    static func toGrayscale(_ cg: CGImage, side: Int) -> [UInt8]? {
        guard let cs = CGColorSpace(name: CGColorSpace.genericGrayGamma2_2)
            ?? CGColorSpace(name: CGColorSpace.linearGray) else {
            return nil
        }
        let bytesPerRow = side
        var pixels = [UInt8](repeating: 0, count: side * side)
        let ctxOpt: CGContext? = pixels.withUnsafeMutableBytes { buf -> CGContext? in
            guard let base = buf.baseAddress else { return nil }
            return CGContext(
                data: base,
                width: side,
                height: side,
                bitsPerComponent: 8,
                bytesPerRow: bytesPerRow,
                space: cs,
                bitmapInfo: CGImageAlphaInfo.none.rawValue
            )
        }
        guard let ctx = ctxOpt else { return nil }
        ctx.interpolationQuality = .low
        ctx.draw(cg, in: CGRect(x: 0, y: 0, width: side, height: side))
        return pixels
    }

    /// Compute 64-bit dHash: resize to 9x8 grayscale, then for each row
    /// compute 8 bits where bit[i] = (pixel[i] > pixel[i+1]).
    static func dHash(_ cg: CGImage) -> UInt64 {
        // Use 9x8 grayscale buffer directly.
        guard let cs = CGColorSpace(name: CGColorSpace.genericGrayGamma2_2) else { return 0 }
        var buf = [UInt8](repeating: 0, count: hashW * hashH)
        let ctxOpt: CGContext? = buf.withUnsafeMutableBytes { p -> CGContext? in
            guard let base = p.baseAddress else { return nil }
            return CGContext(
                data: base,
                width: hashW,
                height: hashH,
                bitsPerComponent: 8,
                bytesPerRow: hashW,
                space: cs,
                bitmapInfo: CGImageAlphaInfo.none.rawValue
            )
        }
        guard let ctx = ctxOpt else { return 0 }
        ctx.interpolationQuality = .low
        ctx.draw(cg, in: CGRect(x: 0, y: 0, width: hashW, height: hashH))

        var hash: UInt64 = 0
        var bit: UInt64 = 0
        for row in 0..<hashH {
            for col in 0..<(hashW - 1) {
                let l = buf[row * hashW + col]
                let r = buf[row * hashW + col + 1]
                if l > r {
                    hash |= (1 << bit)
                }
                bit += 1
            }
        }
        return hash
    }

    /// Compute pixel buffer at 64x64 grayscale for region diff.
    static func lowResPixels(_ cg: CGImage) -> [UInt8] {
        return toGrayscale(cg, side: gridN) ?? []
    }

    static func hammingDistance(_ a: UInt64, _ b: UInt64) -> Int {
        (a ^ b).nonzeroBitCount
    }

    /// Connected-components + region assembly on the 64x64 grid.
    static func findRegions(
        lastPixels: [UInt8],
        currPixels: [UInt8],
        screenDims: (Int, Int),
        maxRegions: Int,
        minMagnitude: Double
    ) -> [ScreenDiffRegion] {
        let n = gridN
        guard lastPixels.count == n * n, currPixels.count == n * n else { return [] }

        // 1. Threshold.
        var changed = [Bool](repeating: false, count: n * n)
        for i in 0..<(n * n) {
            let a = Int(lastPixels[i])
            let b = Int(currPixels[i])
            if abs(a - b) > Int(changeThreshold) {
                changed[i] = true
            }
        }

        // 2. Connected components (8-neighbor flood fill).
        var labels = [Int](repeating: -1, count: n * n)
        var nextLabel = 0
        struct Comp {
            var minX: Int
            var minY: Int
            var maxX: Int
            var maxY: Int
            var count: Int
        }
        var comps: [Comp] = []

        var stack: [Int] = []
        stack.reserveCapacity(128)

        for start in 0..<(n * n) {
            if !changed[start] || labels[start] != -1 { continue }
            let label = nextLabel
            nextLabel += 1
            var comp = Comp(minX: n, minY: n, maxX: -1, maxY: -1, count: 0)
            stack.removeAll(keepingCapacity: true)
            stack.append(start)
            labels[start] = label
            while let idx = stack.popLast() {
                let x = idx % n
                let y = idx / n
                comp.count += 1
                if x < comp.minX { comp.minX = x }
                if y < comp.minY { comp.minY = y }
                if x > comp.maxX { comp.maxX = x }
                if y > comp.maxY { comp.maxY = y }
                for dy in -1...1 {
                    for dx in -1...1 {
                        if dx == 0 && dy == 0 { continue }
                        let nx = x + dx
                        let ny = y + dy
                        if nx < 0 || ny < 0 || nx >= n || ny >= n { continue }
                        let nIdx = ny * n + nx
                        if changed[nIdx] && labels[nIdx] == -1 {
                            labels[nIdx] = label
                            stack.append(nIdx)
                        }
                    }
                }
            }
            comps.append(comp)
        }

        if comps.isEmpty { return [] }

        // 3. Merge components within 2 cells of each other.
        //    Simple O(k^2) pass - k is small (usually < 32).
        var merged: [Comp] = []
        var used = [Bool](repeating: false, count: comps.count)
        for i in 0..<comps.count {
            if used[i] { continue }
            var m = comps[i]
            used[i] = true
            var changedAny = true
            while changedAny {
                changedAny = false
                for j in 0..<comps.count {
                    if used[j] { continue }
                    let c = comps[j]
                    // Distance between boxes (0 if overlapping/touching).
                    let dx = max(0, max(m.minX - c.maxX, c.minX - m.maxX))
                    let dy = max(0, max(m.minY - c.maxY, c.minY - m.maxY))
                    if dx <= 2 && dy <= 2 {
                        m.minX = min(m.minX, c.minX)
                        m.minY = min(m.minY, c.minY)
                        m.maxX = max(m.maxX, c.maxX)
                        m.maxY = max(m.maxY, c.maxY)
                        m.count += c.count
                        used[j] = true
                        changedAny = true
                    }
                }
            }
            merged.append(m)
        }

        // 4. Compute magnitude and build regions in screen coordinates.
        let (sw, sh) = screenDims
        // Guard against zero dims to avoid division issues.
        let scaleX = sw > 0 ? Double(sw) / Double(n) : 1.0
        let scaleY = sh > 0 ? Double(sh) / Double(n) : 1.0

        var regions: [ScreenDiffRegion] = []
        for c in merged {
            let boxW = (c.maxX - c.minX + 1)
            let boxH = (c.maxY - c.minY + 1)
            let total = boxW * boxH
            let mag = total > 0 ? Double(c.count) / Double(total) : 0
            if mag < minMagnitude { continue }
            let rx = Int((Double(c.minX) * scaleX).rounded(.down))
            let ry = Int((Double(c.minY) * scaleY).rounded(.down))
            let rw = Int((Double(boxW) * scaleX).rounded(.up))
            let rh = Int((Double(boxH) * scaleY).rounded(.up))
            regions.append(ScreenDiffRegion(x: rx, y: ry, w: rw, h: rh, magnitude: mag))
        }

        // 5. Sort by magnitude desc; trim; collapse overflow into one box.
        regions.sort { $0.magnitude > $1.magnitude }
        if regions.count > maxRegions {
            let keep = Array(regions.prefix(maxRegions - 1))
            let overflow = regions.suffix(from: maxRegions - 1)
            var minX = Int.max, minY = Int.max, maxX = Int.min, maxY = Int.min
            var avgMag = 0.0
            var k = 0
            for r in overflow {
                if r.x < minX { minX = r.x }
                if r.y < minY { minY = r.y }
                if r.x + r.w > maxX { maxX = r.x + r.w }
                if r.y + r.h > maxY { maxY = r.y + r.h }
                avgMag += r.magnitude
                k += 1
            }
            if k > 0 {
                let collapsed = ScreenDiffRegion(
                    x: minX, y: minY,
                    w: maxX - minX, h: maxY - minY,
                    magnitude: avgMag / Double(k)
                )
                return keep + [collapsed]
            }
            return keep
        }
        return regions
    }

    /// Main entry point: capture a fresh frame, hash, diff against memory.
    static func compute(params: ScreenDiffParams, capture: Any) async throws -> ScreenDiffResult {
        let t0 = IOLoop.nowNs()

        // Short-circuit on since_ts_ns if last frame is older than cutoff.
        let snap = FrameMemory.shared.snapshot()
        if let since = params.since_ts_ns, snap.hasBaseline, snap.ts < since {
            // Caller is asking "any change since ts"; if our last hashed frame
            // predates that cutoff we have nothing newer to report.
            return ScreenDiffResult(
                changed: false,
                hamming: 0,
                regions: nil,
                full_hash: String(format: "%016llx", snap.hash),
                capture_ns: 0
            )
        }

        let maxRegions = max(1, params.max_regions ?? 8)
        let minMag = params.min_magnitude ?? 0.02

        // Get a CGImage: prefer the macOS 12.3+ engine, else legacy path.
        let cg: CGImage
        if #available(macOS 12.3, *), let eng = capture as? CaptureEngine {
            cg = try await eng.captureCGImage()
        } else {
            cg = try legacyCGImage()
        }

        let screenDims = (cg.width, cg.height)
        let hash = dHash(cg)
        let pixels = lowResPixels(cg)
        let nowTs = Int64(IOLoop.nowNs())

        let result: ScreenDiffResult
        if !snap.hasBaseline {
            // First frame: no baseline to compare.
            result = ScreenDiffResult(
                changed: true,
                hamming: 0,
                regions: nil,
                full_hash: String(format: "%016llx", hash),
                capture_ns: Int64(IOLoop.nowNs() &- t0)
            )
        } else {
            let ham = hammingDistance(snap.hash, hash)
            // Hamming > 3 is a meaningful visual change on 64-bit dHash.
            let changed = ham > 3
            var regions: [ScreenDiffRegion]? = nil
            if changed, snap.pixels.count == pixels.count {
                let rs = findRegions(
                    lastPixels: snap.pixels,
                    currPixels: pixels,
                    screenDims: screenDims,
                    maxRegions: maxRegions,
                    minMagnitude: minMag
                )
                regions = rs
            } else if changed {
                regions = []
            }
            result = ScreenDiffResult(
                changed: changed,
                hamming: ham,
                regions: regions,
                full_hash: String(format: "%016llx", hash),
                capture_ns: Int64(IOLoop.nowNs() &- t0)
            )
        }

        FrameMemory.shared.update(hash: hash, pixels: pixels, dims: screenDims, ts: nowTs)
        return result
    }

    // Legacy (<12.3) CGImage capture using CGWindowListCreateImage.
    private static func legacyCGImage() throws -> CGImage {
        if !CGPreflightScreenCaptureAccess() {
            throw BridgeError(
                code: ErrorCode.permissionDenied,
                message: "Screen Recording permission not granted for this process.",
                url: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
                remediable: true
            )
        }
        guard let cg = CGWindowListCreateImage(.infinite, .optionOnScreenOnly,
                                               kCGNullWindowID, []) else {
            throw BridgeError(code: ErrorCode.captureFailed,
                              message: "CGWindowListCreateImage returned nil")
        }
        return cg
    }
}
