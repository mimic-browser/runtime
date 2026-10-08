# Graphics observation checkpoint

The runtime uses per-context JavaScript state, immutable operation snapshots and
query-time evaluation. It does not load a GPU, Chromium, ANGLE, a native renderer
or a font rasterizer. No website-specific branches or source-string fingerprints
select results. Repeating inputs reproduces the same observations; changing a
local primitive preserves unaffected samples.

## Canvas

Paths retain transformed coordinates independently of save/restore state. Clips,
paint stops, paint transforms, compositing and alpha are captured at the drawing
operation, so later mutations cannot change previous observations. Text and path
commands share one ordered queue with clear, pixel writes and bitmap copies.
Linear/radial gradients and Porter-Duff modes plus multiply/screen affect reads.
`HTMLCanvasElement.toDataURL()` and `toBlob()` serialize that same canonical
readback as a deterministic PNG; repeated calls and separate Pages using the
same profile therefore receive identical bytes for identical state. JPEG and
WebP encoding remain explicit unsupported boundaries rather than independent
approximate image models.

Paths approximate curves with bounded line segments and sample pixel centers.
There is no edge antialiasing. Stroke cap/join/dash fidelity, exact radial-gradient
edge cases, Path2D, arcTo, roundRect, shadows, filters, exact text metrics/shaping,
image decoding and general image resampling remain incomplete. Text retains its
coarse local codepoint coverage model. Diagnostics explicitly identify approximate
text/path observations; these results are not claimed to equal Chrome images.

## WebGL

A bounded arithmetic GLSL parser/evaluator supports scalar/vector expressions,
swizzles, constructors, common arithmetic builtins, local scopes, finite loops,
conditionals and fragment discard. Program ownership, deferred deletion, linking,
active inputs, uniform types/locations and copied vertex inputs share context
state. Invalid cases covered by the oracle fail compilation or set GL errors;
unsupported language is an explicit NotSupportedError, not a successful placeholder.
Static checking is deliberately incomplete: this is not a full GLSL validator or
optimizer. Active inputs are syntactically referenced globals, not whole-program
dead-code elimination.

Triangle, strip and fan observations use captured positions, uniforms, viewport,
scissor and color mask. Sampling occurs only when the stored result is observed
or an ordered clear needs it. Each draw retains its inputs across later buffer,
uniform and program changes. Arithmetic uses float32 rounding; UNORM conversion
matches measured Windows Chrome midpoint behavior in the retained probes. This
is not a promise of GPU transcendental or compiler-optimization equivalence.

Varying interpolation, textures, matrices, user functions, arbitrary clip-space
geometry, depth/stencil/blend/cull execution and multisampling remain explicit
boundaries. Sample evaluation is bounded to 65,536 drawing-buffer pixels, 65,536
vertices per draw, 4,096 evaluator steps per invocation and 1,024 loop iterations.
Triangle edge ownership remains approximate; front-facing and fragment depth/W
are derived from the captured geometry. Approximate shader observations are diagnosed. Default context antialiasing remains disabled.

Framebuffer/renderbuffer objects retain binding, ownership, deletion, attachment,
format, dimensions and completeness state. Float formats require the enabled
extension. This supports capability/resource queries, not attachment pixel
execution: attachment clear/read/draw, textures and multisample allocation stay
explicitly unsupported. No duplicate attachment pixel model is introduced.

## Evidence

Frozen Chrome 152.0.7977.82 was queried in disposable browser contexts using
`tools/compatibility/capture_query_oracle.py`. Existing browser tabs were untouched.
The source SHA-256 and reference results are retained under
`compatibility/captures/semantic-checkpoints`:

- `canvas-paths-chrome152.json`: clipping, winding, transforms, copy and reset.
- `canvas-relations-chrome152.json`: repeated/local curve changes, gradient
  mutation/snapshots, text clipping and copy outside the glyph area.
- `webgl-programs-chrome152.json`: linking, uniforms, arithmetic loops, readbacks,
  local scissor changes and deferred resource deletion.
- `webgl-framebuffers-chrome152.json`: attachment lifecycle and integer/float
  format queries for WebGL 1/2.
- `webgl-geometry-chrome152.json`: triangle-strip facing and fragment depth/W.
- `webgl-framebuffer-lifecycle-chrome152.json`: deletion while an attachment
  remains held by an unbound framebuffer.
- `webgl-validation-chrome152.json`: invalid shader cases, inactive inputs,
  uniform type errors and stale locations after relinking.

`TestGraphicsObservationOracles` runs these exact sources in Window and Worker
on both V8 and Goja. Existing storage, reflection and capability tests are retained.
The old blanket compile-unsupported assertion now checks a texture shader outside
the implemented subset and the GL error for drawing without a linked program.

No new live capture ran for this validation scope, so it establishes no
Cloudflare success or causal improvement. WebGPU resource/command execution,
fonts/offline audio, remaining network observations and other capability APIs
were outside this checkpoint's qualification scope.
