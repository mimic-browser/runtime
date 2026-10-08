# Scrolling observations

Scroll state belongs to the document's main realm. Isolated worlds and borrowed
element methods route to the owner; adopted elements use the current document.
Layout boxes stay in document coordinates. Client rectangles, CDP quads, input
hit tests and intersection observations apply viewport and ancestor offsets.
Scroll extents are derived from the existing box graph and cached for its epoch;
scrolling itself does not invalidate unchanged style/layout work.

Implemented paths include window/element scroll, scrollTo, scrollBy,
scrollLeft/scrollTop, scrollWidth/scrollHeight, scrollIntoView alignment and nearest
container selection, scroll margins/padding, overflow clipping, RTL offsets,
basic sticky positioning and transformed fixed containing blocks. Trusted wheel
and keyboard input perform default scrolling unless canceled; focus respects
preventScroll. CDP scroll-into-view invokes a private browser operation rather
than a replaceable author method, including optional rectangles and parent frames.
Fragment targets scroll after parsing or fragment navigation. Same-document
history traversal restores stored viewport offsets unless restoration is manual.

Smooth programmatic scrolling uses Page rendering tasks, supports interruption,
and emits coalesced scroll/scrollend events. The desktop curve and delta-based
duration follow Chromium's [curve implementation](https://chromium.googlesource.com/chromium/src/+/HEAD/cc/animation/scroll_offset_animation_curve.cc)
and [desktop parameters](https://chromium.googlesource.com/chromium/src/+/refs/tags/142.0.7420.0/cc/base/features.cc).
Endpoint, asynchronous completion and interruption checks were also run against
the installed Chrome 152.0.7977.83. Exact compositor frame timing and in-flight
compositor cancellation are not reproduced by the single Page event loop.

The dev preview projects canonical scroll changes. An unchanged projected offset
preserves the viewer's manual scrolling; a changed offset follows the Page.

## Boundaries

This extends the existing approximate geometry model, not a complete layout
engine. Scrollbar gutter layout, vertical writing modes, scroll snapping,
automatic scroll anchoring, touch/momentum scrolling and cross-document history
scroll restoration are not implemented by this change. Complex transformed
scroll chains and smooth scrolling across multiple frames retain geometry/timing
limitations. No pixel-equivalence or universal Chrome performance claim is made.

## Verification

The three fixtures in `internal/browser/scrolling_test.go` were executed directly
in Chrome 152; geometry, smooth/focus/frame, and RTL/sticky/container checks pass.
Focused scrolling, wheel cancellation, history/navigation, document compatibility,
geometry-root-access and CDP tests pass. Existing expectations were not weakened.
Playwright successfully clicked a local below-viewport button; a fresh Steam
login page passed a trial click on `a.login_create_btn` (scroll/actionability/hit
testing only, without navigation). These exploratory observations took 156 ms
and 912 ms respectively, not an isolated performance benchmark.

The first broad test run exposed two regressions (inert/XML document accessors
and unnecessary root getter reads). Both were fixed and focused tests rerun.
The repeat full suite did not complete; no
final full-suite pass or fresh performance-gate result is claimed. Local evidence
is under `.build/scroll-*`; the compiled executable is `.build/mimic-scrolling.exe`.
