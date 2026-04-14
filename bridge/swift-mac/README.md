# providence-mac-bridge

Swift subprocess that exposes fast macOS APIs (ScreenCaptureKit, CGEvent, AX tree) to the Go-based Providence TUI over JSON-over-stdio.

## Why

The Go harness can't link AppKit/ScreenCaptureKit/ApplicationServices directly. This bridge spawns as a long-lived subprocess, accepts line-delimited JSON RPC calls on stdin, and streams responses + unsolicited events back on stdout. One subprocess per session - warm ScreenCaptureKit pipeline stays hot for the life of the session.

## Build

```sh
swift build -c release
```

Binary lands at `.build/release/providence-mac-bridge`. Ad-hoc sign for dev:

```sh
codesign --force --deep --sign - .build/release/providence-mac-bridge
```

Or use the Makefile:

```sh
make build     # build + ad-hoc codesign
make install   # copy to ~/.providence/bin/
```

## Install path

Go side lookup order:

1. `cfg.Bridge.SwiftPath` (config override)
2. `$XDG_DATA_HOME/providence/providence-mac-bridge`
3. sibling of the main `providence` binary
4. `~/.providence/bin/providence-mac-bridge`
5. `/usr/local/lib/providence/providence-mac-bridge`

If none resolve the Go side logs once and degrades to the shell fallback for the session.

## Protocol

Newline-delimited JSON. Every request carries an `id`; responses echo the same `id`. Unsolicited events carry no `id` (they use an `event` field instead).

Request envelope:

```json
{"id":"req-01HX","method":"screenshot","params":{"region":{"x":0,"y":0,"w":800,"h":600}}}
```

Success response:

```json
{"id":"req-01HX","ok":true,"result":{"path":"/tmp/providence-screenshot-1234.jpg","w":2560,"h":1440,"capture_ns":8421312,"sha1":"abc..."}}
```

Error response:

```json
{"id":"req-01HX","ok":false,"error":{"code":"permission_denied","message":"...","url":"x-apple.systempreferences:...","remediable":true}}
```

Event (no id):

```json
{"event":"focus_changed","data":{"pid":1234,"bundle":"com.apple.finder"},"ts_ns":123456789}
```

### Error codes

`permission_denied`, `unsupported_os`, `bad_request`, `element_not_found`, `timeout`, `capture_failed`, `focus_changed`, `internal`.

## Phase 1 scope

Only the following methods are wired:

- `ping` - returns `{pong:true, version, protocol_version}`. Handshake sanity check.
- `preflight` - returns the status of `screen_recording` and `accessibility` permissions without prompting.
- `screenshot` / `screenshot_region` - writes a JPEG (quality 0.75) to `/tmp/providence-screenshot-<ns>.jpg`, returns path + dimensions + elapsed capture ns + SHA-1 of JPEG bytes.
- `shutdown` - acks then `exit(0)` after stdout flush.

Any other method returns `error.code = "bad_request"` with `message = "method not implemented yet: <name>"`. Phase 2-5 wire input, AX, diff, batch, metrics.

## Phase 2 scope

Phase 2 adds: `click`, `double_click`, `right_click`, `type_text`, `key_combo`. Input methods require Accessibility permission.

- `click` - posts `.leftMouseDown/.leftMouseUp` (or `right`/`middle` via `button` param) at `(x, y)`. Supports `count` (double/triple clicks set `.mouseEventClickState`), `modifiers` (`cmd`, `shift`, `option`, `control`, `fn`), and `settle_ms` (cursor move settle delay, default 50ms).
- `double_click` - same params as `click`, caller's `count` is ignored and forced to 2.
- `right_click` - same params as `click`, `button` is forced to `"right"`.
- `type_text` - injects `text` via `CGEvent.keyboardSetUnicodeString` with virtualKey=0. Minimum 8ms inter-keystroke delay; `delay_ms` can bump it higher. Handles emoji / astral-plane scalars via UTF-16 surrogate pair encoding.
- `key_combo` - posts `keyDown`/`keyUp` for `virtual_code` (HIToolbox kVK_*) with `modifiers` held. If `virtual_code == -1`, falls back to Unicode injection of the `key` string under modifier flags. Fails with `bad_request` if neither is provided.

Error mapping: CGEvent post failures return `capture_failed` with a hint about Accessibility. Invalid `key_combo` shape returns `bad_request`.

## Minimum macOS

- **12.0** floor (`Package.swift` platform). On 12.0-12.2 the bridge falls back to `CGWindowListCreateImage` for screenshots.
- **12.3+** uses the `SCStream` warm pipeline (~40ms p50 screenshot on M-series).
- **14.0+** prefers `SCScreenshotManager.captureImage` for one-shot capture (faster, no warm stream needed).

## Smoke tests

```sh
# ping
echo '{"id":"test-1","method":"ping","params":null}' \
  | .build/release/providence-mac-bridge

# preflight (reports permission state, never prompts)
echo '{"id":"test-2","method":"preflight","params":null}' \
  | .build/release/providence-mac-bridge

# screenshot (requires Screen Recording granted; otherwise returns permission_denied)
echo '{"id":"test-3","method":"screenshot","params":null}' \
  | .build/release/providence-mac-bridge
```

EOF on stdin (e.g. `< /dev/null`) exits the process cleanly with status 0.

## Layout

```
Sources/
  providence-mac-bridge/   # executable
    main.swift             # entry
    Protocol.swift         # Request/Response/Event + AnyCodable
    IOLoop.swift           # NDJSON read/write, serial write queue
    Dispatcher.swift       # per-capability queue routing
    Capture.swift          # SCStream warm pipeline + legacy fallback
    Permissions.swift      # CGPreflight... + AXIsProcessTrusted... (no prompts)
  ProvidenceCaptureKit/    # SPM library, placeholder for phase 6
    module.swift
    SCStreamController.swift   # TODO phase 6
```

## Notes

- Warm stream runs at 2 fps idle (`minimumFrameInterval = 1/2s`). Phase 4 will flip this to burst 30 fps on demand.
- Providence's own windows are excluded from capture via `SCContentFilter(excludingApplications:)` matched on PID.
- JPEG bytes are SHA-1'd per capture so the Go side can skip re-reading unchanged frames.
