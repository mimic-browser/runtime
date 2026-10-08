# Flickr Turnstile: VM path after first `/fo` (2026-09-26)

## Trace findings

Mimic **reaches** the path that sends the second `/fo`. The hypothesis that the required branch does not execute was disproved. In ordinary Mimic captures, the first request receives a response, then a second request is sent about 11–16 seconds later. Frozen Chrome 152 sends a similar second request about a second later. In the retained successful Chrome Performance trace, the interval from completion of the first response to the second request was about 0.927 seconds. These sessions received different programs and are not a deterministic comparison of the same instructions. A repeat run of frozen Chrome 152 after instrumentation also ended with `600010` and an empty token, despite an interval of about 0.76 seconds. Thus, latency alone does not explain the difference between Mimic and the successful ordinary Chrome reference session.

In the live Mimic trace, the first `/fo` returned about 779 KB of `text/plain`, the second received a repeated large `text/plain`; the `cf-turnstile-response` field was left empty and Turnstile reported `600010`. In a successful user HAR, the first response is of the same size order, the second is 4208 bytes of `text/html`, after which the token appeared. Therefore, the immediate difference in the result lies in the server's response to the second request. It has not yet been determined which request field or environment property determined this response.

## Executed VM code

The original iframe inline script contains two VM dispatcher loops. Handler numbers retain their role between observed program variants, function names and specific code change. The diagnostic script instruments both loops at the command input and marks the calls to the handlers. After the first `/fo`, the nested VM is called via `Function.prototype.bind` and returns to the main loop again. In Mimic, three long sections were inside the command 176 handler of the first loop: approximately 2.1, 1.5 and 4.1 seconds in one run. **Instruction 176 is a general function call** and not a special decoder. The first two long calls go to the obfuscated helper, and the third calls `JSON.stringify` for `CSSStyleDeclaration`. In frozen Chrome 152 with the same diagnostics, 123,217 commands were recorded up to the second `/fo` versus 104,476 in Mimic; there were no long sections in processor 176. In another run of the same handler, Mimic took approximately 4.5, 1.3, and 4.4 seconds. The variable program and diagnostic load do not allow the speed coefficient of a single pure operation to be deduced, but the place of long execution is localized.

A successful user performance trace contains an iframe CPU profile. Between the first response and the second request, calls to `getImageData`, `appendChild` and VM functions were recorded. These are stack samples, not a complete trace of register and branch values. They confirm the execution of the graphical and DOM path, but do not in themselves prove why Mimic failed.

Separate instrumentation of `JSON.stringify` measured serialization of a styles object with 1213 enumerable properties: in Mimic about 4–4.5 s before corrections, in frozen Chrome 152 about 1.6–1.7 ms. The reason is CSSOM Mimic: the numeric index called `item()`, which re-created the array of all 476 calculated properties; reading named properties accessed the style engine separately. In the nested frame, batch reading and direct access to the numeric name directory reduced one measured call to about 2 s, but the total interval to the second request remained about 11 s. On a blank page, the same call dropped from about 247 ms to 37 ms. Additionally, Chrome 152 returns nine legacy `epub*` names in `Reflect.ownKeys(CSSStyleDeclaration)`, although their handles are missing; Mimic missed them before. The implementation now reproduces this observation.

An independent check on a blank page compared the JSON of the computed `body` style: of the 1213 enumerable fields, seven values ​​differ between Mimic and frozen Chrome 152. The differences relate to `height`/`blockSize` and their dependent `perspectiveOrigin`/`transformOrigin` alias names. This is a separate confirmed geometry gap; The current trace does not establish that the VM read the style `body` or that these seven values ​​were included in the second POST.

A previous narrow capture of the conditional command at offset 252 showed that its predicate after the first response is initially false, then becomes true and execution continues until the second `/fo` is sent. The observation is consistent with the second request actually sent. It does not prove that the serialized request body is equal to Chrome.

## Checking the reproduction of a successful response

The HTML iframe and the first response `/fo` of the last successful update come from the retained successful-session HAR. Substituting just HTML into another Chrome 152 session didn't get the VM to the first request. Substituting only the response into a new live iframe resulted in an `InvalidCharacterError` on `atob`: the response cannot be decoded with the context of another launch. Therefore, HAR is not a standalone oracle for step-by-step comparison of VM branches. Responses and session IDs are not included in the repository.

## What's left for causal inference

It remains unknown which field of the second request or environmental signal causes the server to fail. In the successful Chrome-provided HAR, the body of the second POST contains about 87 KB; A subsequent direct capture of the CDP `Network.requestWillBeSent` showed that Mimic was also passing the body of the second POST, although Playwright `request.postData()` returned empty for that connection. The contents of the bodies have not yet been matched by fields. The following boundaries are checked: VM input values ​​after the first response, browser API calls, and values ​​serialized into the second request. The difference in execution time does not in itself prove that the server failed due to a timeout. You cannot replace the response, token, or site logic in production Mimic based on this data.

Local diagnostic files are located in the ignored directory `.build/flickr-audit/vm-analysis/`; the main script is `.build/flickr_vm_trace.cjs`. The raw Performance trace and HAR remain in private local evidence storage.

## Successful frozen Chrome 152 and graphical discrepancies

A separate headful Chrome 152 launched with a persistent CDP port without running through Playwright received a token. In that capture, `navigator.webdriver` was `false`; the two POSTs had bodies of 3,874 and 88,076 bytes. The audit trace, HTML iframe, reference HAR and Performance trace are saved locally in `.build/flickr-audit/chrome152-success-frozen-20260926/` along with the SHA256 receipt. These private files may contain session IDs and should not end up in Git. There is no need to repeat a successful launch of Chrome to test each hypothesis.

The saved VM trace contains direct pixel readings. For the same drawing operations, Chrome and Mimic diverged as follows:

| Reading | Chrome 152 | Mimic before corrections | Mimic after corrections |
| --- | ---: | ---: | ---: |
| Canvas 48×48, nonzero bytes | 7,948 | 2,921 | 8,241 |
| Canvas 49×44, nonzero bytes | 6,900 | 0 before the full-circle arc fix | 6,904 |
| Downscale 80×24 to 1×1 after `difference` | `[0,0,0,255]` | `[255,255,255,255]` | `[0,0,0,255]` |

The 48×48 canvas has the same sequence of drawing operations, including shadows with a nonzero `shadowBlur`. Mimic previously stored shadow state without adding the shadow to the pixel buffer. The shared model now snapshots shadow state for each operation, blurs the source mask, and blends the shadow before the source for paths and text. The remaining difference of 293 nonzero bytes comes from the approximate rasterization model; its effect on the serialized VM field needs separate investigation.

A separate section of the VM draws 80x24, copies it with the `grayscale(1)` filter, then re-copies it with `globalCompositeOperation='difference'` and reads the smaller copy. Mimic has not previously executed `drawImage` for `difference` mode. Blending modes now go through the general pixel composer; `grayscale()` is applied to the copied pixels. Checking this area gave the same black pixel as the saved Chrome.

Smaller graphics differences remain: a 2×2 readback after a partial arc differs by 6–11 levels in individual channels, a 16×16 canvas by three nonzero bytes, and a 49×44 canvas by four nonzero bytes. Some text probes, including emoji, also differ in glyph coverage. Exact glyph and edge rasterization is outside the current model without a native graphics backend; substituting values for individual sites or characters is inappropriate. The P3 color probes in this trace matched. Causal attribution requires determining which remaining differences the VM puts into the second POST body.

After the fixes, one new run of Mimic sent a second POST with a body of 85,580 bytes, but received `600010` and an empty field. This is not equivalent to a successful Chrome server response. The saved HAR and VM trace is not a complete `offline_replay` bundle with the profile state and all network fixtures, so a strict replay receipt under the strict replay methodology is not yet available. A test run of Mimic without VM instrumentation also sent two POSTs, received `600010` and left the field blank.

## Exact substitution of Chrome results in VM

A diagnostic experiment compared complete readback arrays without changing production Mimic. The previously saved successful Chrome trace contained only the first 16 bytes of each `getImageData` result. One targeted capture of the complete arrays was therefore made in the already running headful Chrome 152; that run also obtained a token. All 16 result hashes matched those from the previous successful trace byte for byte. Complete arrays are stored only in the ignored `.build/flickr-audit/chrome152-success-frozen-20260926/chrome152-success-full-render.json`; its SHA256 was added to the local receipt.

Mimic's diagnostic `getImageData` first produced the normal result, then replaced its `data` with a Chrome array of the same type, canvas size and read size. All 16 calls coincided, there were no passes. Instrumentation of `ImageData.prototype.data` showed that before the second POST the VM actually reads these arrays. Additional counters recorded one `transferToImageBitmap`, after which the pixels are read again via `getImageData`; `WebGL.readPixels`, `toDataURL`, `toBlob` and `convertToBlob` were not called on the observed path.

With the exact Chrome arrays, Mimic sent two POSTs but received `600010` and left the field blank. The second response had `text/html` and 2444 bytes in one capture. The Mimic control with the same wrapper, but without data replacement, also sent two POSTs, received a short `text/html` (2,444–2,480 bytes) and `600010`. A successful HAR of regular Chrome contains a second `text/html` response of 4,208 bytes. MIME itself does not mean success: the response payload is encoded.

A subsequent direct comparison of VM objects **before** packing both POSTs in live Chrome and Mimic is described in the [prepack data report](flickr-turnstile-prepack-2026-09-26.md). It found a Trusted Types failure within one probe block, an entirely missing Mimic block, and other structural differences. The exact-pixel experiment above does not replace that comparison and did not yield a successful server response.

This rules out **exactness of these 16 pixel readbacks as a sufficient fix by itself** for the current failure. The VM continues through the second POST; `600010` appears after its response. The experiment does not prove that pixels are absent from the POST body or unused by the server alongside other fields. The contents and criteria of the server check remain unknown. Substituting both the iframe HTML and first `/fo` response from an older HAR into a new session did not reach the first request, so this partial replay cannot compare request bodies deterministically while changing one graphics value.
