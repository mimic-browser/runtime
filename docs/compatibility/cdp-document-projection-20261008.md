# CDP document and frame projection

Real chromedp navigation kept its old DOM root because `DOM.documentUpdated`
was absent. Real chromiumoxide 0.9.1 rejected `Page.Frame` because required
security fields were absent, leaving native Page initialization pending.

The retained Chrome 152 source revision
`d04cdb24d67b081f6cf80200ffc5233f44b61109` provides the primary control:

- [InspectorDOMAgent](https://github.com/chromium/chromium/blob/d04cdb24d67b081f6cf80200ffc5233f44b61109/third_party/blink/renderer/core/inspector/inspector_dom_agent.cc)
  invalidates frontend bindings for the inspected root after DOMContentLoaded
  and emits documentUpdated when enabled. A child commit has a narrower owner
  invalidation, not a whole-document replacement.
- [InspectorPageAgent](https://github.com/chromium/chromium/blob/d04cdb24d67b081f6cf80200ffc5233f44b61109/third_party/blink/renderer/core/inspector/inspector_page_agent.cc)
  projects secure-context explanation, isolation capability/permission and
  SharedArrayBuffer gating into the required Frame fields.

Mimic now emits the top-document invalidation from its existing canonical
lifecycle trace and derives frame security from the realm's existing security
and permissions-policy state. Neither path evaluates replaceable author
JavaScript or maintains a second browser state model. No browser capture or
headful launch was needed for these source-confirmed projection gaps.

`TestDocumentUpdatedAndFrameSecurityProjection` covers navigation, child-frame
exclusion, disabled-session suppression, refreshed DOM trees and local secure /
isolated frame projections. Full frontend node remapping, BFCache and
zero-document transitions remain explicit partial-support boundaries. Current
frontend node IDs are document-local and may be reused after navigation;
clients must discard their cached tree on the invalidation event.
