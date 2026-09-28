// ===========================================================================
// Controls & hooks feature slice: safe deterministic V8 flags and pre-init
// rules (flags, usage strings, WebAssembly trap handling and entropy), the
// process fatal-error handler, per-isolate OOM and near-heap-limit handlers,
// GC requests and clear-kept-objects, memory-pressure and low-memory
// notifications, atomics-wait toggle, idle and timezone notifications,
// background-task polling, promise hooks and reject callbacks, the
// prepare-stack-trace hook, the use counter, the
// modify-code-generation-from-strings hook, and message listeners.
//
// Excluded by product scope: modules, Wasm and inspector surfaces.
//
// Dispatch design (mirrors features/templates_callbacks.inc):
//   - Go cannot generate code at runtime, so every engine callback shape is
//     served by ONE static C++ trampoline per kind. The trampolines funnel
//     through a single Go dispatcher entry point (gov8_ch_set_dispatcher,
//     registered with syscall.NewCallback on the Go side) that receives a
//     ChFrame on the trampoline's C++ stack.
//   - No Go pointer ever crosses into the engine. Routing is by
//     (kind, engine isolate pointer): the Go registry stores one entry per
//     registered hook slot keyed by the engine isolate it was registered
//     for (engine pointer 0 for the process-global entropy/fatal slots).
//     A freshly created engine isolate starts with every hook slot empty,
//     so a recycled isolate address can only dispatch a hook that the Go
//     side re-registered for it — stale entries can never fire.
//   - The trampolines that receive v8::Local parameters copy the one-word
//     handle slots verbatim into the frame (the wire format, see shim.cc).
//     Their slots live in the CALLER's open HandleScope, which stays open
//     for the whole synchronous dispatch; a hook kind that must CREATE
//     values (prepare-stack-trace return, codegen modified source) is given
//     a GoScope owned by the trampoline (frame.scope) so values created by
//     Go remain valid until the trampoline returns.
//   - A Go panic in a hook is recovered by the Go dispatcher and converted
//     into the process fail-fast abort (gov8_host_panic_abort), matching the
//     native-callback panic boundary of the other slices: unwinding a Go
//     panic through engine C++ frames is unsupported.
//
// Fatal/OOM/near-heap-limit semantics are process-fatal by design: the Go
// fatal handler only OBSERVES (file, line, message); when it returns, the
// engine aborts the process exactly like the pinned Rust oracle (V8 then
// prints the "# Fatal error" banner and raises STATUS_BREAKPOINT). There is
// deliberately no recovery path.
// ===========================================================================

#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <string>

// --- artifact bindings used by this slice (compiled into the static lib) ---
//
// Declared at file scope: identical C-linkage redeclarations of names other
// slices also declare are legal and denote the same engine functions.

extern "C" {
void v8__V8__SetFlagsFromCommandLine(int* argc, char** argv,
                                     const char* usage);
void v8__V8__SetFlagsFromString(const char* flags, size_t length);
bool v8__V8__EnableWebAssemblyTrapHandler(bool use_v8_signal_handler);
void v8__V8__SetEntropySource(v8::EntropySource callback);
void v8__V8__SetFatalErrorHandler(v8::V8FatalErrorCallback that);
void v8__Isolate__MemoryPressureNotification(v8::Isolate* isolate,
                                             v8::MemoryPressureLevel level);
void v8__Isolate__ClearKeptObjects(v8::Isolate* isolate);
void v8__Isolate__LowMemoryNotification(v8::Isolate* isolate);
void v8__Isolate__SetIdle(v8::Isolate* isolate, bool is_idle);
void v8__Isolate__SetPrepareStackTraceCallback(v8::Isolate* isolate,
                                               v8::PrepareStackTraceCallback cb);
void v8__Isolate__SetPromiseHook(v8::Isolate* isolate, v8::PromiseHook hook);
void v8__Isolate__SetPromiseRejectCallback(v8::Isolate* isolate,
                                           v8::PromiseRejectCallback callback);
void v8__Isolate__SetUseCounterCallback(v8::Isolate* isolate,
                                        v8::Isolate::UseCounterCallback cb);
void v8__Isolate__SetModifyCodeGenerationFromStringsCallback(
    v8::Isolate* isolate, v8::ModifyCodeGenerationFromStringsCallback2 cb);
bool v8__Isolate__AddMessageListener(v8::Isolate* isolate,
                                     v8::MessageCallback callback);
bool v8__Isolate__AddMessageListenerWithErrorLevel(v8::Isolate* isolate,
                                                   v8::MessageCallback callback,
                                                   int error_level);
void v8__Isolate__AddNearHeapLimitCallback(v8::Isolate* isolate,
                                           v8::NearHeapLimitCallback callback,
                                           void* data);
void v8__Isolate__RemoveNearHeapLimitCallback(v8::Isolate* isolate,
                                              v8::NearHeapLimitCallback cb,
                                              size_t heap_limit);
void v8__Isolate__SetOOMErrorHandler(v8::Isolate* isolate,
                                     v8::OOMErrorCallback callback);
void v8__Isolate__SetAllowAtomicsWait(v8::Isolate* isolate, bool allow);
bool v8__Isolate__HasPendingBackgroundTasks(v8::Isolate* isolate);
void v8__Isolate__RequestGarbageCollectionForTesting(
    v8::Isolate* isolate, v8::Isolate::GarbageCollectionType type);
void v8__Isolate__DateTimeConfigurationChangeNotification(
    v8::Isolate* isolate, v8::Isolate::TimeZoneDetection time_zone_detection);
void v8__Context__AllowCodeGenerationFromStrings(v8::Context& self, bool allow);
bool v8__Context_IsCodeGenerationFromStringsAllowed(v8::Context& self);
int v8__Message__ErrorLevel(const v8::Message& self);
int v8__Message__GetLineNumber(const v8::Message& self,
                               const v8::Context& context);
int v8__Message__GetStartPosition(const v8::Message& self);
int v8__Message__GetEndPosition(const v8::Message& self);
const v8::Message* v8__Exception__CreateMessage(v8::Isolate* isolate,
                                                const v8::Value& exception);
}

// Hook dispatch kinds (mirrored by the Go chKind* constants; keep in sync).
namespace gov8_ch_impl {
constexpr int32_t chKindEntropy = 0;
constexpr int32_t chKindFatal = 1;
constexpr int32_t chKindOOM = 2;
constexpr int32_t chKindNearHeapLimit = 3;
constexpr int32_t chKindPromiseHook = 4;
constexpr int32_t chKindPrepareStackTrace = 5;
constexpr int32_t chKindUseCounter = 6;
constexpr int32_t chKindCodegen = 7;
constexpr int32_t chKindMessageListener = 8;

// The frame passed to the Go dispatcher. All wires are one-word handle slots
// valid in the scope that owns them (the engine caller's scope for incoming
// wires, the trampoline-owned GoScope for outgoing wires).
struct ChFrame {
  int32_t kind;
  int32_t pad;
  // Engine isolate pointer this dispatch belongs to (0 for the
  // process-global entropy/fatal slots). The Go registry keys on it.
  uintptr_t engine;
  // Hook-specific words: raw pointers for string-carrying hooks (entropy
  // buffer, fatal file/message, OOM location/detail), handle-slot wires for
  // value-carrying hooks, plain integers for enum/scalar payloads.
  uintptr_t a;
  uintptr_t b;
  uintptr_t c;
  // Trampoline-owned GoScope for hook kinds that may create values
  // (prepare-stack-trace, codegen); nullptr otherwise.
  void* scope;
  // Out slot: address of the returned Local (prepare-stack-trace) or of the
  // returned ModifyCodeGenerationFromStringsResult (codegen; bool at +0,
  // MaybeLocal wire at +8). nullptr when the hook returns a scalar.
  void* out;
};

// The single Go dispatcher entry point (a syscall.NewCallback value on the
// Go side). Zero until gov8_ch_set_dispatcher runs; set exactly once.
uintptr_t g_go_ch_dispatch = 0;

int64_t CallChDispatcher(ChFrame* frame) {
  if (g_go_ch_dispatch == 0) {
    // A hook fired but the Go side never registered its dispatcher: hard
    // programming error, fail loudly like the other slices.
    std::abort();
  }
  using DispatchFn = uintptr_t (*)(uintptr_t);
  return static_cast<int64_t>(reinterpret_cast<DispatchFn>(g_go_ch_dispatch)(
      reinterpret_cast<uintptr_t>(frame)));
}

// Trampoline-owned GoScope helpers (the same wrapper shape the core
// gov8_scope_enter/exit exports use, kept local so hook dispatch has no
// re-entry through exported symbols).
GoScope* ChScopeOpen(v8::Isolate* iso) {
  GoScope* s = new GoScope{};
  s->magic = kScopeMagic;
  s->iso = iso;
  GlobalPlacementNew<v8::HandleScope>(s->hs_storage, iso);
  return s;
}

void ChScopeClose(GoScope* s) {
  if (s == nullptr) return;
  s->hs()->~HandleScope();
  delete s;
}

// v8::Local::operator* yields T* (not T&), so passing a dereferenced local
// to a const-T& binding requires this helper (same shape as the snapshots
// slice's DerefLocal).
template <class T>
T& ChDeref(const v8::Local<T>& local) {
  return *local.operator->();
}

// --- static engine-callback trampolines -------------------------------------

bool ChEntropyTrampoline(unsigned char* buffer, size_t length) {
  ChFrame frame{};
  frame.kind = chKindEntropy;
  frame.engine = 0;  // process-global slot
  frame.a = reinterpret_cast<uintptr_t>(buffer);
  frame.b = static_cast<uintptr_t>(length);
  return CallChDispatcher(&frame) != 0;
}

void ChFatalTrampoline(const char* file, int line, const char* message) {
  ChFrame frame{};
  frame.kind = chKindFatal;
  frame.engine = 0;  // process-global slot
  frame.a = reinterpret_cast<uintptr_t>(file);
  frame.b = reinterpret_cast<uintptr_t>(message);
  frame.c = static_cast<uintptr_t>(static_cast<int64_t>(line));
  CallChDispatcher(&frame);
  // Returning lets V8 abort the process (the pinned, intended semantics:
  // there is no recovery from a fatal CHECK).
}

void ChOomTrampoline(const char* location, const v8::OOMDetails& details) {
  ChFrame frame{};
  frame.kind = chKindOOM;
  frame.engine = reinterpret_cast<uintptr_t>(v8::Isolate::TryGetCurrent());
  frame.a = reinterpret_cast<uintptr_t>(location);
  frame.b = reinterpret_cast<uintptr_t>(details.detail);
  frame.c = details.is_heap_oom ? 1 : 0;
  CallChDispatcher(&frame);
}

size_t ChNearHeapLimitTrampoline(void* data, size_t current_heap_limit,
                                 size_t initial_heap_limit) {
  ChFrame frame{};
  frame.kind = chKindNearHeapLimit;
  frame.engine = reinterpret_cast<uintptr_t>(v8::Isolate::TryGetCurrent());
  frame.a = static_cast<uintptr_t>(current_heap_limit);
  frame.b = static_cast<uintptr_t>(initial_heap_limit);
  return static_cast<size_t>(CallChDispatcher(&frame));
}

void ChPromiseHookTrampoline(v8::PromiseHookType type,
                             v8::Local<v8::Promise> promise,
                             v8::Local<v8::Value> parent) {
  ChFrame frame{};
  frame.kind = chKindPromiseHook;
  frame.engine = reinterpret_cast<uintptr_t>(v8::Isolate::TryGetCurrent());
  frame.a = static_cast<uintptr_t>(static_cast<int32_t>(type));
  frame.b = reinterpret_cast<uintptr_t>(ToWire(promise));
  frame.c = reinterpret_cast<uintptr_t>(ToWire(parent));
  CallChDispatcher(&frame);
}

v8::MaybeLocal<v8::Value> ChPrepareStackTraceTrampoline(
    v8::Local<v8::Context> context, v8::Local<v8::Value> error,
    v8::Local<v8::Array> sites) {
  v8::Isolate* iso = v8::Isolate::TryGetCurrent();
  // The callback may create the returned value. Go creates it inside the
  // trampoline-owned GoScope; Escape then moves the slot into the ENGINE's
  // scope level so it survives this trampoline's return (the engine reads
  // the result after the callback returns and CHECKs IsValue()).
  v8::EscapableHandleScope esc(iso);
  GoScope* gs = nullptr;
  try {
    gs = ChScopeOpen(iso);
  } catch (...) {
    gs = nullptr;  // OOM at the boundary: report an empty result.
  }
  v8::Local<v8::Value> result;
  ChFrame frame{};
  frame.kind = chKindPrepareStackTrace;
  frame.engine = reinterpret_cast<uintptr_t>(iso);
  frame.a = reinterpret_cast<uintptr_t>(ToWire(context));
  frame.b = reinterpret_cast<uintptr_t>(ToWire(error));
  frame.c = reinterpret_cast<uintptr_t>(ToWire(sites));
  frame.scope = gs;
  frame.out = &result;
  CallChDispatcher(&frame);
  v8::Local<v8::Value> escaped = esc.Escape(result);
  ChScopeClose(gs);
  // An empty Local converts to an empty MaybeLocal; an escaped wire stays
  // valid in the engine's scope.
  return v8::MaybeLocal<v8::Value>(escaped);
}

void ChUseCounterTrampoline(v8::Isolate* isolate,
                            v8::Isolate::UseCounterFeature feature) {
  ChFrame frame{};
  frame.kind = chKindUseCounter;
  frame.engine = reinterpret_cast<uintptr_t>(isolate);
  frame.a = static_cast<uintptr_t>(static_cast<int32_t>(feature));
  CallChDispatcher(&frame);
}

v8::ModifyCodeGenerationFromStringsResult ChCodegenTrampoline(
    v8::Local<v8::Context> context, v8::Local<v8::Value> source,
    bool is_code_like) {
  v8::Isolate* iso = v8::Isolate::TryGetCurrent();
  // Default (false, empty): also the answer if scope allocation fails.
  v8::ModifyCodeGenerationFromStringsResult result{};
  // Go creates the modified source inside the trampoline-owned GoScope;
  // Escape moves the slot into the engine's scope level so it survives
  // this trampoline's return.
  v8::EscapableHandleScope esc(iso);
  GoScope* gs = nullptr;
  try {
    gs = ChScopeOpen(iso);
  } catch (...) {
    gs = nullptr;
  }
  v8::Local<v8::Value> modified;
  ChFrame frame{};
  frame.kind = chKindCodegen;
  frame.engine = reinterpret_cast<uintptr_t>(iso);
  frame.a = reinterpret_cast<uintptr_t>(ToWire(context));
  frame.b = reinterpret_cast<uintptr_t>(ToWire(source));
  frame.c = is_code_like ? 1 : 0;
  frame.scope = gs;
  frame.out = &modified;
  // Go dispatcher return: 0 = block, 1 = allow unchanged, 2 = allow with a
  // modified-source wire written through frame.out.
  const uintptr_t verdict = CallChDispatcher(&frame);
  if (verdict == 2 && !modified.IsEmpty()) {
    result.codegen_allowed = true;
    result.modified_source = esc.Escape(modified).As<v8::String>();
  } else if (verdict == 1) {
    result.codegen_allowed = true;
  }
  ChScopeClose(gs);
  return result;
}

void ChMessageListenerTrampoline(v8::Local<v8::Message> message,
                                 v8::Local<v8::Value> exception) {
  v8::Isolate* iso = v8::Isolate::TryGetCurrent();
  ChFrame frame{};
  frame.kind = chKindMessageListener;
  frame.engine = reinterpret_cast<uintptr_t>(iso);
  frame.a = reinterpret_cast<uintptr_t>(ToWire(message));
  frame.b = reinterpret_cast<uintptr_t>(ToWire(exception));
  // Capture the engine's entered-or-microtask context for the Go-side
  // message getters (empty when none is entered).
  v8::Local<v8::Context> ctx;
  if (iso != nullptr) {
    ctx = iso->GetEnteredOrMicrotaskContext();
  }
  frame.c = reinterpret_cast<uintptr_t>(ToWire(ctx));
  CallChDispatcher(&frame);
}

}  // namespace gov8_ch_impl

extern "C" {

// --- dispatcher registration --------------------------------------------------

__declspec(dllexport) int64_t gov8_ch_set_dispatcher(uintptr_t dispatch) {
  ClearErr();
  if (gov8_ch_impl::g_go_ch_dispatch != 0 &&
      gov8_ch_impl::g_go_ch_dispatch != dispatch) {
    SetErr("controls-hooks dispatcher already registered");
    return kErrState;
  }
  gov8_ch_impl::g_go_ch_dispatch = dispatch;
  return kOk;
}

// --- flags / entropy / fatal (process-level, pre-init rules live in Go) -------

__declspec(dllexport) int64_t gov8_ch_enable_wasm_trap_handler(
    int32_t use_v8_signal_handler) {
  ClearErr();
  if (use_v8_signal_handler != 0 && use_v8_signal_handler != 1) {
    SetErr("invalid signal-handler flag");
    return kErrBadArg;
  }
  try {
    return v8__V8__EnableWebAssemblyTrapHandler(
               use_v8_signal_handler != 0)
               ? 1
               : 0;
  } catch (...) {
    SetErr("C++ exception in enable_web_assembly_trap_handler");
    return kErrCpp;
  }
}

// Passes the command line to the engine (recognized flags are consumed and
// *argc shrinks) without a custom usage prefix.
__declspec(dllexport) int64_t gov8_ch_set_flags_from_command_line(
    int32_t* argc, char** argv) {
  ClearErr();
  if (argc == nullptr || *argc <= 0 || argv == nullptr) {
    SetErr("null out params");
    return kErrBadArg;
  }
  try {
    int c = static_cast<int>(*argc);
    v8__V8__SetFlagsFromCommandLine(&c, argv, nullptr);
    *argc = static_cast<int32_t>(c);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_flags_from_command_line");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_flags_from_command_line_with_usage(
    int32_t* argc, char** argv, const char* usage) {
  ClearErr();
  if (argc == nullptr || *argc <= 0 || argv == nullptr || usage == nullptr) {
    SetErr("null argument");
    return kErrBadArg;
  }
  try {
    int c = static_cast<int>(*argc);
    v8__V8__SetFlagsFromCommandLine(&c, argv, usage);
    *argc = static_cast<int32_t>(c);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_flags_from_command_line_with_usage");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_flags_from_string(const char* ptr,
                                                            int64_t len) {
  ClearErr();
  if (len < 0 || (ptr == nullptr && len > 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    // Post-initialization value-changing flag writes are FATAL by engine
    // design (frozen flag set, "Check failed: !IsFrozen()."); they are
    // characterized out-of-process and never guarded away here.
    v8__V8__SetFlagsFromString(ptr, static_cast<size_t>(len));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_flags_from_string");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_entropy_source(void) {
  ClearErr();
  try {
    v8__V8__SetEntropySource(gov8_ch_impl::ChEntropyTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_entropy_source");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_fatal_error_handler(void) {
  ClearErr();
  try {
    v8__V8__SetFatalErrorHandler(gov8_ch_impl::ChFatalTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_fatal_error_handler");
    return kErrCpp;
  }
}

// --- isolate creation with explicit heap limits -------------------------------

// Creates an isolate with CreateParams::constraints configured from the
// given heap-size pair (the CreateParams::heap_limits(0, 10 MiB) path of the
// pinned oracle). The heap ceiling makes every heap-pressure workload the
// intended, bounded fatal path instead of uncontrolled process growth.
__declspec(dllexport) void* gov8_ch_isolate_new_heap_limits(int64_t initial,
                                                            int64_t maximum) {
  ClearErr();
  if (initial < 0 || maximum < 0) {
    SetErr("negative heap limit");
    return nullptr;
  }
  if (g_platform == nullptr) {
    SetErr("v8 not initialized");
    return nullptr;
  }
  try {
    v8::Isolate::CreateParams params;
    params.array_buffer_allocator = g_allocator.get();
    params.constraints.ConfigureDefaultsFromHeapSize(
        static_cast<size_t>(initial), static_cast<size_t>(maximum));
    v8::Isolate* iso = Gov8NewIndependentIsolate(params);
    if (iso == nullptr) {
      SetErr("Isolate::New failed");
      return nullptr;
    }
    // Entered-owned-isolate model, same as gov8_isolate_new.
    iso->Enter();
    return iso;
  } catch (...) {
    SetErr("C++ exception in isolate_new_heap_limits");
    return nullptr;
  }
}

// --- isolate controls -----------------------------------------------------------

__declspec(dllexport) int64_t gov8_ch_request_gc(v8::Isolate* iso,
                                                 int32_t type) {
  ClearErr();
  if (iso == nullptr || (type != 0 && type != 1)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    // Without --expose-gc this site fails a fatal CHECK in the pinned build
    // ("Must use --expose-gc") and aborts; the Go wrapper documents the
    // requirement and never guards it away.
    v8__Isolate__RequestGarbageCollectionForTesting(
        iso, static_cast<v8::Isolate::GarbageCollectionType>(type));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in request_gc");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_clear_kept_objects(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__ClearKeptObjects(iso);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in clear_kept_objects");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_memory_pressure(v8::Isolate* iso,
                                                      int32_t level) {
  ClearErr();
  if (iso == nullptr || level < 0 || level > 2) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__MemoryPressureNotification(
        iso, static_cast<v8::MemoryPressureLevel>(level));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in memory_pressure");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_low_memory(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__LowMemoryNotification(iso);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in low_memory");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_allow_atomics_wait(v8::Isolate* iso,
                                                             int32_t allow) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetAllowAtomicsWait(iso, allow != 0);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_allow_atomics_wait");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_idle(v8::Isolate* iso,
                                               int32_t idle) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetIdle(iso, idle != 0);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_idle");
    return kErrCpp;
  }
}

// Returns 1/0, negative on misuse.
__declspec(dllexport) int64_t gov8_ch_has_pending_background_tasks(
    v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    return v8__Isolate__HasPendingBackgroundTasks(iso) ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception in has_pending_background_tasks");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_date_time_config_change(
    v8::Isolate* iso, int32_t detection) {
  ClearErr();
  if (iso == nullptr || detection < 0 || detection > 1) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__DateTimeConfigurationChangeNotification(
        iso, static_cast<v8::Isolate::TimeZoneDetection>(detection));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in date_time_config_change");
    return kErrCpp;
  }
}

// --- hook registration ------------------------------------------------------------

__declspec(dllexport) int64_t gov8_ch_set_promise_hook(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetPromiseHook(iso,
                                gov8_ch_impl::ChPromiseHookTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_promise_hook");
    return kErrCpp;
  }
}

// (The promise-reject callback registration is owned by the promises slice,
// which has its own trampoline and registry; this slice does not duplicate
// it.)

__declspec(dllexport) int64_t gov8_ch_set_prepare_stack_trace(
    v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetPrepareStackTraceCallback(
        iso, gov8_ch_impl::ChPrepareStackTraceTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_prepare_stack_trace");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_use_counter(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetUseCounterCallback(iso,
                                       gov8_ch_impl::ChUseCounterTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_use_counter");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_codegen_callback(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetModifyCodeGenerationFromStringsCallback(
        iso, gov8_ch_impl::ChCodegenTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_codegen_callback");
    return kErrCpp;
  }
}

// Registers the isolate's single all-levels engine listener (the Go side
// fans out to per-registration listeners with level filtering). Returns the
// engine's bool.
__declspec(dllexport) int64_t gov8_ch_add_message_listener_wl(
    v8::Isolate* iso, int32_t level) {
  ClearErr();
  if (iso == nullptr || level <= 0 || level > 0x1F) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    return v8__Isolate__AddMessageListenerWithErrorLevel(
               iso, gov8_ch_impl::ChMessageListenerTrampoline,
               static_cast<int>(level))
               ? 1
               : 0;
  } catch (...) {
    SetErr("C++ exception in add_message_listener_wl");
    return kErrCpp;
  }
}

// Registers (or replaces) the near-heap-limit callback. The engine CHECKs
// against registering the same function pointer twice, so a replacement is
// modeled as remove-then-add: had_previous must be 1 exactly when this
// isolate already has this slice's trampoline registered (tracked on the Go
// side; the engine keeps one logical slot — only the most recently added
// callback is ever invoked).
__declspec(dllexport) int64_t gov8_ch_add_near_heap_limit(v8::Isolate* iso,
                                                          int32_t had_previous) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    if (had_previous != 0) {
      // heap_limit 0: removal without restoring a limit (no pressure is
      // active at registration time).
      v8__Isolate__RemoveNearHeapLimitCallback(
          iso, gov8_ch_impl::ChNearHeapLimitTrampoline, 0);
    }
    v8__Isolate__AddNearHeapLimitCallback(
        iso, gov8_ch_impl::ChNearHeapLimitTrampoline, nullptr);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in add_near_heap_limit");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_remove_near_heap_limit(
    v8::Isolate* iso, int64_t heap_limit) {
  ClearErr();
  if (iso == nullptr || heap_limit < 0) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__RemoveNearHeapLimitCallback(
        iso, gov8_ch_impl::ChNearHeapLimitTrampoline,
        static_cast<size_t>(heap_limit));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in remove_near_heap_limit");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_set_oom_error_handler(v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Isolate__SetOOMErrorHandler(iso, gov8_ch_impl::ChOomTrampoline);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in set_oom_error_handler");
    return kErrCpp;
  }
}

// --- context codegen toggle ---------------------------------------------------

// The context wrapper belongs to the templates slice; this slice accepts the
// same wrapper pointer type without touching its internals (validated by the
// shared AsCtx tag check).
__declspec(dllexport) int64_t gov8_ch_context_allow_codegen_from_strings(
    void* ctxw, int32_t allow) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  if (w == nullptr) {
    SetErr("invalid context handle");
    return kErrMagic;
  }
  try {
    Gov8IsolateScope iso_scope(w->iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(w->iso);
    v8__Context__AllowCodeGenerationFromStrings(gov8_ch_impl::ChDeref(ctx), allow != 0);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in context_allow_codegen_from_strings");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ch_context_codegen_allowed(void* ctxw) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  if (w == nullptr) {
    SetErr("invalid context handle");
    return kErrMagic;
  }
  try {
    Gov8IsolateScope iso_scope(w->iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(w->iso);
    return v8__Context_IsCodeGenerationFromStringsAllowed(gov8_ch_impl::ChDeref(ctx)) ? 1
                                                                        : 0;
  } catch (...) {
    SetErr("C++ exception in context_codegen_allowed");
    return kErrCpp;
  }
}

// --- callback-time readers ------------------------------------------------------
//
// These run inside an active hook dispatch: the wires live in scopes that
// outlast the synchronous Go callback. Every export validates the Go scope
// wrapper and re-enters the isolate through Gov8IsolateScope (already
// current on the owner thread, so it is a compare).

// Runs a compiled script with NO TryCatch active, so an exception escapes
// to the isolate's message listeners (the core gov8_script_run installs a
// fallback TryCatch, which would swallow the message). The wrapper structs
// are the core slice's; the validation mirrors gov8_script_run.
__declspec(dllexport) int64_t gov8_ch_script_run_uncaught(v8::Isolate* iso,
                                                          void* ctxw,
                                                          void* scope,
                                                          void* scriptw,
                                                          void** out_result) {
  ClearErr();
  CtxWrap* w = AsCtx(ctxw);
  ScriptWrap* s = AsScript(scriptw);
  if (w == nullptr || s == nullptr || AsScope(scope) == nullptr ||
      out_result == nullptr || !OwnedBy(iso, w->iso) ||
      !OwnedBy(iso, s->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = w->ctx->Get(iso);
    v8::Context::Scope ctx_scope(ctx);
    v8::Local<v8::Value> result;
    if (!s->script->Get(iso)->Run(ctx).ToLocal(&result)) {
      SetErr("run failed");
      return kErrException;
    }
    *out_result = ToWire(result);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in ch_script_run_uncaught");
    return kErrCpp;
  }
}

// ECMAScript ToString of a callback-delivered value, UTF-8 (lossy). The
// context is the raw wire captured by the trampoline (may be null; the
// engine requires a live context for the conversion).
__declspec(dllexport) int64_t gov8_ch_value_text_utf8(v8::Isolate* iso,
                                                      void* ctx_wire,
                                                      void* scope,
                                                      void* wire, char* buf,
                                                      int64_t cap,
                                                      int64_t* out_len) {
  ClearErr();
  if (wire == nullptr || out_len == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Context> ctx = FromWire<v8::Context>(ctx_wire);
    if (ctx.IsEmpty()) {
      SetErr("no current context for conversion");
      return kErrState;
    }
    v8::Context::Scope ctx_scope(ctx);
    std::string text;
    if (!ValueText(iso, ctx, FromWire<v8::Value>(wire), &text)) {
      return kErrException;
    }
    return BufferUtf8(text, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception in ch_value_text_utf8");
    return kErrCpp;
  }
}

// Message::Get text (the "Uncaught ..." message string), UTF-8. No context
// needed (the engine takes only the isolate).
__declspec(dllexport) int64_t gov8_ch_message_text_utf8(v8::Isolate* iso,
                                                        void* scope,
                                                        void* msg, char* buf,
                                                        int64_t cap,
                                                        int64_t* out_len) {
  ClearErr();
  if (msg == nullptr || out_len == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Message> m = FromWire<v8::Message>(msg);
    v8::Local<v8::String> text = m->Get();
    if (text.IsEmpty()) {
      SetErr("message text unavailable");
      return kErrState;
    }
    const size_t n = text->Utf8Length(iso);
    std::string out(static_cast<size_t>(n), '\0');
    if (n > 0) {
      text->WriteUtf8(iso, out.data(), n,
                      v8::String::WriteFlags::kReplaceInvalidUtf8);
    }
    return BufferUtf8(out, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception in ch_message_text_utf8");
    return kErrCpp;
  }
}

// 1-based line; ok=0 when the Maybe is empty (the binding maps Nothing to
// -1). Needs the context wire captured by the message-listener trampoline.
__declspec(dllexport) int64_t gov8_ch_message_line_number(
    v8::Isolate* iso, void* ctx_wire, void* scope, void* msg, int32_t* out,
    int32_t* ok) {
  ClearErr();
  if (msg == nullptr || out == nullptr || ok == nullptr ||
      !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Context> ctx = FromWire<v8::Context>(ctx_wire);
    if (ctx.IsEmpty()) {
      SetErr("no current context for message");
      return kErrState;
    }
    const int line = v8__Message__GetLineNumber(
        gov8_ch_impl::ChDeref(FromWire<v8::Message>(msg)), gov8_ch_impl::ChDeref(ctx));
    *out = static_cast<int32_t>(line);
    *ok = line >= 0 ? 1 : 0;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in ch_message_line_number");
    return kErrCpp;
  }
}

#define GOV8_CH_MSG_SIMPLE(name, expr)                                         \
  __declspec(dllexport) int64_t name(v8::Isolate* iso, void* scope,            \
                                     void* msg) {                              \
    ClearErr();                                                                \
    if (msg == nullptr || !ScopeIs(iso, scope)) {                              \
      SetErr("invalid argument");                                              \
      return kErrBadArg;                                                       \
    }                                                                          \
    try {                                                                      \
      Gov8IsolateScope iso_scope(iso);                                         \
      v8::HandleScope hs(iso);                                                 \
      v8::Local<v8::Message> m = FromWire<v8::Message>(msg);                   \
      return static_cast<int64_t>(expr);                                       \
    } catch (...) {                                                            \
      SetErr("C++ exception in message getter");                               \
      return kErrCpp;                                                          \
    }                                                                          \
  }

GOV8_CH_MSG_SIMPLE(gov8_ch_message_error_level,
                   v8__Message__ErrorLevel(gov8_ch_impl::ChDeref(m)))
GOV8_CH_MSG_SIMPLE(gov8_ch_message_start_position, m->GetStartPosition())
GOV8_CH_MSG_SIMPLE(gov8_ch_message_end_position, m->GetEndPosition())

#undef GOV8_CH_MSG_SIMPLE

// Exception::CreateMessage(...)->Get() text for a callback-delivered
// exception value (the prepare-stack-trace formatting path), UTF-8.
__declspec(dllexport) int64_t gov8_ch_exception_message_text_utf8(
    v8::Isolate* iso, void* scope, void* exc, char* buf, int64_t cap,
    int64_t* out_len) {
  ClearErr();
  if (exc == nullptr || out_len == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Value> e = FromWire<v8::Value>(exc);
    // The binding returns the raw handle-slot pointer; re-wrap it with the
    // same trivially-copyable word copy FromWire uses.
    const v8::Message* raw = v8__Exception__CreateMessage(iso, gov8_ch_impl::ChDeref(e));
    v8::Local<v8::Message> m;
    std::memcpy(&m, &raw, sizeof(void*));
    if (m.IsEmpty()) {
      SetErr("message creation failed");
      return kErrState;
    }
    v8::Local<v8::String> text = m->Get();
    if (text.IsEmpty()) {
      SetErr("message text unavailable");
      return kErrState;
    }
    const size_t n = text->Utf8Length(iso);
    std::string out(static_cast<size_t>(n), '\0');
    if (n > 0) {
      text->WriteUtf8(iso, out.data(), n,
                      v8::String::WriteFlags::kReplaceInvalidUtf8);
    }
    return BufferUtf8(out, buf, cap, out_len);
  } catch (...) {
    SetErr("C++ exception in ch_exception_message_text_utf8");
    return kErrCpp;
  }
}

// v8::Array::Length of a callback-delivered call-site array.
__declspec(dllexport) int64_t gov8_ch_array_length(v8::Isolate* iso,
                                                   void* scope, void* arr) {
  ClearErr();
  if (arr == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8::Local<v8::Array> a = FromWire<v8::Array>(arr);
    if (a.IsEmpty()) {
      SetErr("not an array");
      return kErrBadArg;
    }
    return static_cast<int64_t>(a->Length());
  } catch (...) {
    SetErr("C++ exception in ch_array_length");
    return kErrCpp;
  }
}

}  // extern "C"
