# Live development preview

Start Mimic with `-dev-preview`, then open
`http://127.0.0.1:9222/debug/preview/` in a browser. Select the CDP target;
`?target=<target-id>` selects it directly. Refresh targets after creating or
closing a target. The viewer is an observation surface, not page automation:
drive the real Page over CDP. The viewer includes a passive viewport and a live
activity panel. Light, dark and system themes are available; the choice is saved
locally in the viewer browser. The theme changes only the viewer's interface.

The mirrored viewport keeps the target's dimensions. Fit scales it to the
available space; explicit zoom levels let users inspect a larger view by scrolling
the surrounding workspace. Neither option resizes the Mimic target. A shield,
disabled pointer hit testing and an inert iframe prevent viewer mouse/keyboard
input, selection and author hover effects. Scrolling the workspace moves the
visual surface; page scrolling and form input remain under automation control.
Restoring mirrored modal state preserves the viewer's keyboard focus.
An overlay shows observed CDP mouse coordinates and press pulses.

Activity streams commands as they are dispatched, command errors, Page lifecycle,
network requests/responses/failures, formatted console text and exceptions for the
selected target. Command dispatch is not a success/completion assertion. Logs
start when the viewer connects; earlier activity is not replayed. The latest 500
entries are retained, with category filters, text search, expandable details,
clear and follow-latest controls. Pause view freezes the visual snapshot while
activity continues; resume applies the latest pending snapshot. A new connection
resumes the view, and selecting another target clears the previous target's log
and pointer overlay. Discovery and WebSocket reconnection recover automatically
after a local server restart.

The viewer displays the current DOM as an ordinary styled website, using the
viewer's browser for rendering. It does not invoke Mimic's separate
[approximate screenshot renderer](approximate-screenshots.md), require a GPU,
or add a Chromium runtime dependency. This visual DOM mirror is not a screenshot
or proof that Mimic's geometry matches the viewer browser's layout.

The implementation reuses the existing canonical DOM snapshot projection,
including live form values, declarative shadow roots and adopted stylesheets.
Document CSSOM rules are projected as inline styles with relocated resource URLs.
Attached iframe documents are embedded as snapshots. Scripts, event attributes,
active embeds and form/navigation actions are removed; the viewer additionally
uses a sandbox without script permission and a script-free document CSP. Only
`allow-same-origin` is granted so the parent viewer can patch the mirrored DOM;
`allow-scripts` is never granted. Viewer actions do not drive the Mimic Page or
execute its JavaScript. Inert `noscript` fallbacks are omitted: the viewer's
script-disabled sandbox must not activate markup hidden in the source Page.

The Page command boundary checks existing DOM/resource, CSSOM, focus and viewport
revisions only while a viewer is subscribed. Debug-only form setter tracking
covers value changes that do not mutate attributes. A changed revision generates
one immutable HTML update over a separate WebSocket. There is no additional
polling timer; the existing CDP event loop supplies turn boundaries. A pending
publication coalesces turns for 50 ms without restarting its delay, so continuous
page activity cannot starve the initial snapshot or subsequent updates. A one-item
mailbox replaces stale updates so slow network writes do not block Page execution.
The wire still carries full immutable HTML snapshots. The viewer coalesces pending
updates per animation frame and reconciles the existing DOM by realm/node IDs.
Unchanged attributes, images, styles and nested documents are retained. Scroll,
focus and selection survive ordinary updates; a new document resets scroll.
The iframe gets one empty bootstrap document, not a new `srcdoc` per update.
Snapshot work itself runs under the Page command lock.

Activity uses the Page's existing filtered trace subscription without enabling
API/property tracing or creating a debugger session or request interceptor.
It has a separate, nonblocking 256-entry mailbox, so visual coalescing cannot
erase commands and a slow viewer cannot stall Page execution. Overflow is
reported explicitly. Activity fields and console text are bounded; request
headers, response bodies and debugger object handles are not sent to the viewer.
Viewer subscriptions are removed on disconnect and server shutdown.

Dialog top-layer membership is projected separately from the `open` attribute.
The viewer restores native modal state after reconciling the connected tree, so
`:modal`, backdrops and centering apply without running site scripts in the mirror.
Closing a mirrored dialog also removes the viewer's top-layer membership.

Without `-dev-preview`, debug HTTP/WebSocket routes are not registered, debug JS
is excluded from the realm bootstrap, no debug form tracking is installed, and
no preview serialization, revision reads, resource requests or network messages
occur. The command unlock path has only a nil observer check. Even in dev mode,
without viewers no preview snapshots or version reads are performed. Bootstrap
cache keys include the debug source to keep dev and ordinary realms separate.

Images and fonts are loaded by the viewer browser from their original URLs.
They do not use Mimic's resource loader or credentials; protected resources,
cross-origin font restrictions and Page-local blob URLs may therefore differ or
be unavailable. Canvas/WebGL pixels and live video are not exported. Shadow
stylesheet CSSOM changes and shadow-contained iframe mapping are not fully
projected. Fonts, browser CSS support and layout can differ from Mimic's modeled
observations. This preview does not claim pixel equivalence to Chrome 152.

Keep the debug listener on loopback unless its DOM contents are intended to be
accessible to other users of that listener. Viewer WebSockets enforce same-origin
browser connections; the existing CDP listener's access model is otherwise unchanged.

Browser regression: serve `internal/cdp` with a local static server and open
`testdata/dev_preview_dom_test.html`. It checks repeated updates, node/document
identity, nested iframe and shadow content, scroll, focus, selection, identical
updates, script isolation and navigation.
It also checks modal centering, persistence across updates, closing and reopening.
`tools/compatibility/preview_dom_smoke.py` runs it in a temporary Chrome 152 context.

The same static server also serves `testdata/dev_preview_ui_test.html`. This uses
the real embedded client with a deterministic test transport and checks passive
hit testing, hover isolation, input overlays, safe activity text, category filters,
search, pause/resume, stable mirror identity, themes, scaling, bounded retention,
clearing, target changes and mobile layout. It requires no live Chrome oracle
capture or external site. Focused server coverage is `go test ./internal/cdp
-run TestDevPreview -count=1`.
