# Read-only CDP CSS support

The SDK integration gate found that chromedp enables `DOM` and `CSS` before
navigating an attached target. Mimic now supports the read-only CSS domain
lifecycle and `CSS.getComputedStyleForNode` through the document owner's
existing CSSOM projection. There is no independent CDP style model, injected
page helper or invocation of author-replaceable `getComputedStyle` methods.

The frozen Chrome 152 implementation at Chromium commit
`d04cdb24d67b081f6cf80200ffc5233f44b61109` provides the command prerequisites:
[`InspectorCSSAgent::enable`, `disable` and `getComputedStyleForNode`](https://github.com/chromium/chromium/blob/d04cdb24d67b081f6cf80200ffc5233f44b61109/third_party/blink/renderer/core/inspector/inspector_css_agent.cc).
Enable requires the DOM agent; computed-style reads require the CSS agent;
disable is idempotent. No new Chrome capture was launched for this change.

The implementation is partial. Stylesheet inventory, stylesheet/font/change
notifications, rule editing, rule coverage, matched-rule inspection and exact
Blink property inventory/order remain unsupported. Computed values inherit the
existing runtime CSSOM support boundaries. The read-only projection includes
modeled standard properties and inherited custom properties. It does not claim
full DevTools CSS agent compatibility.

`TestCSSReadOnlyDomainUsesCanonicalStyle` covers command prerequisites,
idempotent enable/disable, per-session isolation, canonical stylesheet and
inline mutations, custom properties, stale node errors and immunity to
author replacements of computed-style methods.
