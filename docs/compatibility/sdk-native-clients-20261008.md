# Native automation integration

Mimic SDK adapters use real Playwright, Puppeteer, PuppeteerSharp, chromedp, Rod,
chromiumoxide, Ferrum and chrome-php objects. Framework integration relies on
the shared runtime state and protocol boundaries described below.

## Context and capture ownership

`Mimic.configureContext` atomically installs the current profile, proxy,
resource policy and optional media configuration into an empty ordinary Context.
This lets a framework retain its actual Context object after a short, owned
blank-page probe resolves its public CDP identity. Existing, initializing and
closing Pages, retained uploads and active captures prevent the transition.
Validation failure leaves the existing identity unchanged. The resulting
environment profile is immutable; idempotent font/media initialization is
accepted without changing its state.

Media discovery uses that Context's explicit ID before configuration. Private
source IDs are Context-bound, while public device IDs, labels, groups and
capabilities come from the logical media profile. `tools/sdk-media-fixture`
supplies deterministic camera/microphone providers for SDK regression tests; it
never opens hardware. Source B produces blue camera frames and a 660 Hz PCM tone,
distinct from source A. The existing media implementation remains authoritative.

`TestConfigureEmptyFrameworkContext` covers atomic configuration, ownership and
managed-profile initialization. `TestConfigureProfileWaitsForRetainedUploads`
covers teardown with an upload still using the old transport. SDK media tests
also cover source discovery, native capture, public identity, missing devices,
error privacy, failed factories and release of capture workers.

## Protocol projections

| Native client failure | Shared correction | Focused evidence |
| --- | --- | --- |
| Playwright sometimes discarded a runtime context received before its frame tree | Preserve Page command arrival order; Promise waits and asynchronous navigation yield their turn, and independent sessions remain concurrent | `TestPipelinedPageCommandsPreserveFrameContextOrder`, `TestQueuedPageCommandsDoNotBlockIndependentSessions`, `TestCDPAwaitPromiseAllowsSameSessionResolverAndMultipleWaiters` |
| Puppeteer retained a closed nested Page after its tab detached | Publish child detach notifications before their parent | `TestDetachNotifiesNestedSessionsBeforeParent` |
| Resizing an ordinary context failed when a separate managed context existed | Assign modeled windows per Context and route bounds by window ID | `TestBrowserWindowBoundsStayWithinOwningContext` |
| Strict clients rejected string connection IDs | Project opaque transport identities into stable numeric CDP IDs | `TestNetworkConnectionNumericIdentity` |
| chromedp could not resolve selector results in a shallow frontend DOM tree | Publish the existing ancestor path before returning matching node IDs | `TestDOMQueriesPublishAncestorPathBeforeReply` |
| Ferrum waited indefinitely for native navigation completion | Project loading start/stop from canonical frame/navigation state, including failure and explicit stop, without fabricating a load event | `TestFrameLoadingEventsFollowCanonicalNavigation`, `TestFrameStoppedLoadingOnExplicitStopWithoutSyntheticLoad`, `TestFailedNavigationStopsFrameLoading` |

Loading transitions and their event publication remain ordered even when
cancellation occurs outside the Page turn. A stale loader cannot finish a newer
navigation, and trace callbacks can reenter without holding the Page state lock.
`TestFrameLoadingPublicationPreservesConcurrentTransitions` covers that boundary.

Read-only CSS support and document/frame projections have separate evidence in
[CDP CSS](cdp-css-readonly-20261008.md) and
[document projection](cdp-document-projection-20261008.md). The semantic support
registry lists remaining limitations: one modeled window per Context, new Page
profile bounds, incomplete DOM frontend remapping and the existing CSSOM scope.
Generated wire shapes do not imply complete Chrome or framework compatibility.

## Availability and validation

The released v0.2.2 runtime does not contain all of the integration behavior
described here. Adapters requiring these changes need a compatible runtime
release. Consult the SDK support matrix before choosing a client.

Focused tests listed above cover the protocol and ownership boundaries.
Compatibility qualification retains exact runtime/fixture hashes, client versions
and artifact identities as local or CI evidence. Runtime and SDK releases remain
separate; runtime releases do not automatically bump SDK versions or rewrite
user pins.
