# gov8 — Go bindings for V8

[![Go Reference](https://pkg.go.dev/badge/github.com/maclof/gov8.svg)](https://pkg.go.dev/github.com/maclof/gov8)

Embed Google's V8 JavaScript engine in Go with a typed API and a packaged
Windows/Linux runtime. This is Mimic’s local fork; see [build provenance](README.mimic.md).
Applications run without requiring
Rust, Visual Studio, a C/C++ compiler, or a separate V8 download.

## Highlights

- **Real V8** — execute JavaScript and WebAssembly using the engine from Chrome.
- **Go and JavaScript interop** — expose Go callbacks to JavaScript and call
  JavaScript functions from Go.
- **Zero-build application setup** — the verified Windows and Linux amd64 runtimes are
  included in the Go module and extracted automatically.
- **Audited behavior** — a pinned `rusty_v8` oracle runs matching fixtures and
  benchmarks against the Go implementation.
- **Broad safe API coverage** — isolates, contexts, handle scopes, scripts,
  callbacks, promises, modules, WebAssembly, snapshots, and Inspector.

The public API follows the safe executable surface of the pinned Rust
[`v8`](https://crates.io/crates/v8) crate while preserving V8's explicit
ownership, lifetime, and thread-affinity rules.

## Requirements

This fork supports **Windows amd64** and **Linux amd64 (glibc 2.39+)**.
Applications need Go 1.26.4+. ARM64, macOS and musl builds are not packaged.

The current engine is V8 `15.2.124.1-rusty`, from Rust `v8 = 152.2.0`.

## Install

Mimic's root `go.mod` selects this local fork. Keep it in the checkout;
`go get` of upstream gov8 does not install Mimic's Linux support or HTMLDDA fixes.

The appropriate gzip-compressed native shim is verified and extracted to the
user's content-addressed OS cache. The Windows and Linux assets are selected at
build time; a binary does not include both. The cache must allow executable
mappings. No native rebuild or separate engine download is needed.

`GOV8_SHIM_LIBRARY` selects a trusted developer library with the exact module
ABI. `GOV8_SHIM_DLL` remains a legacy alias. See [rebuild instructions](README.mimic.md)
for Linux and Windows; normal users need neither override.

Maintainers who need to rebuild the native shim or run the Rust oracle also
need Rust 1.98, Visual Studio with the MSVC C++ x64 build tools, and PowerShell:

```powershell
Set-Location third_party/gov8
# Build the patched V8 source first; see README.mimic.md.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\setup_windows.ps1 `
  -NativeSource C:\build\v8-152.2.0 `
  -NativeArchive C:\build\target\release\gn_out\obj\rusty_v8.lib
$env:GOV8_SHIM_DLL = (Resolve-Path build\shim\gov8_shim.dll)
go test ./...
```

The setup script downloads or reuses the pinned inputs, verifies their SHA-256
digests, and atomically writes `build\shim\gov8_shim.dll`.

When intentionally updating the packaged shim after a source change, rebuild
it and regenerate the deterministic gzip asset:

```powershell
# Build the patched V8 source first; see README.mimic.md.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\setup_windows.ps1 `
  -NativeSource C:\build\v8-152.2.0 `
  -NativeArchive C:\build\target\release\gn_out\obj\rusty_v8.lib
go run ./internal/cmd/package-shim
```

## Run JavaScript

An isolate is the closest V8 equivalent to a standalone VM. A context supplies
its global environment, and a scope owns temporary V8 values:

```go
package main

import (
	"fmt"
	"log"

	gov8 "github.com/maclof/gov8"
)

func run() error {
	if err := gov8.Initialize(); err != nil {
		return err
	}
	defer gov8.Shutdown()

	iso, err := gov8.NewIsolate()
	if err != nil {
		return err
	}
	defer iso.Close()
	defer gov8.ReleaseIsolateHostState(iso)

	ctx, err := iso.NewContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	scope, err := iso.NewScope()
	if err != nil {
		return err
	}
	defer scope.Close()

	script, err := ctx.Compile(scope, `21 * 2`, nil)
	if err != nil {
		return err
	}
	defer script.Close()

	result, err := script.Run(scope, nil)
	if err != nil {
		return err
	}
	n, ok, err := result.IntegerValue(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("JavaScript result is not an integer")
	}
	fmt.Println(n) // 42
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
```

The same program lives in [`examples/basic/main.go`](examples/basic/main.go).

## Call Go from JavaScript

Create a V8 function backed by a Go callback, then put it on the context's
global object. This snippet assumes the `iso`, `ctx`, and `scope` from the
previous example:

```go
add, err := iso.NewFunction(scope, ctx,
	func(cs *gov8.CallbackScope, args gov8.FunctionCallbackArguments, rv gov8.ReturnValue) {
		if args.Length() < 2 {
			return // an unset return slot becomes JavaScript undefined
		}
		a, err := args.Get(0)
		if err != nil {
			return
		}
		b, err := args.Get(1)
		if err != nil {
			return
		}
		av, aok, err := cs.IntegerValue(a)
		if err != nil || !aok {
			return
		}
		bv, bok, err := cs.IntegerValue(b)
		if err != nil || !bok {
			return
		}
		_ = rv.SetInt32(int32(av + bv))
	}, nil)
if err != nil {
	return err
}

global, err := ctx.GlobalObject(scope)
if err != nil {
	return err
}
set, err := global.SetByName(scope, ctx, "add", add.Value)
if err != nil {
	return err
}
if !set {
	return fmt.Errorf("could not define global add")
}

script, err := ctx.Compile(scope, `add(20, 22)`, nil)
if err != nil {
	return err
}
defer script.Close()
sum, err := script.Run(scope, nil)
if err != nil {
	return err
}
sumText, err := sum.ToString(ctx)
if err != nil {
	return err
}
fmt.Println(sumText) // 42
```

Callback arguments, return values, and values created through `CallbackScope`
are borrowed views valid only during that callback. Do not retain them. To turn
a callback failure into a JavaScript exception, create an error with
`cs.NewError` and pass it to `cs.ThrowException`.

## Call JavaScript from Go

Evaluate a function, check its type, and call it with a receiver and arguments:

```go
script, err := ctx.Compile(scope,
	`(function (name) { return "Hello, " + name + "!" })`, nil)
if err != nil {
	return err
}
defer script.Close()

value, err := script.Run(scope, nil)
if err != nil {
	return err
}
fn, ok, err := gov8.AsFunction(value, ctx)
if err != nil {
	return err
}
if !ok {
	return fmt.Errorf("script did not return a function")
}

receiver, err := scope.Undefined()
if err != nil {
	return err
}
name, err := scope.NewString("Go")
if err != nil {
	return err
}
greeting, called, err := fn.Call(scope, receiver, name)
if err != nil {
	return err
}
if !called {
	return fmt.Errorf("JavaScript function threw")
}
text, err := greeting.ToString(ctx) // "Hello, Go!"
if err != nil {
	return err
}
fmt.Println(text)
```

## Exceptions and errors

Ordinary misuse, lifecycle failures, and uncaught JavaScript exceptions are
returned as Go errors. APIs such as `Compile` and `Run` take an optional
`*gov8.TryCatch`: pass `nil` when the error is enough, or pass a catcher when
you need the JavaScript exception or message.

```go
tc, err := iso.NewTryCatch()
if err != nil {
	return err
}
defer tc.Close()

script, err := ctx.Compile(scope, `throw new Error("boom")`, tc)
if err != nil {
	return err
}
defer script.Close()

if _, err := script.Run(scope, tc); err != nil {
	caught, catchErr := tc.HasCaught()
	if catchErr != nil {
		return catchErr
	}
	if caught {
		message, catchErr := tc.ExceptionText(scope, ctx)
		if catchErr != nil {
			return catchErr
		}
		fmt.Println(message) // Error: boom
	}
}
```

## Lifetimes and threads

V8's ownership rules are visible in the API:

- Call `Initialize` once before creating isolates, then call `Shutdown` only
  after every isolate is closed.
- `NewIsolate` locks the creating goroutine to its OS thread. Use the isolate
  and close it from that same goroutine; a different isolate can run on its own
  goroutine.
- Close resources in dependency order: scripts and scopes, contexts,
  `ReleaseIsolateHostState`, isolate, then the global platform.
- Scopes must nest. Closing a scope invalidates every local `Value` created in
  it. Use persistent handles such as `Global` when a value must survive a scope.
- Never retain callback-borrowed arguments, return slots, or callback-scope
  values after the callback returns.
- Check every `error`, and check accompanying `ok` booleans where an operation
  can fail without a Go error or JavaScript can throw.

## Verify and benchmark

From the repository root:

```powershell
go test ./... -count=1
go test -race ./... -count=1
go vet ./...

# One quick Go benchmark smoke run.
go test . -run '^$' -bench '^BenchmarkScriptRunPrecompiledWorkload$' -benchtime=1x

# Rust oracle and benchmark smoke run.
Push-Location rust-oracle
cargo test --locked
cargo bench --locked --bench script -- --test
Pop-Location
```

Matched benchmark reports and their environment metadata are kept in
[`rust-oracle/bench-results`](rust-oracle/bench-results/).

## Project status

The current suite contains 526 normalized cross-language fixture checks: 525
are exact and 1 uses a documented safety normalization. The declaration ledger
records 1,698 direct matches, 10 intentional Go-shape differences, no safe
executable gaps, and 149 intentionally unexposed
raw/borrowed/generic Rust shapes.

This does not mean every Rust ownership or generic type has a literal Go
spelling, and it is not a performance-parity claim. The audited safe executable
surface has feature and behavioral parity; intentional Go safety shapes and
measured performance gaps are tracked openly. The raw CreateParams stack-limit
pointer is omitted, and the oracle confirms pinned V8 overwrites it before
JavaScript execution.

- [`PARITY.md`](PARITY.md) - feature coverage, behavior notes, and performance
  evidence
- [`API_AUDIT.md`](API_AUDIT.md) - declaration-level API audit
- [`rust-oracle/README.md`](rust-oracle/README.md) - pinned versions, oracle
  workflow, and reproducibility details

Contributions should include tests for success, failure, exception, lifetime,
and concurrency behavior where applicable, plus matched benchmarks for hot
paths.

## License

gov8 is available under the [MIT License](LICENSE). The packaged native shim
also contains third-party software covered by the notices in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
