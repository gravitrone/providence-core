import Foundation
import ApplicationServices
import CoreGraphics

struct PermissionStatus: Codable {
    let permission: String      // "screen_recording" | "accessibility"
    let granted: Bool
    let settingsURL: String
    let hint: String
}

enum Permissions {
    /// Phase 1: read-only. Does NOT prompt; TCC prompt is wired in a later
    /// `prompt_permissions` method.
    static func check() -> [PermissionStatus] {
        var out: [PermissionStatus] = []

        // Screen Recording.
        let srGranted = CGPreflightScreenCaptureAccess()
        out.append(PermissionStatus(
            permission: "screen_recording",
            granted: srGranted,
            settingsURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
            hint: srGranted
                ? ""
                : "Open System Settings > Privacy & Security > Screen Recording and enable Providence."
        ))

        // Accessibility. Non-prompting check.
        let axOptions = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: false] as CFDictionary
        let axGranted = AXIsProcessTrustedWithOptions(axOptions)
        out.append(PermissionStatus(
            permission: "accessibility",
            granted: axGranted,
            settingsURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
            hint: axGranted
                ? ""
                : "Open System Settings > Privacy & Security > Accessibility and enable Providence."
        ))

        return out
    }
}
