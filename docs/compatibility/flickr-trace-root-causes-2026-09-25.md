# Flickr: Mimic Error Root Cause Audit

> **Reference scope, September 25, 2026.** An ordinary Chrome session showed a
> nonempty `cf-turnstile-response`, including after refresh, in retained DevTools
> screenshots. An extension-based read returned an empty value and did not expose
> that session's exact network traffic. Frozen Chrome 152 and Mimic produced no
> token within 90 seconds in separate automated captures. These observations use
> different environments and do not form a matched success/failure comparison.

Date: September 25, 2026. Reviewed commit: `059a25dc8094a9860959f3f739d6424de561c6cf`. Reference: frozen Chrome **152.0.7977.82**, Windows, Playwright/CDP.

**The initial audit made no implementation changes.** It produced diagnostic materials and this report. Later fixes are described in the addendum below.

## Result

The four original unhandled page errors were attributed to several independent browser-semantic defects. They were not four Turnstile errors. The failures affect Weglot, Webflow, and consent manager loading. Reducing the cases exposed additional defects at the same boundaries.

Main reasons: incorrect URL serialization; loss of canonical DOM identity when transitioning between realms; DOM checking via overridable getter; local storage of Event state; calling public `setAttribute` from reflected script properties; selecting a random, sometimes not yet initialized realm for Trusted Types.

| Observation after loading Flickr | Mimic | Chrome 152 |
|---|---|---|
| Home page | HTTP 200, readyState complete | HTTP 200, readyState complete |
| Unhandled JS errors in baseline capture | 4 causes/failure sites, 8 exception records | None |
| Weglot | undefined | object |
| turnstile | undefined | object |
| Field cf-turnstile-response | Missing | Yes, value length 0 |
| Network loadingFailed | None in baseline capture | DNS and ORB; details below |

**Successful token acquisition not confirmed for any browser.** Chrome loads Turnstile, but in the control capture there are two `ERR_NAME_NOT_RESOLVED` for `brunhild.challenges.cloudflare.com` and Turnstile message `600010`. Separately, TrustArc is blocked by `ERR_BLOCKED_BY_ORB`. The exact cause of the DNS failure, the relationship to the service solution, and the ORB policy were not determined in this audit. Fixing Mimic bugs found does not in itself prove passing the test.

## Verification method and boundaries

1. Performed independent basic Flickr captures in Mimic and frozen Chrome; CDP events, document/script bodies, page state and internal Mimic trace are saved.
2. To extract the stack, diagnostic wrappers have been added to the captured scripts. Such runs are clearly decoupled from the base: wrappers can influence execution.
3. For each root cause, abbreviated local checks were performed in both browsers and compared with the sources.
4. All categories of errors, unsupported and semantic-missing in the base trace were checked. This is an audit of this implementation and associated shorthand examples, not a statement that all Web APIs or all branches of the site have been audited.

The capture waited 15 seconds after DOMContentLoaded. The absence of a token relates to this observation. The complete external resources of the two live downloads may differ; inferences about semantics are based on locally identical examples.

## What the trace contains

In `.build/flickr-audit/baseline-mimic/trace.json`:

| Category | Number | Interpretation |
|---|---:|---|
| exception | 8 | Four errors, each recorded twice |
| error | 4 | Parser script tasks execution errors |
| semantic-missing | 83 | 82 CSS.fontSizeResolution and 1 HTMLAnchorElement.type |
| unsupported | 38 | Mainly reading missing properties and checking libraries; not 38 missing APIs |

Initial crashes: `NotFoundError` when loading Weglot; subsequent call to `Weglot.initialize` when undefined; `Illegal invocation` when starting Webflow; stack overflow in the SOE/Osano chain. The instrumented runs additionally caught the alternative early failure of Trusted Types.

## Page causal chain

```text
URL without trailing / → Weglot decides a URL polyfill is needed
  → script creation/configuration/insertion through Osano wrappers
    → Trusted Types may select an uninitialized isolated world → TypeError
    or
    → borrowed parentNode from iframe changes canonical DOM wrappers
      → prepareInsertion compares wrappers → false NotFoundError
  → Weglot is not installed → Weglot.initialize fails secondarily

Corrupted BODY wrapper + realm-local Event state
  → Webflow dispatchEvent(ix2-animation-started) → Illegal invocation

SOE sets script.async → public setAttribute → Osano wrapper
  → script.async again → recursion → RangeError
```

Neither `Weglot.initialize` nor the absence of a Turnstile field is a sufficient diagnosis on its own. In basic Mimic, page initialization is already interrupted before Turnstile is observed to load in Chrome.

## Confirmed defects

### 1. HTTP URL with empty path is serialized without `/`

**Where:** `internal/browser/url_host.go`, functions resolveURL/urlParts/setURLPart; `internal/webapi/fetch_primitives.js`, URL implementation.

Host uses `net/url` and returns `u.String()`/`u.EscapedPath()` without normalizing the empty special URL path to Chrome's observed behavior. JS saves the received value.

`new URL('http://weglot.com').href`: Mimic `http://weglot.com`, Chrome `http://weglot.com/`. After changing search, the difference remains. URLSearchParams in this shortened example works in both browsers.

In Weglot's live diagnostics, the list of missing features is `["URL"]`, after which the URL-polyfill is selected. This means that the problem is not the absence of a URL constructor, but its behavior. Validation: `weglot-url-feature`.

### 2. Returning DOM from another realm overwrites the canonical wrapper

**Where:** `internal/webapi/surface.js`: cachedDOMParent about 1581; Node.parentNode about 2979; wrap about 4539; branch nodeId in unwrapCrossRealm around 7356.

The borrowed getter parentNode performs a wrap in its realm. When returning a node, unwrapCrossRealm writes a proxy to `elementWrappers` over the existing nodeId, replacing the local canonical object.

After calling the `iframe.contentWindow.Node.prototype.parentNode` getter on a local child, Mimic fails three identity checks: the returned parent against the original, the usual `child.parentNode` against the original, and `getElementById` against the original. All three identities hold in Chrome. A subsequent `insertBefore` fails in Mimic.

Osano does borrow Node.parentNode from its iframe. In the Weglot capture before insertion, both parents are named HEAD, but `sameParent=false`, `sameHead=false`, with the original first child being the same. This is an identity error, not the actual absence of a reference node. Validation: `borrowed-parent-canonical-identity`.

### 3. The insertBefore check depends on the public parentNode

**Where:** `internal/webapi/surface.js:3132`, prepareInsertion, compare about 3151.

The resource/script insertion path checks `before.parentNode !== parent`. This reads an overridable JS property instead of checking membership in the authoritative DOM by identifiers.

It is enough to define a real child’s own getter parentNode, which returns null: inserting a script before it into Mimic gives NotFoundError, Chrome inserts successfully. This independent defect also reinforces problem #2. Validation: `insert-before-overridden-parent`.

### 4. dispatchEvent does not accept Event from another realm

**Where:** `internal/webapi/events_compatibility.js:19`, stateOf; dispatchEventCore about 354; dispatchEvent about 480.

The state is looked up via realm-local `eventSlots.get(event)`. A valid Event from another realm does not have an entry in this table and is rejected as an Illegal invocation.

Three checks - foreign event/local target, local event/foreign target, borrowed dispatch/main event - fail in Mimic and are successful in Chrome.

In the live Webflow capture, dispatching **ix2-animation-started** on **BODY** fails: the event belongs to the local realm, but the target no longer passes the local `instanceof Node` check. The stack reaches `stateOf` through a foreign `Proxy.dispatchEvent`. Wrapper observations confirm the connection with defect #2. This interrupts Webflow animation startup; it is not a Turnstile API call.

### 5. async/defer calls the overridden setAttribute and creates a recursion

**Where:** `internal/webapi/surface.js:4103` and `:4112`, setters HTMLScriptElement.async/defer.

Setter calls `this.setAttribute(...)`. Osano wraps setAttribute and reflective properties: setting an async/defer attribute calls the corresponding property. A cycle arises between the native-looking setter Mimic and the library wrapper.

Minimal check with overridden setAttribute: Mimic calls hook once and does not set the attribute; Chrome doesn't call the hook at all and exposes the property/attribute. Check with recursion limiter: Mimic six calls to artificial guard, Chrome zero. The full SOE stack repeats this chain until RangeError.

setupConsentManager crashes when installing script.async before adding the consent manager is completed. Checks: `script-async-public-hook`, `script-defer-public-hook`, both `*-recursion`.

### 6. Trusted Types selects an arbitrary document realm, including the lazy world

**Where:** `internal/browser/trusted_types.go:95`, trustedTypesOwner; `internal/browser/debugger_worlds.go`, isolatedWorld; `internal/browser/deferred_runtime.go`; `internal/webapi/trusted_types.js:387`.

The owner is selected by the first match in the Go `realmOwners` map for root/arena. Different isolated worlds of the same document satisfy the condition. A lazy world may not have installed `trustedTypesEnforcer` yet. An empty value is encoded as a cross-realm descriptor. JS checks the descriptor's truthiness and tries to call the unwrapped result, which is `undefined`.

Hence `TypeError: unwrapCrossRealm(...) is not a function`. The error was caught in live Weglot on the path trustedConvert → trustedAttributeValue → Proxy.setAttribute.

Local loop borrowed setAttribute for src: 83 successes and 17 errors out of 100. Controlled experiment with six lazy isolated worlds: 3 successes and 97 errors; after initializing all relevant worlds - 100 successes. Chrome: 100 successes in both stages. The numbers describe a specific run, not a fixed probability: the order of the map is not guaranteed.

### 7. Borrowed Document.createElement creates an object in the realm of the method

**Where:** `internal/webapi/document_compatibility.js:155–187`; `internal/webapi/surface.js`, Document.createElement around 4925.

The wrapper first calls original, which creates an object via local host/wrap, then adopt changes the ownership of the document. This does not fix the prototype realm of an already created object.

`iframe.contentWindow.Document.prototype.createElement.call(document, 'div')`: ownerDocument and insertion are correct, but the prototype in Mimic belongs to the iframe. Chrome returns the prototype of the main document. Validation: `borrowed-create-element`.

Found in reducing the realm problem; A separate fall of live Flickr has not been proven by this point. The general wrapper covers other create methods, but the result is only given for the measured createElement.

### 8. getElementsByTagName checks realm via instanceof

**Where:** `internal/webapi/document_compatibility.js:256–259`.

Checking `this instanceof Document/Element` against a realm function rejects a valid Document of another realm. Borrowed getElementsByTagName.call(mainDocument, 'head') gives Illegal invocation in Mimic; Chrome returns the correct head. Validation: `borrowed-tags-head`.

This is a nearby confirmed defect found by a local test. It cannot be mixed with the live Illegal invocation Webflow: it has a different stack, described in No. 4.

### 9. Font size JS resolver does not resolve viewport units

**Where:** `internal/webapi/css_font_metrics.js:18`, cssResolveLength; cssComputedFontSize about 140; `internal/webapi/css_computed_values.js:387`.

Resolver can do absolute units, em/rem/% and the calc part, but not vw. The Flickr root formula `calc(0.017733564013841074rem + 1.3840830449826986vw)` returns null, creating 82 semantic-missing entries. Dependent paths substitute 16 px in places.

On an initial blank document, this formula and a pure vw give 16 px; Chrome with a width of 1280 gives respectively 18 px and the correct viewport value. On a loaded HTTP document, the native producer takes priority - that’s why the error looks different there, see No. 10. These are two ways to calculate observed font size with different results.

### 10. Native font-size inherits Stylo quantization, which is different from Chrome

**Where:** `internal/webapi/surface.js:2092`, blitzStyleValue; `internal/layoutblitz/native/src/lib.rs`, style/style_batch; pinned blitz `19a72d4a4163038d748603fbcf709d38bfa6a998`, resolved_style.rs; dependency `stylo-0.21.0/values/specified/length.rs:947`, `font.rs:1023`.

Stylo pre-truncates the viewport result in app units, and then `FontSize::quantize_font_size` discards 14 bits of the f32 mantissa, leaving 10 bits of precision. Mimic returns the serialized result of this producer as computed style.

With width=1280 on a loaded HTTP document:

| Input font-size | Mimic | Chrome |
|---|---|---|
| 18px | 18px | 18px |
| 1.40625vw | 18px | 18px |
| 1.3840830449826986vw | 17.6875px | 17.7163px |
| 0.017733564013841074rem | 0.283691px | 0.283737px |
| Flickr formula from #9 | 17.9688px | 18px |
| calc from two px terms with sum 18 | 18px | 18px |

The checks are saved in font-matrix-http.json. This is a numerical result inconsistency, not a proven cause of JS crash or Turnstile failure.

### 11. Serializing a given font-size preserves the original numbers/calc

**Where:** `internal/webapi/webkit_css.js:73`, normalizeCSSValue; `internal/webapi/surface.js`, CSSStyleDeclaration.setProperty/getPropertyValue.

For checked font-size values, the normalization path returns the original string without a specialized parser/serializer. Therefore style.fontSize preserves long fractions, but `calc(0.2837370242214572px + 17.716262975778544px)` remains the same. Chrome returns the normalized numbers (`1.38408vw`, `0.0177336rem`) and `calc(18px)` respectively.

This is an independent observable CSSOM difference, confirmed by both font-matrix, with no proven connection to the site crash.

### 12. HTMLAnchorElement.type exists as a stub

**Where:** `internal/webapi/surface.js:4377`, HTMLAnchorElement; generic accessor fallback around 8912/8931.

The class has no reflective implementation of type; a generic getter/setter is installed that calls semanticMissing. Before setting the attribute, reading gives undefined instead of the empty string. After setAttribute('type','text/html') the property is still undefined; writing a property does not change the attribute. Chrome reflects both sides correctly. There is one HTMLAnchorElement.type entry in the trace. Validation: `anchor-type`.

### 13. Parser-script errors bypass Window errors and lose structure in CDP

**Where:** `internal/browser/realm.go`, evaluateClassicScript about 457 and Evaluate about 554; `internal/browser/page.go` about 792; `internal/cdp/server.go`, transform trace.Exception; `internal/engine/v8/spike.go`, exceptionError; `internal/webapi/window_errors.js`.

The script path does not call the existing `reportWindowException`. The error is recorded separately as an evaluation and a script error, and CDP publishes both records. The exception is reduced to a string first, losing the Error object, stackTrace, and full coordinates/context.

On one inline `throw new Error('audit-uncaught-script')`:

- Mimic: window.error handler not called; two Runtime.exceptionThrown; line/column are 0; no structured stack/exception.
- Chrome: one window.error and one Runtime.exceptionThrown with object, stack and actual column.

This explains the empty error array of the diagnostic init-script when the page actually crashed and the doubling of the original four exceptions. Validation: `parserScriptErrorReporting`. The missing DOMException.stack does not apply here: it is undefined in both Chrome and Mimic.

### 14. Explicit navigation to about:blank is not implemented in this path

**Where:** `internal/browser/page.go:490-496`, beginNavigationRequestWithCommit.

When preparing an additional CSS check, `page.goto('about:blank')` returned `unsupported navigation scheme "about"`. The reason is obvious: entry point only accepts http/https. A new empty Page still exists and allows JS to be executed; initial document creation and explicit navigation use different paths.

This is a separate unsupported navigation boundary, not a cause of the original Flickr errors. The CSS measurement used a new Page and a local HTTP document without changing the implementation.

## What should not be considered missing APIs found

Unsupported included documentMode, Document.namespaceURI, globalPrivacyControl, script.node, document.document/type/uniqueID, div.type and dynamic jQuery/sizzle fields. Checked fixed properties return undefined in frozen Chrome too. The lack of library expando before installing it also does not prove a missing Web API.

Therefore, it would be incorrect to fill all 38 records with stubs. The actual HTMLAnchorElement.type is kept separate because it behaves differently from Chrome. The remaining entries are left as diagnostic observations, without declaring unconfirmed defects.

## Priorities for future fixes

This is the order of consideration, not the changes made:

1. Canonical DOM identity and choice of realm owner, including Trusted Types.
2. DOM checks based on the authoritative state and Event transmission between realms.
3. Reflected script properties without calling custom override; URL serialization.
4. Error pipeline: window.error, a single CDP event, and preserving Error/stack/context.
5. Other borrowed DOM methods, CSS-resolvers/serialization and anchor.type.
6. Separately, the about:blank navigation border.

Future changes should be tested with reduced cases against frozen Chrome and a repeat unmodified Flickr capture. Obtaining a Turnstile token should remain a separate verification result; the presence of an API, field, or HTTP 200 does not replace it.

## Evidence and reproduction

All paths below are relative to the repository root. `.build` - local ignored materials, not part of the commit report. Full captures may contain network identifiers; their values ​​are not transferred to the report.

| Material | Path |
|---|---|
| Basic Route and Mimic Answers | `.build/flickr-audit/baseline-mimic/` |
| Control Chrome | `.build/flickr-audit/baseline-chrome/` |
| Trusted Types Stack | `.build/flickr-audit/dom-instrumented-mimic/capture.json` |
| Weglot URL and DOM Insertion | `.build/flickr-audit/detailed-instrumented-mimic/capture.json` |
| Webflow Live Event | `.build/flickr-audit/detailed-event-instrumented-mimic/capture.json` |
| Short comparisons | `.build/flickr-audit/probes-mimic.json`, `probes-chrome.json` |
| CSS, new empty Page | `.build/flickr-audit/font-matrix.json` |
| CSS, HTTP document | `.build/flickr-audit/font-matrix-http.json` |
| Diagnostic scripts | `.build/flickr_audit.cjs`, `.build/flickr_probes.cjs`, `.build/flickr_font_audit.cjs` |

Commands for repeating basic checks when Mimic is running on 127.0.0.1:9223:

```powershell
node .build/flickr_probes.cjs mimic
node .build/flickr_probes.cjs chrome
node .build/flickr_font_audit.cjs
node .build/flickr_audit.cjs mimic repeat-mimic
node .build/flickr_audit.cjs chrome repeat-chrome
```

Trusted Types numbers may change between runs. The local font script in its current form repeats the HTTP matrix; the original empty Page matrix is ​​saved as a separate file. Live-capture depends on external resources and the network. Production code, frozen harnesses and reference expectations have not changed.

## Addendum: implementation and re-testing

Since the initial audit, defects #1-14 have been fixed in the general URL, DOM/realm, Event, Reflected Attributes, Trusted Types, CSS, Error Passing, and Navigation mechanisms. Added or performed short regressions to `internal/browser/flickr_semantics_test.go` and existing tests. This does not change the historical base capture results above.

Unhandled Weglot, Webflow, and Osano errors stopped occurring on the reloaded page. `window.turnstile` and one response field appear. The next difference involved the `IntersectionObserver` lifecycle: Webflow starts Turnstile for a form only when it enters the observed region. After the first shadow tree was created, Mimic switched **the entire** page from native layout to fallback JS layout. On Flickr, this incorrectly changed the coordinates of lower forms and initialized two extra widgets. For a zero-size shadow host without ordinary child nodes, the fix limits that switch to the affected branch; hosts that can change document flow retain the page-wide fallback calculation. Mimic and frozen Chrome then each create one widget for the search form; the two subscription forms remain outside the observed region. The upper subscription form measured `y=2764.56` in Mimic and `y=2758.53` in Chrome; the lower form measured `y=4278.06` and `y=4270.61`, respectively. Existing focused Shadow DOM, CSSOM, and IntersectionObserver checks pass after the fix.

In a separate 90-second network trace, after corrections, Mimic shows one field with a blank value in all 18 measurements. The page does not throw any raw JS errors. There is `600010` in the Turnstile console (Cloudflare refers to the `600*` family as a general validation failure, without publicly decoding the suffix; see [official code table](https://developers.cloudflare.com/turnstile/troubleshooting/client-side-errors/error-codes/)); four calls to `brunhild.challenges.cloudflare.com` failed with a DNS error. In a separate capture, frozen Chrome 152 also did not receive a token and encountered this DNS failure. Since a separate ordinary Chrome session on the same page received a token, these automatic captures do not prove that a bug in Mimic's implementation is causing the Turnstile failure. The live network and successful session context of regular Chrome were not available through the extension-based observation.

Replays: `.build/flickr_io_audit.cjs`, `.build/flickr-audit/io-audit.txt`, `.build/flickr-audit/challenge-mimic/trace.json` and `.build/flickr-audit/challenge-chrome/trace.json`. Token values ​​were not transferred to the report.
