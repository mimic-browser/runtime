# Canvas color glyph raster module

The bundled `raster.wasm` renders selected COLRv1 font glyphs with Vello's CPU
renderer. It has no imports, GPU dependency, or native graphics backend. Mimic
instantiates it lazily in the owning Page realm, retains its font bytes for that
Page, and composites a rendered tile into the canonical Canvas surface.

Rebuild the checked-in artifact from this directory with a Rust toolchain that
supports edition 2024 and the `wasm32-unknown-unknown` target:

```sh
cargo build --release --locked --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/mimic_canvas_raster.wasm raster.wasm
```

`Cargo.lock` pins the dependency graph. Release terms for its dependencies are
retained in `tools/release/licenses/canvas-raster-notices.txt`. The module's
output is a CPU approximation of Chrome 152 color glyph rasterization; the
retained 20×20 emoji control differs in 172 pixels. Other Canvas text modes
remain diagnosed approximations.
