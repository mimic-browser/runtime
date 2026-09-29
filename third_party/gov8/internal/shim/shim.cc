// shim.cc вЂ” gov8 C ABI shim over the pinned prebuilt V8 static library.
//
// Build: scripts/setup_windows.ps1 (Windows/MSVC) or scripts/setup_linux.py
// (Linux/Clang), both linking the pinned rusty_v8 152.2.0 release archive.
//
// ABI rules (contract with the Go side, see ffi.go in the Go module root):
//   - Exports use extern "C" with the host ABI. Go calls pass pointer-sized
//     words. Linux wrappers normalize the five legacy floating-point exports;
//     wide pointer-word calls use gov8_call_words. Windows retains its existing
//     positional integer/XMM dispatch and packaged ABI.
//   - No C++ exception may cross the boundary: every function body is wrapped
//     in try/catch(...) and converts failures to a negative status code plus a
//     thread-local error string readable via gov8_last_error.
//   - v8::Local<T> handles are trivially-copyable one-word slot handles; the
//     wire format is that one word (the handle slot address) copied verbatim.
//     Slots live in a Go-owned v8::HandleScope (gov8_scope_enter/gov8_scope_exit),
//     so a wire value is valid exactly while that scope is open and on the
//     thread that owns it.
//   - Persistent objects (contexts, scripts, microtask queues, try-catches)
//     are wrapped in small heap structs tagged with a magic word so stale or
//     cross-thread misuse is detected where practical.
//   - Entered-owned-isolate model (matches the pinned Rust crate, whose
//     OwnedIsolate is "entered upon creation and exited upon being dropped"):
//     gov8_isolate_new / gov8_isolate_new_snapshot leave the engine isolate
//     ENTERED on the creating thread, and gov8_isolate_dispose EXITS it
//     before Isolate::Dispose (Dispose CHECK-fails on an entered isolate).
//     Exports therefore use Gov8IsolateScope below instead of an
//     unconditional v8::Isolate::Scope: the wrapper enters only when the
//     isolate is not already the thread's current one, so every call from
//     the owning thread (the only legal caller per the Go affinity checks)
//     skips both the Enter and the Exit, and the engine's per-isolate entry
//     stack stays at its single creation level. Two isolates created
//     sequentially on one thread (Go allows this; LockOSThread nests) keep
//     working: the conditional scope performs the same Enter/Exit switch the
//     old unconditional scopes did, and an out-of-order Close exits the
//     disposed isolate's own creation level (per-isolate entry stack),
//     after which conditional scopes transparently re-enter the survivor.

#ifndef _WIN32
#define __declspec(x) __attribute__((visibility("default")))
#endif

#include <cstdlib>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <memory>
#include <new>
#include <optional>
#include <string>
#include <utility>

#include "v8.h"
#include "v8-version-string.h"

// Keep every execution isolate in a separate compression cage. Sharing a cage
// would impose one aggregate 4 GiB heap limit on otherwise independent Pages.
// The isolate retains the group; its final disposal also releases the cage.
static v8::Isolate* Gov8NewIndependentIsolate(
    const v8::Isolate::CreateParams& params) {
  return v8::Isolate::New(v8::IsolateGroup::Create(), params);
}


// Linux's pointer-word trampoline covers exports wider than purego's 15 words.
// All shim arguments and results use this integer/pointer ABI, never FP registers.
#ifndef _WIN32
template <size_t... I>
uintptr_t Gov8CallWords(uintptr_t address, const uintptr_t* words, std::index_sequence<I...>) {
  using Fn = uintptr_t (*)(decltype((void)I, uintptr_t{})...);
  return reinterpret_cast<Fn>(address)(words[I]...);
}
template <size_t N>
uintptr_t Gov8CallWide(uintptr_t address, size_t count, const uintptr_t* words) {
  if (count == N) return Gov8CallWords(address, words, std::make_index_sequence<N>{});
  if constexpr (N > 16) return Gov8CallWide<N-1>(address, count, words);
  std::abort();
}
extern "C" __attribute__((visibility("default")))
uintptr_t gov8_call_words(uintptr_t address, size_t count, const uintptr_t* words) {
  return Gov8CallWide<42>(address, count, words);
}
#endif

// --- C bindings exported by the pinned prebuilt artifact --------------------
//
// Two APIs differ between the vendored V8 headers and the artifact binary, so
// the shim binds to the artifact's own binding layer (src/binding.cc of the
// pinned crate), which is part of the released static library and therefore
// matches its ABI exactly:
//
//  - v8::platform::NewDefaultPlatform is not exported as C++ symbol; the
//    artifact exports v8__Platform__NewDefaultPlatform instead (used with
//    (0, false) to mirror the oracle's new_default_platform(0, false)).
//  - v8::MicrotaskQueue::New in the artifact returns a raw pointer; the
//    artifact exports the owning RustyMicrotaskQueueHandle wrapper.
extern "C" {
v8::Platform* v8__Platform__NewDefaultPlatform(int thread_pool_size,
                                               bool idle_task_support);
void v8__Platform__DELETE(v8::Platform* self);
void* v8__MicrotaskQueueHandle__New(v8::Isolate* isolate,
                                    v8::MicrotasksPolicy policy);
void v8__MicrotaskQueueHandle__DELETE(void* self);
v8::MicrotaskQueue* v8__MicrotaskQueueHandle__Get(const void* self);
// v8::MicrotaskQueue's class layout in the artifact differs from the
// vendored headers (its New returns a raw pointer, and the set of virtual
// EnqueueMicrotask overloads differs), so ALL dispatch on queue objects goes
// through these flat bindings, never through virtual calls compiled from the
// vendored header.
void v8__MicrotaskQueue__PerformCheckpoint(v8::Isolate* isolate,
                                           v8::MicrotaskQueue* self);
void v8__MicrotaskQueue__EnqueueMicrotask(v8::Isolate* isolate,
                                          v8::MicrotaskQueue* self,
                                          v8::Function* callback);
void cppgc__shutdown_process();
v8::Platform* gov8_cppgc_take_detached_platform();
}

// --- Fail-loud stubs for artifact symbols implemented on the Rust side ------
//
// The pinned artifact's binding.obj references callback implementations that
// the pinned Rust crate normally supplies (serializer/deserializer delegates,
// inspector clients and custom platform hooks). None of them
// are reachable through this shim's API surface: the shim never creates a
// ValueSerializer/ValueDeserializer delegate, never instantiates an inspector,
// and never registers a custom platform. They exist purely to close the
// static link; if one is ever reached it is a hard programming error and the
// process aborts loudly instead of misbehaving.
namespace gov8_stub {
[[noreturn]] void Unreachable(const char* name) {
  std::abort();
}
}  // namespace gov8_stub

#define GOV8_STUB(name)                         \
  extern "C" void name() {                      \
    gov8_stub::Unreachable(#name);              \
  }

GOV8_STUB(v8__ValueDeserializer__Delegate__GetSharedArrayBufferFromId)
GOV8_STUB(v8__ValueDeserializer__Delegate__GetWasmModuleFromId)
GOV8_STUB(v8__ValueDeserializer__Delegate__ReadHostObject)
GOV8_STUB(v8__ValueSerializer__Delegate__FreeBufferMemory)
GOV8_STUB(v8__ValueSerializer__Delegate__GetSharedArrayBufferId)
GOV8_STUB(v8__ValueSerializer__Delegate__GetWasmModuleTransferId)
GOV8_STUB(v8__ValueSerializer__Delegate__HasCustomHostObject)
GOV8_STUB(v8__ValueSerializer__Delegate__IsHostObject)
GOV8_STUB(v8__ValueSerializer__Delegate__ReallocateBufferMemory)
GOV8_STUB(v8__ValueSerializer__Delegate__ThrowDataCloneError)
GOV8_STUB(v8__ValueSerializer__Delegate__WriteHostObject)
GOV8_STUB(v8_inspector__V8InspectorSession__Inspectable__BASE__DROP)
GOV8_STUB(v8_inspector__V8InspectorSession__Inspectable__BASE__get)

#undef GOV8_STUB

namespace {

constexpr int64_t kOk = 0;
constexpr int64_t kErrGeneric = -1;
constexpr int64_t kErrBadArg = -2;
constexpr int64_t kErrState = -3;       // wrong lifecycle state
constexpr int64_t kErrNoMemory = -4;    // buffer too small (version strings)
constexpr int64_t kErrException = -5;   // JS exception observed
constexpr int64_t kErrCpp = -6;         // C++ exception caught at the boundary
constexpr int64_t kErrMagic = -7;       // stale/invalid wrapper handle

constexpr uint64_t kScopeMagic = 0x674f563853435050ULL;   // "gOV8SCPP"
constexpr uint64_t kTcMagic = 0x674f563854434154ULL;      // "gOV8TCAT"
constexpr uint64_t kCtxMagic = 0x674f563843545854ULL;     // "gOV8CTXT"
constexpr uint64_t kScriptMagic = 0x674f563853435254ULL;  // "gOV8SCRT"
constexpr uint64_t kMqMagic = 0x674f56384d515551ULL;      // "gOV8MQUQ"

thread_local std::string tls_error;

void SetErr(const char* msg) { tls_error = msg; }
void ClearErr() { tls_error.clear(); }

// ---------------------------------------------------------------------------
// Wire-format helpers. v8::Local<T> is trivially copyable and exactly one
// pointer wide (V8_TRIVIAL_ABI); its single word is the handle slot. Copying
// the word preserves the handle; copying the slot through Go is equally safe
// because the slot itself is stable (the GC updates slot contents, not slot
// addresses) until the owning HandleScope closes.

template <class T>
void* ToWire(const v8::Local<T>& local) {
  void* wire = nullptr;
  if (!local.IsEmpty()) {
    std::memcpy(&wire, &local, sizeof(void*));
  }
  return wire;
}

template <class T>
v8::Local<T> FromWire(void* wire) {
  v8::Local<T> local;
  if (wire != nullptr) {
    std::memcpy(&local, &wire, sizeof(void*));
  }
  return local;
}

// ---------------------------------------------------------------------------
// Wrapper structs handed to Go. Every one carries a magic word so that a
// stale wrapper (used after Close) is reliably detected at the shim layer.

struct GoScope {
  uint64_t magic;
  v8::Isolate* iso;
  // v8::HandleScope forbids heap operator new/delete; it is placement-new'd
  // into this storage so the wrapper pointer handed to Go stays stable.
  alignas(v8::HandleScope) unsigned char hs_storage[sizeof(v8::HandleScope)];

  v8::HandleScope* hs() { return reinterpret_cast<v8::HandleScope*>(hs_storage); }
};

struct TcWrap {
  uint64_t magic;
  v8::Isolate* iso;
  // v8::TryCatch likewise forbids heap operator new/delete; placement-new'd.
  alignas(v8::TryCatch) unsigned char tc_storage[sizeof(v8::TryCatch)];

  v8::TryCatch* tc() { return reinterpret_cast<v8::TryCatch*>(tc_storage); }
};

struct CtxWrap {
  uint64_t magic;
  v8::Isolate* iso;
  v8::Global<v8::Context>* ctx;
};

struct ScriptWrap {
  uint64_t magic;
  v8::Isolate* iso;
  v8::Global<v8::Script>* script;
};

struct MqWrap {
  uint64_t magic;
  v8::Isolate* iso;
  // Owning handle created by the artifact's v8__MicrotaskQueueHandle__New
  // binding (the artifact's v8::MicrotaskQueue::New returns a raw pointer, so
  // the ownership wrapper from the pinned binding layer is reused verbatim).
  void* handle;
  v8::MicrotaskQueue* queue;
};

GoScope* AsScope(void* p) {
  if (p == nullptr) return nullptr;
  GoScope* s = static_cast<GoScope*>(p);
  return s->magic == kScopeMagic ? s : nullptr;
}

TcWrap* AsTc(void* p) {
  if (p == nullptr) return nullptr;
  TcWrap* w = static_cast<TcWrap*>(p);
  return w->magic == kTcMagic ? w : nullptr;
}

CtxWrap* AsCtx(void* p) {
  if (p == nullptr) return nullptr;
  CtxWrap* w = static_cast<CtxWrap*>(p);
  return w->magic == kCtxMagic ? w : nullptr;
}

ScriptWrap* AsScript(void* p) {
  if (p == nullptr) return nullptr;
  ScriptWrap* w = static_cast<ScriptWrap*>(p);
  return w->magic == kScriptMagic ? w : nullptr;
}

MqWrap* AsMq(void* p) {
  if (p == nullptr) return nullptr;
  MqWrap* w = static_cast<MqWrap*>(p);
  return w->magic == kMqMagic ? w : nullptr;
}

// Validates that a wrapper handed in by Go belongs to the isolate the call
// targets. Every persistent wrapper records the isolate it was created on,
// so this is a cheap pointer comparison before any engine access; V8
// requires Global handles and scope objects to be used on their owning
// isolate. The Go wrapper performs the same check first вЂ” this is the
// second line of defense at the ABI boundary.
bool OwnedBy(v8::Isolate* iso, v8::Isolate* wrapper_iso) {
  if (wrapper_iso != iso) {
    SetErr("wrapper belongs to a different isolate");
    return false;
  }
  return true;
}

// v8::HandleScope and v8::TryCatch declare a private class-level operator
// new, which also hides the implicit placement form inside those classes.
// Routing construction through the global scope (::new) is the standard
// workaround and is well-formed: their storage never escapes this shim's
// wrapper allocations.
template <class T, class... Args>
T* GlobalPlacementNew(void* storage, Args&&... args) {
  return ::new (storage) T(std::forward<Args>(args)...);
}

// Conditional isolate scope for the entered-owned-isolate model.
//
// Why not an unconditional v8::Isolate::Scope: with the isolate entered once
// at creation (gov8_isolate_new / gov8_isolate_new_snapshot) and exited once
// before Dispose (gov8_isolate_dispose), a per-call Enter/Exit pair is pure
// overhead on the hot path вЂ” the already-entered Enter still runs
// Heap::SetStackStart (a VirtualQuery on Windows) and bumps the entry stack,
// and the non-entered path additionally allocates an EntryStackItem and
// looks up per-isolate thread data. Every export here is called from the
// isolate's owning thread (the Go wrapper checks thread affinity first), so
// a single TLS read via Isolate::TryGetCurrent proves the isolate is already
// current and the whole scope becomes a compare.
//
// Why it is still correct when it does fire: Go permits two isolates to live
// sequentially on one OS thread (LockOSThread nests). The second isolate's
// creation Enter pushed a new level, so a call targeting the first isolate
// observes TryGetCurrent() != iso and performs exactly the Enter/Exit switch
// the old unconditional scopes performed вЂ” V8 restores the previous isolate
// from the per-isolate entry stack on Exit, so interleaved use and
// out-of-order Close keep the same observable behavior as before. During
// engine callbacks the isolate is current by construction, so re-entrant
// calls from Go callbacks skip the scope too.
class Gov8IsolateScope {
 public:
  explicit Gov8IsolateScope(v8::Isolate* iso) : iso_(nullptr) {
    if (v8::Isolate::TryGetCurrent() != iso) {
      iso_ = iso;
      iso_->Enter();
    }
  }
  ~Gov8IsolateScope() {
    if (iso_ != nullptr) {
      iso_->Exit();
    }
  }
  Gov8IsolateScope(const Gov8IsolateScope&) = delete;
  Gov8IsolateScope& operator=(const Gov8IsolateScope&) = delete;

 private:
  v8::Isolate* iso_;  // non-null only while entered
};

// ---------------------------------------------------------------------------
// Process state. The strict state machine itself lives in Go (it mirrors the
// pinned crate's behavior: double initialize is an error, dispose ordering is
// enforced); the shim only tracks what the engine needs.

v8::Platform* g_platform = nullptr;
std::unique_ptr<v8::ArrayBuffer::Allocator> g_allocator;

v8::TryCatch* TcOf(TcWrap* w) { return w == nullptr ? nullptr : w->tc(); }

// Writes `text` into the caller's fixed buffer. Returns byte length or
// kErrNoMemory when the buffer is too small (the needed size is not reported;
// callers use a generous buffer).
int64_t CopyOut(const char* text, size_t len, char* buf, int64_t cap) {
  if (buf == nullptr || cap < 0) {
    SetErr("null output buffer");
    return kErrBadArg;
  }
  if (static_cast<int64_t>(len) > cap) {
    SetErr("output buffer too small");
    return kErrNoMemory;
  }
  std::memcpy(buf, text, len);
  return static_cast<int64_t>(len);
}

// Writes `str` into the caller's buffer. Returns kOk with *out_len set, or
// kErrNoMemory with *out_len set to the required size. Text is always copied
// during the shim call; no pointer into engine or shim memory escapes.
int64_t BufferUtf8(const std::string& str, char* buf, int64_t cap,
                   int64_t* out_len) {
  if (out_len == nullptr) {
    SetErr("null out params");
    return kErrBadArg;
  }
  *out_len = static_cast<int64_t>(str.size());
  if (buf == nullptr || static_cast<int64_t>(str.size()) > cap) {
    SetErr("output buffer too small");
    return kErrNoMemory;
  }
  std::memcpy(buf, str.data(), str.size());
  return kOk;
}

// A TryCatch created only when the caller did not supply one. V8 routes
// exceptions to the INNERMOST active TryCatch, so a fallback must not exist
// while a caller's TryCatch is active.
struct FallbackTryCatch {
  alignas(v8::TryCatch) unsigned char storage[sizeof(v8::TryCatch)];
  bool used = false;

  v8::TryCatch* Init(v8::Isolate* iso) {
    used = true;
    return GlobalPlacementNew<v8::TryCatch>(storage, iso);
  }
  ~FallbackTryCatch() {
    if (used) {
      reinterpret_cast<v8::TryCatch*>(storage)->~TryCatch();
    }
  }
};

// ECMAScript ToString of a value, encoded as UTF-8 (lossy, matching the
// oracle's to_rust_string_lossy: unpaired surrogates become U+FFFD).
bool ValueText(v8::Isolate* iso, v8::Local<v8::Context> ctx,
               v8::Local<v8::Value> v, std::string* out) {
  v8::TryCatch tc(iso);
  v8::Local<v8::String> s;
  if (!v->ToString(ctx).ToLocal(&s)) {
    SetErr("ToString failed");
    return false;
  }
  const size_t n = s->Utf8Length(iso);
  out->resize(n);
  if (n > 0) {
    const size_t written =
        s->WriteUtf8(iso, out->data(), n, v8::String::WriteFlags::kReplaceInvalidUtf8);
    out->resize(written);
  }
  return true;
}

}  // namespace

// ===========================================================================
// Exported C ABI.
// ===========================================================================

extern "C" {

// --- ABI -------------------------------------------------------------------

__declspec(dllexport) int64_t gov8_abi_version(void) {
  ClearErr();
  return 44;
}

// --- platform / process lifecycle -------------------------------------------

__declspec(dllexport) int64_t gov8_initialize_platform(void) {
  ClearErr();
  try {
    if (g_platform != nullptr) {
      SetErr("platform already created");
      return kErrState;
    }
    // Pinned oracle configuration: new_default_platform(0, false) вЂ” default
    // worker count, no idle-task support.
    v8::Platform* platform = gov8_cppgc_take_detached_platform();
    if (platform == nullptr) {
      platform = v8__Platform__NewDefaultPlatform(0, 0);
    }
    if (platform == nullptr) {
      SetErr("NewDefaultPlatform failed");
      return kErrState;
    }
    v8::V8::InitializePlatform(platform);
    g_platform = platform;
    if (!v8::V8::Initialize()) {
      SetErr("V8::Initialize failed");
      return kErrState;
    }
    g_allocator.reset(v8::ArrayBuffer::Allocator::NewDefaultAllocator());
    return kOk;
  } catch (...) {
    SetErr("C++ exception in initialize_platform");
    return kErrCpp;
  }
}

// The pinned crate's get_current_platform() panics when no platform was ever
// installed; the Go layer's lifecycle state machine provides the same
// observable behavior, backed by this recorded flag.
__declspec(dllexport) int64_t gov8_platform_present(void) {
  ClearErr();
  return g_platform != nullptr ? 1 : 0;
}

// Returns 1 when V8::Dispose() returned true, 0 when it returned false,
// negative on wrapper errors. C++ V8::Dispose is idempotent-ish; the strict
// one-shot behavior the oracle characterizes is enforced in Go.
__declspec(dllexport) int64_t gov8_v8_dispose(void) {
  ClearErr();
  try {
    if (g_platform == nullptr) {
      SetErr("v8 not initialized");
      return kErrState;
    }
    const bool disposed = v8::V8::Dispose();
    cppgc__shutdown_process();
    g_allocator.reset();
    return disposed ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception in v8_dispose");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_v8_dispose_platform(void) {
  ClearErr();
  try {
    if (g_platform == nullptr) {
      SetErr("platform not present");
      return kErrState;
    }
    v8::V8::DisposePlatform();
    v8__Platform__DELETE(g_platform);
    g_platform = nullptr;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in v8_dispose_platform");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_version(int32_t* out) {
  ClearErr();
  if (out == nullptr) {
    SetErr("null out params");
    return kErrBadArg;
  }
  out[0] = V8_MAJOR_VERSION;
  out[1] = V8_MINOR_VERSION;
  out[2] = V8_BUILD_NUMBER;
  out[3] = V8_PATCH_LEVEL;
  return kOk;
}

__declspec(dllexport) int64_t gov8_version_string(char* buf, int64_t cap) {
  ClearErr();
  return CopyOut(V8_VERSION_STRING, sizeof(V8_VERSION_STRING) - 1, buf, cap);
}

__declspec(dllexport) int64_t gov8_runtime_version(char* buf, int64_t cap) {
  ClearErr();
  try {
    return CopyOut(v8::V8::GetVersion(), std::strlen(v8::V8::GetVersion()), buf, cap);
  } catch (...) {
    SetErr("C++ exception in runtime_version");
    return kErrCpp;
  }
}

// --- isolate -----------------------------------------------------------------

__declspec(dllexport) void* gov8_isolate_new(void) {
  ClearErr();
  try {
    if (g_platform == nullptr) {
      SetErr("v8 not initialized");
      return nullptr;
    }
    v8::Isolate::CreateParams params;
    params.array_buffer_allocator = g_allocator.get();
    v8::Isolate* iso = Gov8NewIndependentIsolate(params);
    if (iso == nullptr) {
      SetErr("Isolate::New failed");
      return nullptr;
    }
    // Entered-owned-isolate model (the pinned crate: "entered upon creation
    // and exited upon being dropped"): the creating thread is the owner for
    // the isolate's whole life (the Go wrapper locks the OS thread first),
    // so keep the isolate entered from here until gov8_isolate_dispose.
    iso->Enter();
    return iso;
  } catch (...) {
    SetErr("C++ exception in isolate_new");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_isolate_dispose(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    // Exit the creation Enter before Dispose: Isolate::Dispose CHECK-fails
    // ("Disposing the isolate that is entered by a thread") unless the
    // isolate's own entry stack is empty. Exit pops this isolate's creation
    // level and restores the previously entered isolate from it, which is
    // exactly the LIFO drop the pinned crate performs on OwnedIsolate; for
    // an out-of-order Close (a newer isolate still entered on this thread)
    // the restored isolate is the one current at creation time and the
    // survivor's next call re-enters it through Gov8IsolateScope.
    iso->Exit();
    iso->Dispose();
    return kOk;
  } catch (...) {
    SetErr("C++ exception in isolate_dispose");
    return kErrCpp;
  }
}

// --- handle scopes ------------------------------------------------------------

__declspec(dllexport) void* gov8_scope_enter(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return nullptr;
  }
  try {
    GoScope* s = new GoScope{};
    s->magic = kScopeMagic;
    s->iso = iso;
    GlobalPlacementNew<v8::HandleScope>(s->hs_storage, iso);
    return s;
  } catch (...) {
    SetErr("C++ exception in scope_enter");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_scope_exit(void* scope) {
  ClearErr();
  GoScope* s = AsScope(scope);
  if (s == nullptr) {
    SetErr("invalid scope handle");
    return kErrMagic;
  }
  try {
    s->hs()->~HandleScope();
    delete s;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in scope_exit");
    s->hs()->~HandleScope();
    delete s;
    return kErrCpp;
  }
}

// --- context -------------------------------------------------------------------

__declspec(dllexport) void* gov8_context_new(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Context> ctx = v8::Context::New(iso);
    if (ctx.IsEmpty()) {
      SetErr("Context::New failed");
      return nullptr;
    }
    CtxWrap* w = new CtxWrap{};
    w->magic = kCtxMagic;
    w->iso = iso;
    w->ctx = new v8::Global<v8::Context>(iso, ctx);
    return w;
  } catch (...) {
    SetErr("C++ exception in context_new");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_context_dispose(void* ctxw) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  if (w == nullptr) {
    SetErr("invalid context handle");
    return kErrMagic;
  }
  try {
    Gov8IsolateScope iso_scope(w->iso);
    w->ctx->Reset();
    delete w->ctx;
    delete w;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in context_dispose");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_context_global_object(v8::Isolate* iso,
                                                         void* ctxw, void* scope,
                                                         void** out) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (w == nullptr || s == nullptr || out == nullptr ||
      !OwnedBy(iso, w->iso) || !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    *out = ToWire(ctx->Global());
    return kOk;
  } catch (...) {
    SetErr("C++ exception in context_global_object");
    return kErrCpp;
  }
}

// --- primitive constructors -----------------------------------------------------
//
// All constructors create their handle slots in the CALLER's open HandleScope
// (the Go-owned scope, validated via the wrapper). They must not open their
// own HandleScope: a slot created in a shim-local scope would dangle the
// moment the shim returns.

bool ScopeIs(v8::Isolate* iso, void* scope);

#define GOV8_PRIM(name, iso_arg, body)                                       \
  __declspec(dllexport) void* name iso_arg {                                 \
    ClearErr();                                                              \
    try {                                                                    \
      Gov8IsolateScope iso_scope(iso);                                     \
      body;                                                                  \
    } catch (...) {                                                          \
      SetErr("C++ exception");                                               \
      return nullptr;                                                        \
    }                                                                        \
  }

GOV8_PRIM(gov8_undefined, (v8::Isolate * iso, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Undefined(iso));)
GOV8_PRIM(gov8_null, (v8::Isolate * iso, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Null(iso));)
GOV8_PRIM(gov8_boolean, (v8::Isolate * iso, int64_t value, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Boolean::New(iso, value != 0));)
GOV8_PRIM(gov8_integer_new, (v8::Isolate * iso, int32_t value, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Integer::New(iso, value));)
GOV8_PRIM(gov8_integer_new_unsigned, (v8::Isolate * iso, uint32_t value, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Integer::NewFromUnsigned(iso, value));)
GOV8_PRIM(gov8_number_new, (v8::Isolate * iso, double value, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::Number::New(iso, value));)
GOV8_PRIM(gov8_bigint_new_i64, (v8::Isolate * iso, int64_t v, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::BigInt::New(iso, v));)
GOV8_PRIM(gov8_bigint_new_u64, (v8::Isolate * iso, uint64_t v, void* scope),
          if (!ScopeIs(iso, scope)) return nullptr;
          return ToWire(v8::BigInt::NewFromUnsigned(iso, v));)

#undef GOV8_PRIM

bool ScopeIs(v8::Isolate* iso, void* scope) {
  GoScope* s = AsScope(scope);
  if (s == nullptr || s->iso != iso) {
    SetErr("invalid scope handle for this isolate");
    return false;
  }
  return true;
}

__declspec(dllexport) int64_t gov8_bigint_i64_value(v8::Isolate* iso, void* v,
                                                    int64_t* out,
                                                    int32_t* lossless) {
  ClearErr();
  if (out == nullptr || lossless == nullptr) {
    SetErr("null out params");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    // Read-only: a shim-local HandleScope is safe here because no handle
    // escapes this call.
    v8::HandleScope hs(iso);
    v8::Local<v8::Value> val = FromWire<v8::Value>(v);
    if (!val->IsBigInt()) {
      SetErr("not a BigInt");
      return kErrBadArg;
    }
    bool is_lossless = false;
    *out = val.As<v8::BigInt>()->Int64Value(&is_lossless);
    *lossless = is_lossless ? 1 : 0;
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_string_new_utf8(v8::Isolate* iso,
                                                   void* scope, const char* p,
                                                   int64_t len, void** out) {
  ClearErr();
  if (p == nullptr && len > 0) {
    SetErr("null string buffer");
    return kErrBadArg;
  }
  if (out == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::MaybeLocal<v8::String> maybe =
        v8::String::NewFromUtf8(iso, p, v8::NewStringType::kNormal,
                                static_cast<int>(len));
    v8::Local<v8::String> s;
    if (!maybe.ToLocal(&s)) {
      SetErr("String::NewFromUtf8 failed");
      return kErrGeneric;
    }
    *out = ToWire(s);
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_string_length(v8::Isolate* iso, void* v) {
  ClearErr();
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    return FromWire<v8::String>(v)->Length();
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_string_utf8_length(v8::Isolate* iso,
                                                      void* v) {
  ClearErr();
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    return static_cast<int64_t>(FromWire<v8::String>(v)->Utf8Length(iso));
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_string_write_utf8(v8::Isolate* iso, void* v,
                                                     char* buf, int64_t cap) {
  ClearErr();
  if (buf == nullptr && cap > 0) {
    SetErr("null buffer");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::String> s = FromWire<v8::String>(v);
    return static_cast<int64_t>(s->WriteUtf8(
        iso, buf, static_cast<size_t>(cap),
        v8::String::WriteFlags::kReplaceInvalidUtf8));
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

// --- predicates (0/1) ------------------------------------------------------------

#define GOV8_PREDICATE(name, expr)                                          \
  __declspec(dllexport) int64_t name(v8::Isolate* iso, void* v) {           \
    ClearErr();                                                             \
    try {                                                                   \
      Gov8IsolateScope iso_scope(iso);                                    \
      v8::HandleScope hs(iso);                                              \
      v8::Local<v8::Value> val = FromWire<v8::Value>(v);                    \
      return (expr) ? 1 : 0;                                                \
    } catch (...) {                                                         \
      SetErr("C++ exception");                                              \
      return kErrCpp;                                                       \
    }                                                                       \
  }

GOV8_PREDICATE(gov8_is_undefined, val->IsUndefined())
GOV8_PREDICATE(gov8_is_null, val->IsNull())
GOV8_PREDICATE(gov8_is_null_or_undefined, val->IsNullOrUndefined())
GOV8_PREDICATE(gov8_is_boolean, val->IsBoolean())
GOV8_PREDICATE(gov8_is_string, val->IsString())
GOV8_PREDICATE(gov8_is_int32, val->IsInt32())
GOV8_PREDICATE(gov8_is_uint32, val->IsUint32())
GOV8_PREDICATE(gov8_is_number, val->IsNumber())
GOV8_PREDICATE(gov8_is_object, val->IsObject())
GOV8_PREDICATE(gov8_is_array, val->IsArray())
GOV8_PREDICATE(gov8_is_function, val->IsFunction())

#undef GOV8_PREDICATE

// --- direct readers ----------------------------------------------------------------

__declspec(dllexport) int64_t gov8_boolean_value(v8::Isolate* iso, void* v) {
  ClearErr();
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    return FromWire<v8::Value>(v)->BooleanValue(iso) ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_integer_raw_value(v8::Isolate* iso,
                                                     void* v, int64_t* out) {
  ClearErr();
  if (out == nullptr) {
    SetErr("null out param");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    // Read-only: a shim-local HandleScope is safe here because no handle
    // escapes this call.
    v8::HandleScope hs(iso);
    *out = FromWire<v8::Value>(v).As<v8::Integer>()->Value();
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_number_raw_value(v8::Isolate* iso, void* v,
                                                    double* out) {
  ClearErr();
  if (out == nullptr) {
    SetErr("null out param");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    *out = FromWire<v8::Value>(v).As<v8::Number>()->Value();
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

// --- context conversions -------------------------------------------------------------

__declspec(dllexport) int64_t gov8_value_to_string_utf8(v8::Isolate* iso,
                                                        void* ctxw, void* scope,
                                                        void* v,
                                                        char* buf, int64_t cap,
                                                        int64_t* out_len) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (w == nullptr || s == nullptr || out_len == nullptr ||
      !OwnedBy(iso, w->iso) || !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    std::string text;
    if (!ValueText(iso, ctx, FromWire<v8::Value>(v), &text)) {
      return kErrException;
    }
    return BufferUtf8(text, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

#define GOV8_CTX_CONV(name, ctype, call, expr_out)                           \
  __declspec(dllexport) int64_t name(v8::Isolate* iso, void* ctxw, void* v, \
                                     ctype* out, int32_t* ok) {              \
    ClearErr();                                                              \
    CtxWrap* w = AsCtx(ctxw);                                                \
    if (w == nullptr || out == nullptr || ok == nullptr ||                   \
        !OwnedBy(iso, w->iso)) {                                             \
      SetErr("invalid argument");                                            \
      return kErrBadArg;                                                     \
    }                                                                        \
    try {                                                                    \
      Gov8IsolateScope iso_scope(iso);                                     \
      v8::Local<v8::Context> ctx = w->ctx->Get(iso);                         \
      v8::Context::Scope ctx_scope(ctx);                                     \
      ctype value;                                                           \
      if ((call).To(&value)) {                                               \
        expr_out;                                                            \
        *ok = 1;                                                             \
      } else {                                                               \
        *ok = 0;                                                             \
      }                                                                      \
      return kOk;                                                            \
    } catch (...) {                                                          \
      SetErr("C++ exception");                                               \
      return kErrCpp;                                                        \
    }                                                                        \
  }

GOV8_CTX_CONV(gov8_value_number_value, double,
              FromWire<v8::Value>(v)->NumberValue(ctx), *out = value)
GOV8_CTX_CONV(gov8_value_integer_value, int64_t,
              FromWire<v8::Value>(v)->IntegerValue(ctx), *out = value)
GOV8_CTX_CONV(gov8_value_int32_value, int32_t,
              FromWire<v8::Value>(v)->Int32Value(ctx), *out = value)
GOV8_CTX_CONV(gov8_value_uint32_value, uint32_t,
              FromWire<v8::Value>(v)->Uint32Value(ctx), *out = value)

#undef GOV8_CTX_CONV

// --- object property access ----------------------------------------------------------

__declspec(dllexport) int64_t gov8_object_get(v8::Isolate* iso, void* ctxw,
                                              void* scope, void* obj, void* key,
                                              void** out, int32_t* ok) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (w == nullptr || s == nullptr || out == nullptr ||
      ok == nullptr || !OwnedBy(iso, w->iso) || !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Local<v8::Value> result;
    if (FromWire<v8::Object>(obj)->Get(ctx, FromWire<v8::Value>(key))
            .ToLocal(&result)) {
      *out = ToWire(result);
      *ok = 1;
    } else {
      *ok = 0;
    }
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_object_set(v8::Isolate* iso, void* ctxw,
                                              void* scope, void* obj, void* key,
                                              void* val, int32_t* ok) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (w == nullptr || s == nullptr || ok == nullptr ||
      !OwnedBy(iso, w->iso) || !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Maybe<bool> result = FromWire<v8::Object>(obj)->Set(
        ctx, FromWire<v8::Value>(key), FromWire<v8::Value>(val));
    if (result.IsNothing()) {
      *ok = 0;
    } else {
      *ok = result.FromJust() ? 1 : 0;
    }
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

// --- try-catch -------------------------------------------------------------------------

__declspec(dllexport) void* gov8_trycatch_new(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return nullptr;
  }
  try {
    TcWrap* w = new TcWrap{};
    w->magic = kTcMagic;
    w->iso = iso;
    GlobalPlacementNew<v8::TryCatch>(w->tc_storage, iso);
    return w;
  } catch (...) {
    SetErr("C++ exception");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_trycatch_dispose(void* tcw) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr) {
    SetErr("invalid trycatch handle");
    return kErrMagic;
  }
  try {
    w->tc()->~TryCatch();
    delete w;
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

#define GOV8_TC_FLAG(name, expr)                                             \
  __declspec(dllexport) int64_t name(void* tcw) {                            \
    ClearErr();                                                              \
    TcWrap* w = AsTc(tcw);                                                   \
    if (w == nullptr) {                                                      \
      SetErr("invalid trycatch handle");                                     \
      return kErrMagic;                                                      \
    }                                                                        \
    try {                                                                    \
      return (expr) ? 1 : 0;                                                 \
    } catch (...) {                                                          \
      SetErr("C++ exception");                                               \
      return kErrCpp;                                                        \
    }                                                                        \
  }

GOV8_TC_FLAG(gov8_tc_has_caught, w->tc()->HasCaught())
GOV8_TC_FLAG(gov8_tc_can_continue, w->tc()->CanContinue())

#undef GOV8_TC_FLAG

__declspec(dllexport) int64_t gov8_tc_reset(void* tcw) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr) {
    SetErr("invalid trycatch handle");
    return kErrMagic;
  }
  try {
    w->tc()->Reset();
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_message_utf8(v8::Isolate* iso,
                                                   void* tcw, void* ctxw,
                                                   void* scope,
                                                   char* buf, int64_t cap,
                                                   int64_t* out_len) {
  ClearErr();
  TcWrap* t = AsTc(tcw);
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (t == nullptr || w == nullptr || s == nullptr || out_len == nullptr ||
      !OwnedBy(iso, t->iso) || !OwnedBy(iso, w->iso) ||
      !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Local<v8::Message> msg = t->tc()->Message();
    if (msg.IsEmpty()) {
      return BufferUtf8(std::string(), buf, cap, out_len);
    }
    std::string text;
    if (!ValueText(iso, ctx, msg->Get(), &text)) {
      return kErrException;
    }
    return BufferUtf8(text, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_exception_utf8(v8::Isolate* iso,
                                                     void* tcw, void* ctxw,
                                                     void* scope,
                                                     char* buf, int64_t cap,
                                                     int64_t* out_len) {
  ClearErr();
  TcWrap* t = AsTc(tcw);
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (t == nullptr || w == nullptr || s == nullptr || out_len == nullptr ||
      !OwnedBy(iso, t->iso) || !OwnedBy(iso, w->iso) ||
      !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Local<v8::Value> exc = t->tc()->Exception();
    if (exc.IsEmpty()) {
      return BufferUtf8(std::string(), buf, cap, out_len);
    }
    std::string text;
    if (!ValueText(iso, ctx, exc, &text)) {
      return kErrException;
    }
    return BufferUtf8(text, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_exception_is_string(void* tcw) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr) {
    SetErr("invalid trycatch handle");
    return kErrMagic;
  }
  try {
    Gov8IsolateScope iso_scope(w->iso);
    v8::HandleScope hs(w->iso);
    v8::Local<v8::Value> exc = w->tc()->Exception();
    return (!exc.IsEmpty() && exc->IsString()) ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_start_position(v8::Isolate* iso,
                                                     void* tcw, void* scope) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr || !ScopeIs(iso, scope) || !OwnedBy(iso, w->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Message> msg = w->tc()->Message();
    return msg.IsEmpty() ? 0 : msg->GetStartPosition();
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_line_number(v8::Isolate* iso, void* tcw,
                                                  void* ctxw, void* scope,
                                                  int32_t* out, int32_t* ok) {
  ClearErr();
  TcWrap* t = AsTc(tcw);
  CtxWrap* w = AsCtx(ctxw);
  GoScope* s = AsScope(scope);
  if (t == nullptr || w == nullptr || s == nullptr || out == nullptr ||
      ok == nullptr || !OwnedBy(iso, t->iso) || !OwnedBy(iso, w->iso) ||
      !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Local<v8::Message> msg = t->tc()->Message();
    if (msg.IsEmpty()) {
      *ok = 0;
      return kOk;
    }
    v8::Maybe<int> line = msg->GetLineNumber(ctx);
    if (line.IsNothing()) {
      *ok = 0;
    } else {
      *out = line.FromJust();
      *ok = 1;
    }
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_tc_start_column(v8::Isolate* iso,
                                                   void* tcw, void* scope) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr || !ScopeIs(iso, scope) || !OwnedBy(iso, w->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Message> msg = w->tc()->Message();
    return msg.IsEmpty() ? 0 : msg->GetStartColumn();
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

// --- scripts ---------------------------------------------------------------------------

extern "C" const v8::Script* v8__Mimic__CompilePlatformScript(
    const v8::Context*, const v8::String*);

static int64_t CompileScript(v8::Isolate* iso,
                                                  void* ctxw, void* scope,
                                                  void* tcw,
                                                  const char* src, int64_t len,
                                                  void** out_script, bool platform_bootstrap) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  if (w == nullptr || out_script == nullptr || !ScopeIs(iso, scope) ||
      !OwnedBy(iso, w->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (src == nullptr && len > 0) {
    SetErr("null source buffer");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    FallbackTryCatch fallback;
    // A supplied but invalid TryCatch wrapper must fail loudly instead of
    // silently falling back to the shim-internal TryCatch (which would
    // swallow the caller's exception details).
    v8::TryCatch* tc = nullptr;
    if (tcw != nullptr) {
      TcWrap* tw = AsTc(tcw);
      if (tw == nullptr) {
        SetErr("invalid trycatch handle");
        return kErrMagic;
      }
      if (!OwnedBy(iso, tw->iso)) {
        return kErrBadArg;
      }
      tc = TcOf(tw);
    }
    if (tc == nullptr) tc = fallback.Init(iso);
    v8::Local<v8::String> source;
    if (!v8::String::NewFromUtf8(iso, src, v8::NewStringType::kNormal,
                                 static_cast<int>(len))
             .ToLocal(&source)) {
      SetErr("String::NewFromUtf8 failed");
      return kErrGeneric;
    }
    v8::Local<v8::Script> script;
    bool compiled;
    if (platform_bootstrap) {
      const v8::Script* result = v8__Mimic__CompilePlatformScript(
          static_cast<const v8::Context*>(ToWire(ctx)),
          static_cast<const v8::String*>(ToWire(source)));
      compiled = result != nullptr;
      if (compiled) script = FromWire<v8::Script>(const_cast<v8::Script*>(result));
    } else {
      compiled = v8::Script::Compile(ctx, source).ToLocal(&script);
    }
    if (!compiled) {
      SetErr("compile failed");
      return kErrException;
    }
    ScriptWrap* sw = new ScriptWrap{};
    sw->magic = kScriptMagic;
    sw->iso = iso;
    sw->script = new v8::Global<v8::Script>(iso, script);
    *out_script = sw;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in script_compile");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_script_compile(
    v8::Isolate* iso, void* ctxw, void* scope, void* tcw,
    const char* src, int64_t len, void** out_script) {
  return CompileScript(iso, ctxw, scope, tcw, src, len, out_script, false);
}

__declspec(dllexport) int64_t gov8_script_compile_platform(
    v8::Isolate* iso, void* ctxw, void* scope, void* tcw,
    const char* src, int64_t len, void** out_script) {
  return CompileScript(iso, ctxw, scope, tcw, src, len, out_script, true);
}

__declspec(dllexport) int64_t gov8_script_run(v8::Isolate* iso, void* ctxw,
                                              void* scope, void* scriptw,
                                              void* tcw, void** out_result) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  ScriptWrap* s = AsScript(scriptw);
  if (w == nullptr) {
    SetErr("invalid context handle");
    return kErrBadArg;
  }
  if (s == nullptr) {
    SetErr("invalid script handle");
    return kErrBadArg;
  }
  if (AsScope(scope) == nullptr) {
    SetErr("invalid scope handle");
    return kErrBadArg;
  }
  if (out_result == nullptr) {
    SetErr("null result output");
    return kErrBadArg;
  }
  if (!OwnedBy(iso, w->iso)) {
    SetErr("context belongs to another isolate");
    return kErrBadArg;
  }
  if (!OwnedBy(iso, s->iso)) {
    SetErr("script belongs to another isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    FallbackTryCatch fallback;
    // See gov8_script_compile: a supplied but invalid or foreign TryCatch
    // wrapper must fail loudly instead of falling back.
    v8::TryCatch* tc = nullptr;
    if (tcw != nullptr) {
      TcWrap* tw = AsTc(tcw);
      if (tw == nullptr) {
        SetErr("invalid trycatch handle");
        return kErrMagic;
      }
      if (!OwnedBy(iso, tw->iso)) {
        return kErrBadArg;
      }
      tc = TcOf(tw);
    }
    if (tc == nullptr) tc = fallback.Init(iso);
    v8::Local<v8::Value> result;
    if (!s->script->Get(iso)->Run(ctx).ToLocal(&result)) {
      SetErr("run failed");
      return kErrException;
    }
    *out_result = ToWire(result);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in script_run");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_script_id(void* scriptw, int32_t* out) {
  ClearErr();
  ScriptWrap* s = AsScript(scriptw);
  if (s == nullptr || out == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(s->iso);
    v8::HandleScope hs(s->iso);
    *out = s->script->Get(s->iso)->GetUnboundScript()->GetId();
    return kOk;
  } catch (...) {
    SetErr("C++ exception in script_id");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_script_dispose(void* scriptw) {
  ClearErr();
  ScriptWrap* s = AsScript(scriptw);
  if (s == nullptr) {
    SetErr("invalid script handle");
    return kErrMagic;
  }
  try {
    Gov8IsolateScope iso_scope(s->iso);
    s->script->Reset();
    delete s->script;
    delete s;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in script_dispose");
    return kErrCpp;
  }
}

// --- microtasks --------------------------------------------------------------------------

__declspec(dllexport) int64_t gov8_isolate_set_microtasks_policy(
    v8::Isolate* iso, int32_t policy) {
  ClearErr();
  if (policy < 0 || policy > 1) {
    SetErr("invalid policy");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    iso->SetMicrotasksPolicy(policy == 0 ? v8::MicrotasksPolicy::kAuto
                                         : v8::MicrotasksPolicy::kExplicit);
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_isolate_get_microtasks_policy(
    v8::Isolate* iso) {
  ClearErr();
  try {
    Gov8IsolateScope iso_scope(iso);
    return iso->GetMicrotasksPolicy() == v8::MicrotasksPolicy::kAuto ? 0 : 1;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_isolate_perform_microtask_checkpoint(
    v8::Isolate* iso) {
  ClearErr();
  try {
    Gov8IsolateScope iso_scope(iso);
    iso->PerformMicrotaskCheckpoint();
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) void* gov8_microtask_queue_new(v8::Isolate* iso,
                                                     int32_t policy) {
  ClearErr();
  if (policy < 0 || policy > 1) {
    SetErr("invalid policy");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    MqWrap* w = new MqWrap{};
    w->magic = kMqMagic;
    w->iso = iso;
    w->handle = v8__MicrotaskQueueHandle__New(
        iso, policy == 0 ? v8::MicrotasksPolicy::kAuto
                         : v8::MicrotasksPolicy::kExplicit);
    if (w->handle == nullptr) {
      delete w;
      SetErr("MicrotaskQueue::New failed");
      return nullptr;
    }
    w->queue = v8__MicrotaskQueueHandle__Get(w->handle);
    return w;
  } catch (...) {
    SetErr("C++ exception");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_microtask_queue_dispose(void* mqw) {
  ClearErr();
  MqWrap* w = AsMq(mqw);
  if (w == nullptr) {
    SetErr("invalid microtask queue handle");
    return kErrMagic;
  }
  try {
    v8__MicrotaskQueueHandle__DELETE(w->handle);
    delete w;
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) void* gov8_microtask_queue_raw(void* mqw) {
  ClearErr();
  MqWrap* w = AsMq(mqw);
  if (w == nullptr) {
    SetErr("invalid microtask queue handle");
    return nullptr;
  }
  return w->queue;
}

extern "C" bool v8__MicrotaskQueue__IsRunningMicrotasks(v8::MicrotaskQueue*);
extern "C" int v8__MicrotaskQueue__GetMicrotasksScopeDepth(v8::MicrotaskQueue*);

__declspec(dllexport) int64_t gov8_context_set_microtask_queue(void* ctxw,
                                                               void* mqw) {
  ClearErr();
  CtxWrap* c = AsCtx(ctxw);
  MqWrap* m = mqw == nullptr ? nullptr : AsMq(mqw);
  if (c == nullptr || (mqw != nullptr && m == nullptr)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (m != nullptr && !OwnedBy(c->iso, m->iso)) return kErrBadArg;
  try {
    Gov8IsolateScope iso_scope(c->iso);
    v8::HandleScope hs(c->iso);
    auto context = c->ctx->Get(c->iso);
    auto previous = context->GetMicrotaskQueue();
    if (previous == nullptr) {
      if (m == nullptr) return kOk;
      SetErr("cannot attach a queue to a shutdown context");
      return kErrState;
    }
    // Queue virtual layouts differ with V8's cppgc build mode; query through
    // the pinned flat bindings, never through header-compiled virtual calls.
    // V8's setter requires an outer boundary. Report misuse before its fatal
    // API checks; shutdown preserves the context and its retained objects.
    if (v8__MicrotaskQueue__IsRunningMicrotasks(previous) ||
        v8__MicrotaskQueue__GetMicrotasksScopeDepth(previous) != 0 ||
        !c->iso->GetEnteredOrMicrotaskContext().IsEmpty()) {
      SetErr("microtask queue change requires an idle context boundary");
      return kErrState;
    }
    context->SetMicrotaskQueue(m == nullptr ? nullptr : m->queue);
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) void* gov8_context_get_microtask_queue(void* ctxw) {
  ClearErr();
  CtxWrap* c = AsCtx(ctxw);
  if (c == nullptr) {
    SetErr("invalid context handle");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(c->iso);
    v8::HandleScope hs(c->iso);
    return c->ctx->Get(c->iso)->GetMicrotaskQueue();
  } catch (...) {
    SetErr("C++ exception");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_microtask_queue_perform_checkpoint(
    v8::Isolate* iso, void* mqw, void* ctxw) {
  ClearErr();
  MqWrap* w = AsMq(mqw);
  CtxWrap* c = AsCtx(ctxw);
  if (w == nullptr) {
    SetErr("invalid microtask queue handle");
    return kErrMagic;
  }
  if (!OwnedBy(iso, w->iso) || (c != nullptr && !OwnedBy(iso, c->iso))) {
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    // The oracle performs checkpoints with the working context still
    // entered (a long-lived ContextScope); mirror that when a context is
    // supplied.
    v8::Local<v8::Context> ctx;
    std::optional<v8::Context::Scope> ctx_scope;
    if (c != nullptr) {
      ctx = c->ctx->Get(iso);
      ctx_scope.emplace(ctx);
    }
    v8__MicrotaskQueue__PerformCheckpoint(iso, w->queue);
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_microtask_queue_enqueue(v8::Isolate* iso,
                                                           void* mqw,
                                                           void* ctxw,
                                                           void* fn) {
  ClearErr();
  MqWrap* w = AsMq(mqw);
  CtxWrap* c = AsCtx(ctxw);
  if (w == nullptr) {
    SetErr("invalid microtask queue handle");
    return kErrMagic;
  }
  if (!OwnedBy(iso, w->iso) || (c != nullptr && !OwnedBy(iso, c->iso))) {
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    // The oracle enqueues with the working context entered; mirror that when
    // a context is supplied (the engine resolves the current native context
    // for the callback task).
    v8::Local<v8::Context> ctx;
    std::optional<v8::Context::Scope> ctx_scope;
    if (c != nullptr) {
      ctx = c->ctx->Get(iso);
      ctx_scope.emplace(ctx);
    }
    v8::HandleScope hs(iso);
    v8::Local<v8::Value> val = FromWire<v8::Value>(fn);
    if (!val->IsFunction()) {
      SetErr("microtask callback is not a function");
      return kErrBadArg;
    }
    v8__MicrotaskQueue__EnqueueMicrotask(iso, w->queue, *val.As<v8::Function>());
    return kOk;
  } catch (...) {
    SetErr("C++ exception");
    return kErrCpp;
  }
}

// --- diagnostics ---------------------------------------------------------------------------

__declspec(dllexport) int64_t gov8_last_error(char* buf, int64_t cap) {
  return CopyOut(tls_error.data(), tls_error.size(), buf, cap);
}

}  // extern "C"

// Feature slices are separate textual includes so independent implementations
// can share the core wrapper types without editing this file concurrently.
#include "features/templates_callbacks.inc"
#include "features/fast_api.inc"
#include "features/promises.inc"
#include "features/modules.inc"
#include "features/modules_synthetic.inc"
#include "features/module_cache.inc"
#include "features/simdutf.inc"
#include "features/icu.inc"
#include "features/wasm_core.inc"
#include "features/wasm_streaming.inc"
#include "features/wasm_cache_positive.inc"
#include "features/wasm_policy_callbacks.inc"
#include "features/buffers_serialization.inc"
#include "features/snapshots_handles.inc"
#include "features/heap_snapshot.inc"
#include "features/module_advanced_residual.inc"
#include "features/context_residual.inc"
#include "features/runtime_values.inc"
#include "features/runtime_values_residual.inc"
#include "features/core_advanced.inc"
#include "features/script_compiler_residual.inc"
#include "features/exceptions_advanced.inc"
#include "features/functions_advanced.inc"
#include "features/context_scopes_advanced.inc"
#include "features/array_buffer_allocator.inc"
#include "features/isolate_advanced.inc"
#include "features/exception_constructors.inc"
#include "features/inspector_transport.inc"
#include "features/inspector_session_controls.inc"
#include "features/inspector_client_callbacks.inc"
#include "features/inspector_client_values.inc"
#include "features/inspector_object_wrapping.inc"
#include "features/crdtp_core.inc"
#include "features/crdtp_dispatcher.inc"
#include "features/inspector_inspected_object.inc"
#include "features/inspector_runtime_events.inc"
#include "features/trycatch_listener_residual.inc"
#include "features/serialization_delegates.inc"
#include "features/serializer_wasm_legacy.inc"
#include "features/object_ops.inc"
#include "features/object_callback_retention.inc"
#include "features/object_residual.inc"
#include "features/cppgc_object_wrapping.inc"
#include "features/cppgc_persistent.inc"
#include "features/cppgc_member.inc"
#include "features/cppgc_heap_lifecycle.inc"
#include "features/cppgc_generic_residual.inc"
#include "features/cppgc_generic_graph.inc"
#include "features/strings_bigint.inc"
#include "features/typed_arrays.inc"
#include "features/fixed_primitive_arrays.inc"
#include "features/controls_hooks.inc"
#include "features/external_references.inc"
#include "features/property_observation.inc"
#include "features/create_params_snapshot.inc"
#include "features/handles_residual.inc"
#include "features/platform.inc"
#include "features/platform_custom.inc"

#ifndef _WIN32
// The legacy Windows ABI mirrors positional integer arguments into XMM
// registers. SysV assigns floating arguments independently. Normalize these
// five legacy exports to pointer words before entering the common shim.
static double Gov8WordDouble(uint64_t word) {
  double value;
  std::memcpy(&value, &word, sizeof(value));
  return value;
}
extern "C" {
__attribute__((visibility("default")))
void* gov8_number_new_words(v8::Isolate* iso, uint64_t value, void* scope) {
  return gov8_number_new(iso, Gov8WordDouble(value), scope);
}
__attribute__((visibility("default")))
int64_t gov8_rv_set_double_words(uintptr_t rv, uint64_t value) {
  return gov8_rv_set_double(rv, Gov8WordDouble(value));
}
__attribute__((visibility("default")))
int64_t gov8_rv_date_new_words(v8::Isolate* iso, void* context, uint64_t value, void* scope, void** out) {
  return gov8_rv_date_new(iso, context, Gov8WordDouble(value), scope, out);
}
__attribute__((visibility("default")))
int64_t gov8_platform_run_idle_tasks_words(v8::Isolate* iso, uint64_t value) {
  return gov8_platform_run_idle_tasks(iso, Gov8WordDouble(value));
}
__attribute__((visibility("default")))
int64_t gov8_pc_idle_task_run_delete_words(v8::IdleTask* task, uint64_t value) {
  return gov8_pc_idle_task_run_delete(task, Gov8WordDouble(value));
}
}
#endif

#include "features/receiver_dispatch.inc"
#include "features/generated_lazy.inc"
#include "features/generated_constructor.inc"
#include "features/exception_state.inc"
