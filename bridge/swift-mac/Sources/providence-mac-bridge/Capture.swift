import Foundation
import CoreGraphics
import CoreMedia
import CoreImage
import ImageIO
import AppKit
import UniformTypeIdentifiers
import CommonCrypto

#if canImport(ScreenCaptureKit)
@preconcurrency import ScreenCaptureKit
#endif

// MARK: - SHA-1 helper (outside CaptureEngine so legacy path can reach it)

enum CaptureHash {
    static func sha1Hex(_ data: Data) -> String {
        var digest = [UInt8](repeating: 0, count: Int(CC_SHA1_DIGEST_LENGTH))
        data.withUnsafeBytes { ptr in
            _ = CC_SHA1(ptr.baseAddress, CC_LONG(data.count), &digest)
        }
        return digest.map { String(format: "%02x", $0) }.joined()
    }
}

// MARK: - Public value types

struct CapturedImage {
    let path: String
    let width: Int
    let height: Int
    /// Nanoseconds elapsed between request receipt and JPEG write completion.
    let captureNs: UInt64
    /// SHA-1 of the JPEG bytes (for dedup / change detection).
    let sha1: String
}

struct BridgeError: Error {
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
}

// MARK: - ScreenCaptureKit engine (macOS 12.3+)

@available(macOS 12.3, *)
final class CaptureEngine: NSObject, SCStreamOutput, SCStreamDelegate, @unchecked Sendable {
    private let ciContext = CIContext(options: [.useSoftwareRenderer: false])
    private let bufferQ = DispatchQueue(label: "bridge.capture.buffer")
    private let outputQ = DispatchQueue(label: "bridge.capture.output")

    // State guarded by bufferQ.
    private var stream: SCStream?
    private var latest: CMSampleBuffer?
    private var latestFrameNs: UInt64 = 0
    private var warm = false
    private var warming = false
    private var warmDisplayID: CGDirectDisplayID = 0
    private var warmContentSize: CGSize = .zero

    private var firstFrameContinuations: [CheckedContinuation<Void, Error>] = []

    /// Capture a single frame. If a region is provided the full-screen image is
    /// cropped to that CG coordinate rect.
    func captureOnce(region: CGRect? = nil, startNs: UInt64) async throws -> CapturedImage {
        // Permission preflight: if Screen Recording is denied, fail fast with
        // a remediable error. Do NOT prompt in Phase 1.
        if !CGPreflightScreenCaptureAccess() {
            throw BridgeError(
                code: ErrorCode.permissionDenied,
                message: "Screen Recording permission not granted for this process.",
                url: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
                remediable: true
            )
        }

        // Prefer the one-shot API on macOS 14+; it's faster and doesn't need a warm stream.
        if #available(macOS 14.0, *) {
            if let img = try await captureOneShot(region: region, startNs: startNs) {
                return img
            }
            // Fall through to warm-stream path if one-shot failed for any reason.
        }

        // Warm-stream path (macOS 12.3+).
        try await ensureWarm()

        // Grab the latest frame. If none yet, wait briefly.
        var sample: CMSampleBuffer?
        var retries = 0
        while sample == nil && retries < 20 {
            sample = self.readLatest()
            if sample != nil { break }
            try await Task.sleep(nanoseconds: 25_000_000) // 25ms
            retries += 1
        }
        guard let sb = sample, let px = CMSampleBufferGetImageBuffer(sb) else {
            throw BridgeError(code: ErrorCode.captureFailed,
                              message: "no frames yet from warm stream")
        }
        let ci = CIImage(cvPixelBuffer: px)
        return try writeJPEG(ciImage: ci, region: region, startNs: startNs)
    }

    private func readLatest() -> CMSampleBuffer? {
        var out: CMSampleBuffer?
        bufferQ.sync { out = self.latest }
        return out
    }

    @available(macOS 14.0, *)
    private func captureOneShot(region: CGRect?, startNs: UInt64) async throws -> CapturedImage? {
        let content: SCShareableContent
        do {
            content = try await SCShareableContent.excludingDesktopWindows(false,
                                                                           onScreenWindowsOnly: true)
        } catch {
            return nil
        }
        guard let display = content.displays.first else {
            throw BridgeError(code: ErrorCode.captureFailed, message: "no displays available")
        }

        let filter = SCContentFilter(
            display: display,
            excludingApplications: selfApps(content: content),
            exceptingWindows: []
        )

        let cfg = SCStreamConfiguration()
        cfg.width = display.width * Int(NSScreen.main?.backingScaleFactor ?? 1)
        cfg.height = display.height * Int(NSScreen.main?.backingScaleFactor ?? 1)
        cfg.pixelFormat = kCVPixelFormatType_32BGRA
        cfg.showsCursor = true

        let cgimage: CGImage
        do {
            cgimage = try await SCScreenshotManager.captureImage(contentFilter: filter,
                                                                 configuration: cfg)
        } catch {
            return nil
        }

        let ci = CIImage(cgImage: cgimage)
        return try writeJPEG(ciImage: ci, region: region, startNs: startNs)
    }

    /// Start the warm stream if not already running and wait for the first frame.
    func ensureWarm() async throws {
        // Fast path: already warm with frames.
        if readLatest() != nil { return }

        // Start if needed and wait for the first frame.
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
            bufferQ.async {
                if self.latest != nil { cont.resume(); return }
                self.firstFrameContinuations.append(cont)
                if !self.warming && !self.warm {
                    self.warming = true
                    self.bufferQ.async {
                        Task {
                            do {
                                try await self.startStream()
                                self.bufferQ.async { self.warming = false }
                            } catch {
                                self.bufferQ.async {
                                    self.warming = false
                                    let waiters = self.firstFrameContinuations
                                    self.firstFrameContinuations.removeAll()
                                    for c in waiters { c.resume(throwing: error) }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    private func startStream() async throws {
        let content = try await SCShareableContent.excludingDesktopWindows(false,
                                                                           onScreenWindowsOnly: true)
        guard let display = content.displays.first else {
            throw BridgeError(code: ErrorCode.captureFailed, message: "no displays available")
        }

        let filter = SCContentFilter(
            display: display,
            excludingApplications: selfApps(content: content),
            exceptingWindows: []
        )

        let cfg = SCStreamConfiguration()
        let scale = Int(NSScreen.main?.backingScaleFactor ?? 1)
        cfg.width = display.width * max(scale, 1)
        cfg.height = display.height * max(scale, 1)
        cfg.pixelFormat = kCVPixelFormatType_32BGRA
        cfg.showsCursor = true
        cfg.minimumFrameInterval = CMTime(value: 1, timescale: 2) // 2 fps idle

        let s = SCStream(filter: filter, configuration: cfg, delegate: self)
        try s.addStreamOutput(self, type: .screen, sampleHandlerQueue: outputQ)
        try await s.startCapture()

        bufferQ.async {
            self.stream = s
            self.warm = true
            self.warmDisplayID = CGDirectDisplayID(display.displayID)
            self.warmContentSize = CGSize(width: display.width, height: display.height)
        }
    }

    /// Filter out Providence's own app(s) from the capture so our windows don't
    /// end up in our own screenshots.
    private func selfApps(content: SCShareableContent) -> [SCRunningApplication] {
        let myPID = NSRunningApplication.current.processIdentifier
        return content.applications.filter { $0.processID == myPID }
    }

    func tearDown() {
        bufferQ.async {
            if let s = self.stream {
                Task { try? await s.stopCapture() }
            }
            self.stream = nil
            self.latest = nil
            self.warm = false
        }
    }

    // MARK: SCStreamDelegate

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        bufferQ.async {
            self.warm = false
            self.stream = nil
        }
    }

    // MARK: SCStreamOutput

    func stream(_ stream: SCStream,
                didOutputSampleBuffer sampleBuffer: CMSampleBuffer,
                of type: SCStreamOutputType) {
        guard type == .screen else { return }
        guard CMSampleBufferIsValid(sampleBuffer) else { return }
        // Copy the sample buffer pointer; ARC keeps the underlying pixel buffer alive.
        bufferQ.async {
            self.latest = sampleBuffer
            self.latestFrameNs = IOLoop.nowNs()
            // Wake anyone waiting for the first frame.
            if !self.firstFrameContinuations.isEmpty {
                let waiters = self.firstFrameContinuations
                self.firstFrameContinuations.removeAll()
                for c in waiters { c.resume() }
            }
        }
    }

    // MARK: - JPEG encoding

    private func writeJPEG(ciImage: CIImage, region: CGRect?, startNs: UInt64) throws -> CapturedImage {
        let cropped: CIImage
        if let region = region {
            // CIImage origin is bottom-left; callers pass top-left-origin CGRect.
            // Flip by subtracting from extent height.
            let ext = ciImage.extent
            let flippedY = ext.height - region.origin.y - region.height
            let rr = CGRect(x: region.origin.x, y: flippedY,
                            width: region.width, height: region.height).integral
            cropped = ciImage.cropped(to: rr).transformed(
                by: CGAffineTransform(translationX: -rr.origin.x, y: -rr.origin.y)
            )
        } else {
            cropped = ciImage
        }

        let colorspace = CGColorSpace(name: CGColorSpace.sRGB) ?? CGColorSpaceCreateDeviceRGB()

        guard let cg = ciContext.createCGImage(cropped, from: cropped.extent,
                                               format: .RGBA8, colorSpace: colorspace) else {
            throw BridgeError(code: ErrorCode.captureFailed, message: "CIContext createCGImage failed")
        }

        let nsBitmap = NSBitmapImageRep(cgImage: cg)
        guard let jpeg = nsBitmap.representation(
            using: .jpeg,
            properties: [.compressionFactor: 0.75]
        ) else {
            throw BridgeError(code: ErrorCode.captureFailed, message: "JPEG encode failed")
        }

        let ns = IOLoop.nowNs()
        let path = "/tmp/providence-screenshot-\(ns).jpg"
        let url = URL(fileURLWithPath: path)
        try jpeg.write(to: url, options: .atomic)

        let sha1Hex = CaptureHash.sha1Hex(jpeg)
        let elapsed = ns &- startNs

        return CapturedImage(
            path: path,
            width: cg.width,
            height: cg.height,
            captureNs: elapsed,
            sha1: sha1Hex
        )
    }
}

// MARK: - Legacy capture (macOS 12.0 - 12.2)

enum LegacyCapture {
    static func captureOnce(region: CGRect?, startNs: UInt64) throws -> CapturedImage {
        if !CGPreflightScreenCaptureAccess() {
            throw BridgeError(
                code: ErrorCode.permissionDenied,
                message: "Screen Recording permission not granted for this process.",
                url: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
                remediable: true
            )
        }

        let rect: CGRect = region ?? .infinite
        guard let cg = CGWindowListCreateImage(rect, .optionOnScreenOnly,
                                               kCGNullWindowID, []) else {
            throw BridgeError(code: ErrorCode.captureFailed,
                              message: "CGWindowListCreateImage returned nil")
        }

        let bitmap = NSBitmapImageRep(cgImage: cg)
        guard let jpeg = bitmap.representation(using: .jpeg,
                                               properties: [.compressionFactor: 0.75]) else {
            throw BridgeError(code: ErrorCode.captureFailed, message: "JPEG encode failed")
        }

        let ns = IOLoop.nowNs()
        let path = "/tmp/providence-screenshot-\(ns).jpg"
        try jpeg.write(to: URL(fileURLWithPath: path), options: .atomic)

        let elapsed = ns &- startNs
        return CapturedImage(
            path: path,
            width: cg.width,
            height: cg.height,
            captureNs: elapsed,
            sha1: CaptureHash.sha1Hex(jpeg)
        )
    }
}
