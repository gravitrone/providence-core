import Foundation

// providence-mac-bridge entry point.
//
// Reads newline-delimited JSON requests from stdin, dispatches them to the
// per-capability handlers, and writes responses/events to stdout. Exits 0 on
// stdin EOF.

let dispatcher = Dispatcher()
let loop = IOLoop(dispatcher: dispatcher)
dispatcher.ioLoop = loop

// Wire AX cache invalidation events back to the IO loop so the Go side
// learns that previously-issued element IDs are stale.
AXCache.shared.onInvalidate = { [weak loop] pid in
    var data: [String: AnyCodable] = [:]
    if let pid = pid {
        data["pid"] = AnyCodable(Int(pid))
    }
    loop?.emitEvent(Event(
        event: "ax_invalidated",
        data: AnyCodable(data),
        ts_ns: IOLoop.nowNs()
    ))
}
AXCache.shared.installObservers()

loop.run()
