# Canvas glyph raster module

The bundled `raster.wasm` renders selected COLRv1 glyphs and monochrome glyph
fills/strokes with Vello's CPU renderer. It has no imports, GPU dependency, or
native graphics backend. Mimic instantiates it lazily in the owning Page realm,
retains selected font bytes for that Page, and composites rendered tiles into
the canonical Canvas surface. Color glyphs keep their intrinsic paints during
`strokeText`; monochrome glyphs use the Canvas stroke width.

Rebuild the checked-in artifact from this directory with a Rust toolchain that
supports edition 2024 and the `wasm32-unknown-unknown` target:

```sh
cargo build --release --locked --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/mimic_canvas_raster.wasm raster.wasm
```

`Cargo.lock` pins the dependency graph. Release terms for its dependencies are
retained in `tools/release/licenses/canvas-raster-notices.txt`. The module's
output is a CPU approximation of Chrome 152 text rasterization. The retained
20×20 emoji control differs in 172 pixels. Glyph outlines, hinting, and shadows
can still differ; unsupported transforms and compositing modes retain the
diagnosed fallback model.
