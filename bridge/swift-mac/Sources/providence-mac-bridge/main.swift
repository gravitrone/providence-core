import Foundation

// providence-mac-bridge entry point.
//
// Reads newline-delimited JSON requests from stdin, dispatches them to the
// per-capability handlers, and writes responses/events to stdout. Exits 0 on
// stdin EOF.

let dispatcher = Dispatcher()
let loop = IOLoop(dispatcher: dispatcher)
dispatcher.ioLoop = loop
loop.run()
