# Retained release license texts

`native-notices.txt` supplements gov8's V8/rusty_v8 and Temporal notices with
V8 15.2.124.1 root/third-party notices and the pinned native dependency licenses
from its DEPS revisions. `sources.json` records the upstream URLs and SHA-256 of
each original text. These are license notices, not runtime source exports.
Update this inventory when changing the packaged V8 dependency closure.

`gcc-runtime.txt` contains the installed MinGW-w64 GCC runtime's COPYING3,
COPYING.RUNTIME (including the GCC Runtime Library Exception), COPYING.LIB, and
README, from the Windows build environment's gcc-libs license directory.
The binaries do not ship a separate GCC runtime DLL.

The packager additionally collects licenses for the actual Go dependency graph,
Go's standard library, embedded QuickJS, Temporal, and vendored script resources.
`canvas-raster-notices.txt` records the pinned Cargo dependency inventory and
deduplicated license texts for the embedded CPU WebAssembly raster module.
fhttp v0.6.9 omits the LICENSE file referenced by its retained Go Authors BSD
headers; the packager includes the Go BSD text with explicit attribution and
checks that the identifying header still exists rather than silently omitting it.
