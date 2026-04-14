import Foundation
import ApplicationServices
import AppKit

/// Stable-ID cache for AXUIElement references.
///
/// AXUIElement values are CF references to elements in remote app processes.
/// They stay valid for the current "generation" - roughly, until focus changes
/// or a window/tab cycles. We hand out opaque string IDs like `gen3-a1b2c3d4`
/// so the Go side can reference an element across RPC boundaries (e.g. call
/// `ax_find` then `ax_perform` on one of the results).
///
/// When focus changes we bump the generation and drop every cached element -
/// the Go side sees an `ax_invalidated` event and knows its IDs are stale.
final class AXCache: @unchecked Sendable {
    static let shared = AXCache()

    /// Closure invoked on every cache invalidation. Set by `main.swift` to
    /// forward an `ax_invalidated` event to the Go side via IOLoop.
    var onInvalidate: ((pid_t?) -> Void)?

    private var generation: UInt64 = 0
    private var byID: [String: AXUIElement] = [:]
    private var observers: [pid_t: AXObserver] = [:]
    private let q = DispatchQueue(label: "bridge.axcache", qos: .userInitiated)

    var currentGeneration: UInt64 { q.sync { generation } }

    /// Store an element under a newly-minted ID scoped to the current
    /// generation. The ID is stable for this process until the next
    /// invalidation.
    func store(_ el: AXUIElement) -> String {
        q.sync {
            let id = "gen\(generation)-\(Self.shortUUID())"
            byID[id] = el
            return id
        }
    }

    /// Store with a caller-known generation. Used by the tree walker so all
    /// nodes emitted in a single walk share the same gen prefix even if a
    /// concurrent invalidate fires mid-walk.
    func store(_ el: AXUIElement, generation: UInt64) -> String {
        q.sync {
            let id = "gen\(generation)-\(Self.shortUUID())"
            byID[id] = el
            return id
        }
    }

    func lookup(_ id: String) -> AXUIElement? {
        q.sync { byID[id] }
    }

    /// Invalidate the cache. Bumps generation, drops all stored IDs, and
    /// fires the `ax_invalidated` event so callers can discard stale IDs.
    func invalidate(pid: pid_t?) {
        q.sync {
            generation &+= 1
            byID.removeAll()
        }
        onInvalidate?(pid)
    }

    // MARK: - Observers

    /// Install focus / activation observers. Best-effort: some apps don't
    /// cooperate, some require Accessibility permission to observe at all.
    /// We register for workspace-level activation changes which always fire,
    /// and try per-app AXObservers for finer-grained focus changes.
    func installObservers() {
        // Workspace-level: new app becomes frontmost.
        NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification,
            object: nil,
            queue: nil
        ) { [weak self] note in
            let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
            self?.invalidate(pid: app?.processIdentifier)
        }

        // Workspace-level: new app launches - register a per-app observer.
        NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didLaunchApplicationNotification,
            object: nil,
            queue: nil
        ) { [weak self] note in
            guard let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
            else { return }
            self?.registerObserver(for: app.processIdentifier)
        }

        // Register observers for already-running apps.
        for app in NSWorkspace.shared.runningApplications where app.bundleIdentifier != nil {
            registerObserver(for: app.processIdentifier)
        }
    }

    /// Register an AXObserver that invalidates the cache on focus-window
    /// change for a given app. Silently no-ops if AX permission is missing
    /// or the app rejects observation.
    private func registerObserver(for pid: pid_t) {
        q.sync {
            if observers[pid] != nil { return }

            var observerRef: AXObserver?
            let cbSelf = Unmanaged.passUnretained(self).toOpaque()
            let err = AXObserverCreate(pid, AXCache.axCallback, &observerRef)
            guard err == .success, let observer = observerRef else { return }

            let appEl = AXUIElementCreateApplication(pid)

            // Focused window change is the main trigger we care about.
            // Some apps reject these with .cannotComplete, which we ignore.
            _ = AXObserverAddNotification(
                observer,
                appEl,
                kAXFocusedWindowChangedNotification as CFString,
                cbSelf
            )
            _ = AXObserverAddNotification(
                observer,
                appEl,
                kAXFocusedUIElementChangedNotification as CFString,
                cbSelf
            )
            _ = AXObserverAddNotification(
                observer,
                appEl,
                kAXApplicationActivatedNotification as CFString,
                cbSelf
            )

            CFRunLoopAddSource(
                CFRunLoopGetMain(),
                AXObserverGetRunLoopSource(observer),
                .defaultMode
            )
            observers[pid] = observer
        }
    }

    // MARK: - C callback trampoline

    /// AXObserver C-function callback. Pulls the cache back out of the
    /// opaque refcon pointer, then bumps generation. We can't read the
    /// element's PID directly from the callback cheaply, so we pass nil
    /// and let the Go side treat it as a whole-tree invalidation.
    private static let axCallback: AXObserverCallback = { _, _, _, refcon in
        guard let refcon = refcon else { return }
        let cache = Unmanaged<AXCache>.fromOpaque(refcon).takeUnretainedValue()
        cache.invalidate(pid: nil)
    }

    // MARK: - ID minting

    private static func shortUUID() -> String {
        String(UUID().uuidString.replacingOccurrences(of: "-", with: "").prefix(8)).lowercased()
    }
}
