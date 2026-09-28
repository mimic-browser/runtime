// ===========================================================================
// Snapshot / startup-data / public persistent (Global) / weak handle /
// thread-safe termination feature slice.
//
// Exports (all invoked from Go through the dynamic proc lookup):
//   - SnapshotCreator construction from scratch or from an existing startup
//     blob, default/added contexts, isolate and context snapshot data, and
//     CreateBlob with both FunctionCodeHandling policies.
//   - Startup blob validation with a GO-SIDE GUARD: StartupData::IsValid on
//     data shorter than the snapshot version header trips a V8 CHECK and
//     aborts the process in the pinned crate (characterized upstream as
//     mode=invalid-startup-data-fatal); this shim answers `invalid` for such
//     data without touching the engine, and every consumer entry point
//     (isolate creation, creator construction, IsValid) validates before any
//     fatal engine call.
//   - Isolate creation from a startup blob (CreateParams::snapshot_blob).
//   - Exactly-once snapshot-data retrieval with the engine's NoData/BadType
//     outcome distinction (the raw data is fetched before the type check, so
//     a BadType request consumes the slot -- engine behavior, mirrored).
//   - Public strong (Global) and weak (Weak) persistent handles over the
//     artifact's own v8__Global__* / v8__WeakCallbackInfo__* bindings, with
//     first/second-pass weak callbacks dispatched to Go through an INTEGER
//     callback registry (no Go pointers ever cross the boundary).
//   - Low-memory notification and terminate/cancel/is-terminating entry
//     points (the terminate pair is safe to call from any thread, matching
//     the pinned crate's thread-safe IsolateHandle).
//
// Wire validity: local-handle wires are slot words created in the CALLER's
// open HandleScope (the Go-owned scope, validated via ScopeIs) and are only
// valid for that call. Persistent cells (Global) are one-word engine handles
// obtained from the artifact bindings and freed by v8__Global__Reset.
//
// Lifecycle notes:
//   - The engine isolate of a SnapshotCreator is owned by CreatorWrap.
//     Creator destruction exits it before the wrapper disposes it, so it
//     is NEVER disposed through gov8_isolate_dispose; the Go wrapper marks
//     its Isolate closed and unregisters it when CreateBlob or
//     gov8_snapshot_creator_dispose runs.
//   - Engine copies of startup blobs handed to isolate/creator construction
//     stay alive for the lifetime of the consuming engine object (V8 reads
//     the blob on every Context::New of a snapshot-backed isolate), and are
//     freed deterministically: with the creator on creator disposal, and for
//     consumer isolates via gov8_snapshot_blob_release once the Go wrapper
//     proves every isolate created from the blob is closed.
//   - WeakWrap objects are freed only by gov8_weak_close (Go Weak.Close);
//     weak callbacks only flip flags on the wrap, never free it, so a wrap
//     pointer held by Go stays valid until Close.
// ===========================================================================

#include <atomic>
#include <cstring>
#include <memory>
#include <mutex>
#include <vector>

namespace gov8_external_references_impl {
void DropTable(v8::Isolate* isolate) noexcept;
}  // namespace gov8_external_references_impl

namespace gov8_snap_impl {

constexpr uint64_t kCreatorMagic = 0x674f5638534e5053ULL;  // "gOV8SNPS"
constexpr uint64_t kWeakMagic = 0x674f56385745414bULL;     // "gOV8WEAK"
constexpr uint64_t kBlobMagic = 0x674f5638424c4f42ULL;     // "gOV8BLOB"

// Snapshot version header layout (src/snapshot/snapshot.cc, SnapshotImpl):
// VersionIsValid CHECKs raw_size > kVersionStringOffset + kVersionStringLength
// and aborts the process otherwise. Data at or below this bound never reaches
// the engine through this shim.
constexpr int64_t kVersionStringOffset = 16;
constexpr int64_t kVersionStringLength = 64;
constexpr int64_t kMinValidRawSize = kVersionStringOffset + kVersionStringLength + 1;  // 81

// --- artifact bindings (compiled into the pinned static library) -----------

using WeakCallback = void (*)(const v8::WeakCallbackInfo<void>&);

extern "C" {
const v8::Data* v8__Local__New(v8::Isolate* isolate, const v8::Data& other);
const v8::Data* v8__Global__New(v8::Isolate* isolate, const v8::Data& other);
const v8::Data* v8__Global__NewWeak(v8::Isolate* isolate,
                                    const v8::Data& other, void* parameter,
                                    WeakCallback callback);
void v8__Global__Reset(const v8::Data* data);
// v8__Data__EQ is declared by buffers_serialization.inc (pointers, ABI
// identical to references) and is visible here in the same TU.
v8::Isolate* v8__WeakCallbackInfo__GetIsolate(
    const v8::WeakCallbackInfo<void>* self);
void* v8__WeakCallbackInfo__GetParameter(const v8::WeakCallbackInfo<void>* self);
void v8__WeakCallbackInfo__SetSecondPassCallback(
    const v8::WeakCallbackInfo<void>* self, WeakCallback callback);
void v8__SnapshotCreator__DESTRUCT(void* self);
v8::Isolate* v8__SnapshotCreator__GetIsolate(const void* self);
void v8__SnapshotCreator__SetDefaultContext(void* self,
                                            const v8::Context& context);
size_t v8__SnapshotCreator__AddContext(void* self, const v8::Context& context);
size_t v8__SnapshotCreator__AddData_to_isolate(void* self,
                                               const v8::Data& data);
size_t v8__SnapshotCreator__AddData_to_context(void* self,
                                               const v8::Context& context,
                                               const v8::Data& data);
// Returns by value; MSVC x64 returns the 16-byte StartupData through a
// hidden sret pointer, so the declaration keeps the struct return type.
v8::StartupData v8__SnapshotCreator__CreateBlob(
    void* self, v8::SnapshotCreator::FunctionCodeHandling function_code_handling);
bool v8__StartupData__IsValid(const v8::StartupData& self);
bool v8__StartupData__CanBeRehashed(const v8::StartupData& self);
void v8__StartupData__data__DELETE(const char* data);
const v8::Data* v8__Isolate__GetDataFromSnapshotOnce(v8::Isolate* isolate,
                                                    size_t index);
const v8::Data* v8__Context__GetDataFromSnapshotOnce(v8::Context& self,
                                                    size_t index);
const v8::Context* v8__Context__FromSnapshot(
    v8::Isolate* isolate, size_t context_snapshot_index,
    v8::Value* global_object, v8::MicrotaskQueue* microtask_queue);
void v8__Isolate__LowMemoryNotification(v8::Isolate* isolate);
void v8__Isolate__TerminateExecution(v8::Isolate* isolate);
bool v8__Isolate__IsExecutionTerminating(v8::Isolate* isolate);
void v8__Isolate__CancelTerminateExecution(v8::Isolate* isolate);
}

// --- wrapper structs ---------------------------------------------------------

// v8::SnapshotCreator is a single pointer (SnapshotCreatorImpl*) in the
// pinned artifact. Operations and destruction use its flat bindings above.
// Construction uses matching pinned headers and the caller-owned-isolate
// overload to select a private group.
constexpr size_t kCreatorStorageSize = 128;

struct CreatorWrap {
  uint64_t magic;
  v8::Isolate* iso;
  // Heap copy of an existing startup blob handed to construction (null when
  // the creator is built from scratch). Freed on creator disposal.
  struct BlobHolder* blob;
  alignas(16) unsigned char sc_storage[kCreatorStorageSize];
};

// Snapshot builders need the same independent-group ownership as consumers.
// This SnapshotCreator overload initializes and enters a caller-owned isolate;
// its destructor exits that isolate but does not dispose it.
void ConstructIndependentCreator(CreatorWrap* wrapper,
                                 const v8::Isolate::CreateParams& params) {
  wrapper->iso = v8::Isolate::Allocate(v8::IsolateGroup::Create());
  static_assert(sizeof(v8::SnapshotCreator) <= kCreatorStorageSize);
  new (wrapper->sc_storage) v8::SnapshotCreator(wrapper->iso, params);
}

void DestroyIndependentCreator(CreatorWrap* wrapper) {
  v8__SnapshotCreator__DESTRUCT(wrapper->sc_storage);
  wrapper->iso->Dispose();
}

struct BlobHolder {
  uint64_t magic;
  v8::StartupData data;  // private copy or shared immutable bytes
  std::shared_ptr<const struct SharedBlobBytes> shared;
};

struct SharedBlobBytes {
  std::unique_ptr<char[]> bytes;
  size_t size;
};

// Snapshot input is immutable after serialization. V8 may read it again when
// an isolate creates a context, so each holder keeps the shared bytes alive
// until its isolate is disposed. The mutex covers construction only; Pages
// continue to execute in independent isolates without a shared runtime lock.
std::mutex shared_blob_mutex;
std::vector<std::weak_ptr<const SharedBlobBytes>> shared_blobs;

struct WeakWrap {
  uint64_t magic;
  v8::Isolate* iso;
  const v8::Data* cell;  // weak global-handle cell; null after first pass
  int64_t go_id;         // Go finalizer registry id; 0 = none/cancelled
  bool first_pass_done;
  bool second_pass_done;
};

CreatorWrap* AsCreator(void* p) {
  if (p == nullptr) return nullptr;
  CreatorWrap* w = static_cast<CreatorWrap*>(p);
  return w->magic == kCreatorMagic ? w : nullptr;
}

BlobHolder* AsBlob(void* p) {
  if (p == nullptr) return nullptr;
  BlobHolder* h = static_cast<BlobHolder*>(p);
  return h->magic == kBlobMagic ? h : nullptr;
}

WeakWrap* AsWeak(void* p) {
  if (p == nullptr) return nullptr;
  WeakWrap* w = static_cast<WeakWrap*>(p);
  return w->magic == kWeakMagic ? w : nullptr;
}

void FreeBlobHolder(BlobHolder* h) {
  if (h == nullptr) return;
  if (h->data.data != nullptr && !h->shared) {
    v8__StartupData__data__DELETE(h->data.data);
    h->data.data = nullptr;
  }
  h->magic = 0;
  delete h;
}

// Copies `len` bytes into a fresh engine-owned holder for creator construction.
// The copy (not the Go buffer) backs CreateParams.
BlobHolder* HoldBlob(const char* p, int64_t len) {
  BlobHolder* h = new BlobHolder{};
  h->magic = kBlobMagic;
  h->data.raw_size = static_cast<int>(len);
  if (len > 0) {
    char* copy = new char[static_cast<size_t>(len)];
    std::memcpy(copy, p, static_cast<size_t>(len));
    h->data.data = copy;
  }
  return h;
}

BlobHolder* HoldSharedBlob(const char* p, int64_t len) {
  const size_t size = static_cast<size_t>(len);
  std::shared_ptr<const SharedBlobBytes> shared;
  {
    std::lock_guard<std::mutex> lock(shared_blob_mutex);
    for (auto it = shared_blobs.begin(); it != shared_blobs.end();) {
      auto candidate = it->lock();
      if (!candidate) {
        it = shared_blobs.erase(it);
        continue;
      }
      if (candidate->size == size &&
          std::memcmp(candidate->bytes.get(), p, size) == 0) {
        shared = std::move(candidate);
        break;
      }
      ++it;
    }
    if (!shared) {
      auto created = std::make_shared<SharedBlobBytes>();
      created->bytes = std::make_unique<char[]>(size);
      std::memcpy(created->bytes.get(), p, size);
      created->size = size;
      shared = created;
      shared_blobs.emplace_back(shared);
    }
  }
  BlobHolder* holder = new BlobHolder{};
  holder->magic = kBlobMagic;
  holder->data.data = shared->bytes.get();
  holder->data.raw_size = static_cast<int>(size);
  holder->shared = std::move(shared);
  return holder;
}

// Builds CreateParams mirroring the pinned oracle defaults: default array
// buffer allocator, external references absent, optional snapshot blob.
void FillCreateParams(v8::Isolate::CreateParams* params, BlobHolder* blob) {
  *params = v8::Isolate::CreateParams{};
  params->array_buffer_allocator = g_allocator.get();
  if (blob != nullptr) {
    params->snapshot_blob = &blob->data;
  }
}

// Go trampoline for weak-finalizer dispatch: (id) -> ignored. The registry
// entry carries the callback and its owning isolate; a missing id means the
// Weak was closed and the finalizer cancelled.
using WeakEntry = uintptr_t (*)(int64_t id);
WeakEntry g_weak_entry = nullptr;

// First pass: V8 clears the handle; the cell is reset and the wrap records
// the transition (mirrors the pinned crate: pointer.take() + Global::Reset).
// Guaranteed to run on the isolate's thread (during GC or isolate teardown).
void WeakSecondPass(const v8::WeakCallbackInfo<void>& info);

void WeakFirstPass(const v8::WeakCallbackInfo<void>& info) {
  WeakWrap* w = static_cast<WeakWrap*>(info.GetParameter());
  if (w == nullptr || w->magic != kWeakMagic) {
    return;
  }
  if (w->cell != nullptr) {
    v8__Global__Reset(w->cell);
    w->cell = nullptr;
  }
  w->first_pass_done = true;
  // Only schedule the second pass when a finalizer may still run.
  if (w->go_id != 0) {
    info.SetSecondPassCallback(WeakSecondPass);
  }
}

// Second pass: runs the Go finalizer exactly once (if not cancelled by
// Weak.Close). The wrap is intentionally NOT freed here: Go frees it in
// gov8_weak_close, so the wrap pointer held by the Go Weak stays valid.
void WeakSecondPass(const v8::WeakCallbackInfo<void>& info) {
  WeakWrap* w = static_cast<WeakWrap*>(info.GetParameter());
  if (w == nullptr || w->magic != kWeakMagic) {
    return;
  }
  w->second_pass_done = true;
  const int64_t id = w->go_id;
  w->go_id = 0;
  WeakEntry entry = g_weak_entry;
  if (id != 0 && entry != nullptr) {
    entry(id);
  }
}

// v8::Local's operator* returns T* (the V8 API keeps the slot address), so
// bindings taking `T&` need this explicit deref of the slot value.
template <class T>
T& DerefLocal(const v8::Local<T>& local) {
  return *local.operator->();
}

// Data downcast predicates for typed snapshot-data retrieval. `want`:
// 0 = Value, 1 = String, 2 = Private. The engine fetches the raw data from
// the snapshot BEFORE the type check, so a BadType request consumes the
// slot exactly like the pinned crate (upstream caveat, characterized).
bool DataMatches(int32_t want, void* wire) {
  v8::Local<v8::Data> d = FromWire<v8::Data>(wire);
  switch (want) {
    case 0:
      return d->IsValue();
    case 1:
      return d->IsValue() && v8::Local<v8::Value>::Cast(d)->IsString();
    case 2:
      return d->IsPrivate();
    default:
      return false;
  }
}

}  // namespace gov8_snap_impl

extern "C" {

using namespace gov8_snap_impl;

// --- weak finalizer registry entry ------------------------------------------

__declspec(dllexport) int64_t gov8_weak_set_entry(uintptr_t entry) {
  ClearErr();
  gov8_snap_impl::g_weak_entry =
      reinterpret_cast<gov8_snap_impl::WeakEntry>(entry);
  return kOk;
}

// --- startup blob guards ------------------------------------------------------

// Guarded validity check: data at or below the version-header bound is
// reported invalid WITHOUT touching the engine (the engine's IsValid would
// CHECK-abort). Returns 1/0 through *out.
__declspec(dllexport) int64_t gov8_snapshot_blob_is_valid(const char* p,
                                                          int64_t len,
                                                          int32_t* out) {
  ClearErr();
  if (out == nullptr || (p == nullptr && len > 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (len < gov8_snap_impl::kMinValidRawSize) {
    *out = 0;
    return kOk;
  }
  v8::StartupData tmp;
  tmp.data = p;
  tmp.raw_size = static_cast<int>(len);
  try {
    *out = v8__StartupData__IsValid(tmp) ? 1 : 0;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_blob_is_valid");
    return kErrCpp;
  }
}

// Guarded rehashability query. As with IsValid, truncated input never reaches
// V8 because its snapshot header checks are fatal rather than recoverable.
__declspec(dllexport) int64_t gov8_snapshot_blob_can_be_rehashed(
    const char* p, int64_t len, int32_t* out) {
  ClearErr();
  if (out == nullptr || (p == nullptr && len > 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (len < gov8_snap_impl::kMinValidRawSize) {
    *out = 0;
    return kOk;
  }
  v8::StartupData tmp;
  tmp.data = p;
  tmp.raw_size = static_cast<int>(len);
  try {
    *out = v8__StartupData__CanBeRehashed(tmp) ? 1 : 0;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_blob_can_be_rehashed");
    return kErrCpp;
  }
}

// --- snapshot creator ---------------------------------------------------------

// Creates a creator isolate from scratch (blob_data == null) or from an
// existing startup blob. Returns the CreatorWrap through *out_wrap and the
// engine isolate through *out_iso. The engine isolate is owned by the
// CreatorWrap and must only be released through CreateBlob or
// gov8_snapshot_creator_dispose.
__declspec(dllexport) int64_t gov8_snapshot_creator_new(
    const char* blob_data, int64_t blob_len, void** out_wrap,
    v8::Isolate** out_iso) {
  ClearErr();
  if (out_wrap == nullptr || out_iso == nullptr ||
      (blob_data == nullptr && blob_len > 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (g_platform == nullptr) {
    SetErr("v8 not initialized");
    return kErrState;
  }
  try {
    CreatorWrap* w = new CreatorWrap{};
    w->magic = kCreatorMagic;
    w->blob = nullptr;
    if (blob_len >= gov8_snap_impl::kMinValidRawSize) {
      w->blob = gov8_snap_impl::HoldBlob(blob_data, blob_len);
    }
    v8::Isolate::CreateParams params;
    gov8_snap_impl::FillCreateParams(&params, w->blob);
    gov8_snap_impl::ConstructIndependentCreator(w, params);
    if (v8__SnapshotCreator__GetIsolate(w->sc_storage) != w->iso) {
      gov8_snap_impl::DestroyIndependentCreator(w);
      gov8_snap_impl::FreeBlobHolder(w->blob);
      delete w;
      SetErr("SnapshotCreator isolate ownership mismatch");
      return kErrState;
    }
    *out_wrap = w;
    *out_iso = w->iso;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_creator_new");
    return kErrCpp;
  }
}

// Destroys the C++ SnapshotCreator, then disposes its exited isolate and
// releases the existing-snapshot blob copy. The Go wrapper must have marked
// its Isolate closed before calling this.
__declspec(dllexport) int64_t gov8_snapshot_creator_dispose(void* creatorw) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  if (w == nullptr) {
    SetErr("invalid creator handle");
    return kErrMagic;
  }
  try {
    v8::Isolate* isolate = w->iso;
    gov8_snap_impl::DestroyIndependentCreator(w);
    gov8_external_references_impl::DropTable(isolate);
    gov8_snap_impl::FreeBlobHolder(w->blob);
    w->magic = 0;
    delete w;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_creator_dispose");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_snapshot_set_default_context(
    v8::Isolate* iso, void* creatorw, void* ctxw) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  CtxWrap* c = AsCtx(ctxw);
  if (w == nullptr || c == nullptr || !OwnedBy(iso, w->iso) ||
      !OwnedBy(iso, c->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    v8__SnapshotCreator__SetDefaultContext(
        w->sc_storage, DerefLocal(c->ctx->Get(iso)));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_set_default_context");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_snapshot_add_context(v8::Isolate* iso,
                                                        void* creatorw,
                                                        void* ctxw,
                                                        int64_t* out_index) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  CtxWrap* c = AsCtx(ctxw);
  if (w == nullptr || c == nullptr || out_index == nullptr ||
      !OwnedBy(iso, w->iso) || !OwnedBy(iso, c->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    *out_index = static_cast<int64_t>(v8__SnapshotCreator__AddContext(
        w->sc_storage, DerefLocal(c->ctx->Get(iso))));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_add_context");
    return kErrCpp;
  }
}

// The data wire lives in the CALLER's open HandleScope (Go-owned scope).
__declspec(dllexport) int64_t gov8_snapshot_add_isolate_data(
    v8::Isolate* iso, void* scope, void* creatorw, void* data_wire,
    int64_t* out_index) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  if (w == nullptr || out_index == nullptr || data_wire == nullptr ||
      !ScopeIs(iso, scope) || !OwnedBy(iso, w->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    *out_index = static_cast<int64_t>(v8__SnapshotCreator__AddData_to_isolate(
        w->sc_storage, DerefLocal(FromWire<v8::Data>(data_wire))));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_add_isolate_data");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_snapshot_add_context_data(
    v8::Isolate* iso, void* scope, void* creatorw, void* ctxw,
    void* data_wire, int64_t* out_index) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  CtxWrap* c = AsCtx(ctxw);
  if (w == nullptr || c == nullptr || out_index == nullptr ||
      data_wire == nullptr || !ScopeIs(iso, scope) ||
      !OwnedBy(iso, w->iso) || !OwnedBy(iso, c->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = c->ctx->Get(iso);
    *out_index = static_cast<int64_t>(v8__SnapshotCreator__AddData_to_context(
        w->sc_storage, DerefLocal(ctx),
        DerefLocal(FromWire<v8::Data>(data_wire))));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_add_context_data");
    return kErrCpp;
  }
}

// Creates the startup blob. The engine-owned data pointer is returned
// through *out_data and must be consumed with gov8_snapshot_blob_read_delete.
// The creator is consumed on success AND on engine failure (the C++
// SnapshotCreator always takes the isolate with it), so this must be the
// last call for the creator.
__declspec(dllexport) int64_t gov8_snapshot_create_blob(
    v8::Isolate* iso, void* creatorw, int32_t keep_function_code,
    const char** out_data, int64_t* out_size) {
  ClearErr();
  CreatorWrap* w = AsCreator(creatorw);
  if (w == nullptr || out_data == nullptr || out_size == nullptr ||
      !OwnedBy(iso, w->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    v8::Isolate* creator_isolate = w->iso;
    v8::StartupData blob;
    {
      // Scoped tightly: DestroyIndependentCreator below EXITS AND DELETES
      // the engine isolate, so the scope must not outlive this block.
      Gov8IsolateScope iso_scope(iso);
      const v8::SnapshotCreator::FunctionCodeHandling policy =
          keep_function_code != 0
              ? v8::SnapshotCreator::FunctionCodeHandling::kKeep
              : v8::SnapshotCreator::FunctionCodeHandling::kClear;
      blob = v8__SnapshotCreator__CreateBlob(w->sc_storage, policy);
    }
    // Tear down the creator and its caller-owned isolate together.
    gov8_snap_impl::DestroyIndependentCreator(w);
    gov8_external_references_impl::DropTable(creator_isolate);
    gov8_snap_impl::FreeBlobHolder(w->blob);
    w->magic = 0;
    delete w;
    if (blob.data == nullptr || blob.raw_size <= 0) {
      SetErr("SnapshotCreator::CreateBlob failed");
      return kErrState;
    }
    *out_data = blob.data;
    *out_size = static_cast<int64_t>(blob.raw_size);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in snapshot_create_blob");
    return kErrCpp;
  }
}

// Copies the engine-owned blob data into the Go buffer and deletes the
// engine allocation (the pinned crate frees the same buffer when its
// StartupData is dropped; the Go StartupData holds a Go copy afterwards).
__declspec(dllexport) int64_t gov8_snapshot_blob_read_delete(
    const char* data, int64_t size, char* buf, int64_t cap) {
  ClearErr();
  if (data == nullptr || buf == nullptr || size <= 0 || cap < size) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  std::memcpy(buf, data, static_cast<size_t>(size));
  v8__StartupData__data__DELETE(data);
  return kOk;
}

// --- isolate creation from a startup blob -------------------------------------

// Creates a consumer isolate whose CreateParams carries the startup blob.
// The blob copy is returned through *out_blob and MUST stay alive for the
// isolate's lifetime (V8 reads it on every Context::New of a snapshot-backed
// isolate); release it with gov8_snapshot_blob_release once every isolate
// created from it is closed.
__declspec(dllexport) int64_t gov8_isolate_new_snapshot(
    const char* blob_data, int64_t blob_len, void** out_blob,
    v8::Isolate** out_iso) {
  ClearErr();
  if (out_blob == nullptr || out_iso == nullptr ||
      (blob_data == nullptr && blob_len > 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (g_platform == nullptr) {
    SetErr("v8 not initialized");
    return kErrState;
  }
  BlobHolder* holder = nullptr;
  try {
    holder = gov8_snap_impl::HoldSharedBlob(blob_data, blob_len);
    v8::Isolate::CreateParams params;
    gov8_snap_impl::FillCreateParams(&params, holder);
    v8::Isolate* iso = Gov8NewIndependentIsolate(params);
    if (iso == nullptr) {
      gov8_snap_impl::FreeBlobHolder(holder);
      SetErr("Isolate::New failed");
      return kErrState;
    }
    // Entered-owned-isolate model, same as gov8_isolate_new: this isolate
    // is disposed through gov8_isolate_dispose, which exits the creation
    // Enter before Isolate::Dispose, so it must be entered here.
    iso->Enter();
    *out_blob = holder;
    *out_iso = iso;
    return kOk;
  } catch (...) {
    gov8_snap_impl::FreeBlobHolder(holder);
    SetErr("C++ exception in isolate_new_snapshot");
    return kErrCpp;
  }
}

// Releases an engine blob copy handed out by gov8_isolate_new_snapshot. The
// GO wrapper only calls this after every isolate created from the blob has
// been closed; the guard keeps a premature release from use-after-free.
__declspec(dllexport) int64_t gov8_snapshot_blob_release(void* blobw) {
  ClearErr();
  BlobHolder* h = AsBlob(blobw);
  if (h == nullptr) {
    SetErr("invalid blob handle");
    return kErrMagic;
  }
  gov8_snap_impl::FreeBlobHolder(h);
  return kOk;
}

// --- snapshot consumption -------------------------------------------------------

// Recovers an added context (global proxy included) from the snapshot.
// Returns a fresh context wrapper, or null when the index has no context.
__declspec(dllexport) void* gov8_context_from_snapshot(v8::Isolate* iso,
                                                       int64_t index) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::HandleScope hs(iso);
    const v8::Context* p = v8__Context__FromSnapshot(
        iso, static_cast<size_t>(index), nullptr, nullptr);
    if (p == nullptr) {
      ClearErr();
      return nullptr;
    }
    v8::Local<v8::Context> ctx;
    std::memcpy(&ctx, &p, sizeof(void*));
    CtxWrap* w = new CtxWrap{};
    w->magic = kCtxMagic;
    w->iso = iso;
    w->ctx = new v8::Global<v8::Context>(iso, ctx);
    return w;
  } catch (...) {
    SetErr("C++ exception in context_from_snapshot");
    return nullptr;
  }
}

// Exactly-once isolate-snapshot data retrieval. Status: 0 = Ok (wire in
// *out), 1 = NoData, 2 = BadType. The wire is created in the CALLER's open
// HandleScope (Go-owned scope).
__declspec(dllexport) int64_t gov8_isolate_data_from_snapshot_once(
    v8::Isolate* iso, void* scope, int64_t index, int32_t want, void** out) {
  ClearErr();
  if (out == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    const v8::Data* data = v8__Isolate__GetDataFromSnapshotOnce(
        iso, static_cast<size_t>(index));
    if (data == nullptr) {
      return 1;  // NoData
    }
    if (!gov8_snap_impl::DataMatches(want, const_cast<void*>(
                                            static_cast<const void*>(data)))) {
      return 2;  // BadType
    }
    *out = const_cast<void*>(static_cast<const void*>(data));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in isolate_data_from_snapshot_once");
    return kErrCpp;
  }
}

// Exactly-once context-snapshot data retrieval for the given context.
// Status: 0 = Ok, 1 = NoData, 2 = BadType.
__declspec(dllexport) int64_t gov8_context_data_from_snapshot_once(
    v8::Isolate* iso, void* ctxw, void* scope, int64_t index, int32_t want,
    void** out) {
  ClearErr();
  CtxWrap* c = AsCtx(ctxw);
  if (out == nullptr || c == nullptr || !ScopeIs(iso, scope) ||
      !OwnedBy(iso, c->iso)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8::Local<v8::Context> ctx = c->ctx->Get(iso);
    const v8::Data* data = v8__Context__GetDataFromSnapshotOnce(
        DerefLocal(ctx), static_cast<size_t>(index));
    if (data == nullptr) {
      return 1;  // NoData
    }
    if (!gov8_snap_impl::DataMatches(want, const_cast<void*>(
                                            static_cast<const void*>(data)))) {
      return 2;  // BadType
    }
    *out = const_cast<void*>(static_cast<const void*>(data));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in context_data_from_snapshot_once");
    return kErrCpp;
  }
}

// --- strong Global handles ------------------------------------------------------

// Creates a new strong cell holding the same object as the wire (a scope
// local or another cell). The wire lives in the CALLER's open HandleScope.
__declspec(dllexport) void* gov8_global_new(v8::Isolate* iso, void* scope,
                                            void* value_wire) {
  ClearErr();
  if (value_wire == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    return const_cast<void*>(static_cast<const void*>(v8__Global__New(
        iso, DerefLocal(FromWire<v8::Data>(value_wire)))));
  } catch (...) {
    SetErr("C++ exception in global_new");
    return nullptr;
  }
}

// Clones the cell another cell points at (Global::clone). Requires no handle
// scope; the engine call enters the isolate.
__declspec(dllexport) void* gov8_global_clone(v8::Isolate* iso,
                                              void* cell) {
  ClearErr();
  if (cell == nullptr) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    return const_cast<void*>(static_cast<const void*>(
        v8__Global__New(iso, *static_cast<const v8::Data*>(cell))));
  } catch (...) {
    SetErr("C++ exception in global_clone");
    return nullptr;
  }
}

// Reopens a cell as a local handle in the CALLER's open HandleScope.
__declspec(dllexport) int64_t gov8_global_to_local(v8::Isolate* iso,
                                                   void* scope, void* cell,
                                                   void** out) {
  ClearErr();
  if (out == nullptr || cell == nullptr || !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    *out = const_cast<void*>(static_cast<const void*>(v8__Local__New(
        iso, *static_cast<const v8::Data*>(cell))));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in global_to_local");
    return kErrCpp;
  }
}

// Identity comparison of the objects held by two cells (v8__Data__EQ). Pure
// pointer reads: no isolate scope is entered, so cross-isolate callers must
// gate themselves (the Go wrapper early-returns false for distinct isolates,
// exactly like the pinned crate's PartialEq).
__declspec(dllexport) int64_t gov8_data_eq(void* a, void* b) {
  ClearErr();
  if (a == nullptr || b == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    return v8__Data__EQ(static_cast<const v8::Data*>(a),
                        static_cast<const v8::Data*>(b))
               ? 1
               : 0;
  } catch (...) {
    SetErr("C++ exception in data_eq");
    return kErrCpp;
  }
}

// Releases a strong cell (Global::Reset / Drop). The cell must not be used
// afterwards.
__declspec(dllexport) int64_t gov8_global_reset(v8::Isolate* iso,
                                                void* cell) {
  ClearErr();
  if (cell == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    v8__Global__Reset(static_cast<const v8::Data*>(cell));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in global_reset");
    return kErrCpp;
  }
}

// --- weak handles --------------------------------------------------------------

// Creates a weak cell over the object held by `cell` (a strong cell or
// scope-local wire) and registers the Go finalizer id. No HandleScope
// required (mirrors Weak::new taking &mut Isolate).
__declspec(dllexport) void* gov8_weak_new(v8::Isolate* iso, void* cell,
                                          int64_t go_id) {
  ClearErr();
  if (cell == nullptr || go_id < 0) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    WeakWrap* w = new WeakWrap{};
    w->magic = kWeakMagic;
    w->iso = iso;
    w->go_id = go_id;
    w->cell = v8__Global__NewWeak(
        iso, *static_cast<const v8::Data*>(cell), w,
        gov8_snap_impl::WeakFirstPass);
    return w;
  } catch (...) {
    SetErr("C++ exception in weak_new");
    return nullptr;
  }
}

// Cell emptiness: true once the first-pass weak callback ran (the engine
// cleared the handle). Pure Go-visible flag read; no engine state touched.
__declspec(dllexport) int64_t gov8_weak_is_empty(v8::Isolate* iso,
                                                 void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return kErrMagic;
  }
  return w->cell == nullptr ? 1 : 0;
}

// Creates a strong cell over the weak's object (Weak::to_global). Returns
// null when the object was already collected.
__declspec(dllexport) void* gov8_weak_to_global(v8::Isolate* iso,
                                                void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return nullptr;
  }
  if (w->cell == nullptr) {
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    return const_cast<void*>(static_cast<const void*>(
        v8__Global__New(iso, *static_cast<const v8::Data*>(w->cell))));
  } catch (...) {
    SetErr("C++ exception in weak_to_global");
    return nullptr;
  }
}

// Reopens the weak's object as a local in the CALLER's open HandleScope.
// Status: 0 = Ok, 1 = collected (no cell).
__declspec(dllexport) int64_t gov8_weak_to_local(v8::Isolate* iso,
                                                 void* scope, void* weakw,
                                                 void** out) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (out == nullptr || w == nullptr || !OwnedBy(iso, w->iso) ||
      !ScopeIs(iso, scope)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  if (w->cell == nullptr) {
    return 1;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    *out = const_cast<void*>(static_cast<const void*>(
        v8__Local__New(iso, *static_cast<const v8::Data*>(w->cell))));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in weak_to_local");
    return kErrCpp;
  }
}

// Clones the weak cell with no finalizer (Weak::clone). Returns null when
// the object was already collected (clone of an empty weak is empty).
__declspec(dllexport) void* gov8_weak_clone(v8::Isolate* iso, void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return nullptr;
  }
  if (w->cell == nullptr) {
    return nullptr;
  }
  try {
    Gov8IsolateScope iso_scope(iso);
    WeakWrap* c = new WeakWrap{};
    c->magic = kWeakMagic;
    c->iso = iso;
    c->go_id = 0;
    c->cell = v8__Global__NewWeak(
        iso, *static_cast<const v8::Data*>(w->cell), c,
        gov8_snap_impl::WeakFirstPass);
    return c;
  } catch (...) {
    SetErr("C++ exception in weak_clone");
    return nullptr;
  }
}

// The weak's underlying cell for identity comparisons; null when collected.
__declspec(dllexport) void* gov8_weak_cell(v8::Isolate* iso, void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return nullptr;
  }
  return const_cast<void*>(static_cast<const void*>(w->cell));
}

// Drops the weak handle (Weak::Drop): resets a still-strong cell -- which
// cancels a pending first pass -- and reports whether a live Go finalizer
// registration must be removed (1) or none was pending (0). The wrap is
// freed here; Go must not use the Weak afterwards.
__declspec(dllexport) int64_t gov8_weak_close(v8::Isolate* iso,
                                              void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return kErrMagic;
  }
  int64_t had = 0;
  try {
    if (w->cell != nullptr) {
      Gov8IsolateScope iso_scope(iso);
      v8__Global__Reset(w->cell);  // cancels the weak callback
      w->cell = nullptr;
      had = 1;
    } else if (w->go_id != 0) {
      // First pass ran, second pass pending: cancelling here means the
      // callback never fires (mirrors Weak::Drop's remove_finalizer).
      had = 1;
    }
    w->go_id = 0;
    w->magic = 0;
    delete w;
    return had;
  } catch (...) {
    SetErr("C++ exception in weak_close");
    return kErrCpp;
  }
}

// Drains one guaranteed finalizer: resets a still-live cell (cancelling the
// weak callback), zeroes the Go id so a later second pass never dispatches,
// and frees the wrap. Part of DrainGuaranteedWeakFinalizers (the Go analog
// of the pinned crate draining its guaranteed-finalizer map at isolate
// drop).
__declspec(dllexport) int64_t gov8_weak_drain_guaranteed(v8::Isolate* iso,
                                                         void* weakw) {
  ClearErr();
  WeakWrap* w = AsWeak(weakw);
  if (w == nullptr || !OwnedBy(iso, w->iso)) {
    SetErr("invalid weak handle");
    return kErrMagic;
  }
  try {
    if (w->cell != nullptr) {
      Gov8IsolateScope iso_scope(iso);
      v8__Global__Reset(w->cell);
      w->cell = nullptr;
    }
    w->go_id = 0;
    w->first_pass_done = true;
    w->magic = 0;
    delete w;
    return kOk;
  } catch (...) {
    SetErr("C++ exception in weak_drain_guaranteed");
    return kErrCpp;
  }
}

// --- termination and memory notifications --------------------------------------

__declspec(dllexport) int64_t gov8_isolate_terminate_execution(
    v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    v8__Isolate__TerminateExecution(iso);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in isolate_terminate_execution");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_isolate_cancel_terminate_execution(
    v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    v8__Isolate__CancelTerminateExecution(iso);
    return kOk;
  } catch (...) {
    SetErr("C++ exception in isolate_cancel_terminate_execution");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_isolate_is_execution_terminating(
    v8::Isolate* iso) {
  ClearErr();
  if (iso == nullptr) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  try {
    return v8__Isolate__IsExecutionTerminating(iso) ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception in isolate_is_execution_terminating");
    return kErrCpp;
  }
}

// gov8_isolate_low_memory_notification lives in buffers_serialization.inc
// (the buffers slice landed it first; same forced-GC entry point backs the
// weak-finalizer observations of this slice).

__declspec(dllexport) int64_t gov8_tc_has_terminated(void* tcw) {
  ClearErr();
  TcWrap* w = AsTc(tcw);
  if (w == nullptr) {
    SetErr("invalid trycatch handle");
    return kErrMagic;
  }
  try {
    return w->tc()->HasTerminated() ? 1 : 0;
  } catch (...) {
    SetErr("C++ exception in tc_has_terminated");
    return kErrCpp;
  }
}

}  // extern "C"
