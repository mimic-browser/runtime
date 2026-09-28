// external_references.inc -- ExternalReference words and isolate-lifetime
// native table ownership for rusty_v8 152.2.0 parity.

#include <cstddef>
#include <cstdint>
#include <cstring>
#include <memory>
#include <mutex>
#include <new>

namespace gov8_external_references_impl {

struct TableState {
  v8::Isolate* isolate = nullptr;
  std::unique_ptr<intptr_t[]> words;
  size_t length = 0;
  TableState* next = nullptr;
};

std::mutex table_mutex;
TableState* table_head = nullptr;

// Stateless native callback used to exercise and expose snapshot-portable
// FunctionCallback external references. Unlike gov8's ordinary Go callback
// trampoline it carries no Go registry state: its External data payload is
// remapped entirely by V8's external-reference table.
void ExternalPointerFunctionTrampoline(
    const v8::FunctionCallbackInfo<v8::Value>& info) {
  if (info.Data().IsEmpty() || !info.Data()->IsExternal()) return;
  const uintptr_t value = reinterpret_cast<uintptr_t>(
      info.Data().As<v8::External>()->Value(
          v8::kExternalPointerTypeTagDefault));
  info.GetReturnValue().Set(v8::BigInt::NewFromUnsigned(info.GetIsolate(), value));
}

void* CopyTable(const intptr_t* words, size_t length) {
  if (words == nullptr && length != 0) return nullptr;
  if (length == SIZE_MAX || length > SIZE_MAX / sizeof(intptr_t)) return nullptr;
  const bool terminated = length != 0 && words[length - 1] == 0;
  const size_t native_length = length + (terminated ? 0 : 1);
  std::unique_ptr<TableState> state(new (std::nothrow) TableState());
  if (!state) return nullptr;
  state->words.reset(new (std::nothrow) intptr_t[native_length]);
  if (!state->words) return nullptr;
  if (length != 0) {
    std::memcpy(state->words.get(), words, length * sizeof(intptr_t));
  }
  if (!terminated) state->words[length] = 0;
  state->length = native_length;
  return state.release();
}

const intptr_t* TableWords(void* opaque) {
  TableState* state = static_cast<TableState*>(opaque);
  return state == nullptr ? nullptr : state->words.get();
}

void AdoptTable(v8::Isolate* isolate, void* opaque) noexcept {
  TableState* state = static_cast<TableState*>(opaque);
  if (state == nullptr) return;
  state->isolate = isolate;
  std::lock_guard<std::mutex> lock(table_mutex);
  state->next = table_head;
  table_head = state;
}

void DestroyTable(void* opaque) noexcept {
  delete static_cast<TableState*>(opaque);
}

void DropTable(v8::Isolate* isolate) noexcept {
  TableState* removed = nullptr;
  {
    std::lock_guard<std::mutex> lock(table_mutex);
    TableState** current = &table_head;
    while (*current != nullptr) {
      if ((*current)->isolate == isolate) {
        removed = *current;
        *current = removed->next;
        break;
      }
      current = &(*current)->next;
    }
  }
  delete removed;
}

intptr_t CallbackAddress(int32_t kind) {
  switch (kind) {
    case 0:
      return reinterpret_cast<intptr_t>(&ExternalPointerFunctionTrampoline);
    case 1:
      return reinterpret_cast<intptr_t>(&HostNamedGetTrampoline);
    case 2:
      return reinterpret_cast<intptr_t>(&HostNamedSetTrampoline);
    case 3:
      return reinterpret_cast<intptr_t>(&HostNamedDefineTrampoline);
    case 4:
      return reinterpret_cast<intptr_t>(&HostNamedDeleteTrampoline);
    case 5:
      return reinterpret_cast<intptr_t>(&HostNamedQueryTrampoline);
    case 6:
      return reinterpret_cast<intptr_t>(&HostIndexedGetTrampoline);
    case 7:
      return reinterpret_cast<intptr_t>(&HostIndexedSetTrampoline);
    case 8:
      return reinterpret_cast<intptr_t>(&HostIndexedDefineTrampoline);
    case 9:
      return reinterpret_cast<intptr_t>(&HostIndexedDeleteTrampoline);
    case 10:
      return reinterpret_cast<intptr_t>(&HostIndexedQueryTrampoline);
    case 11:
      return reinterpret_cast<intptr_t>(&HostNamedEnumTrampoline);
    case 12:
      return reinterpret_cast<intptr_t>(&HostIndexedEnumTrampoline);
    case 13:
      return reinterpret_cast<intptr_t>(
          &gov8_ch_impl::ChMessageListenerTrampoline);
    default:
      return 0;
  }
}

}  // namespace gov8_external_references_impl

extern "C" {

__declspec(dllexport) int64_t gov8_snapshot_creator_new_external_references(
    const char* blob_data, int64_t blob_len, const intptr_t* references,
    size_t reference_count, void** out_wrap, v8::Isolate** out_iso) {
  ClearErr();
  if (out_wrap == nullptr || out_iso == nullptr ||
      (blob_data == nullptr && blob_len > 0) ||
      (references == nullptr && reference_count != 0)) {
    SetErr("invalid external-reference snapshot creator argument");
    return kErrBadArg;
  }
  if (g_platform == nullptr) {
    SetErr("v8 not initialized");
    return kErrState;
  }
  gov8_snap_impl::CreatorWrap* wrapper = nullptr;
  bool constructed = false;
  std::unique_ptr<void, void (*)(void*)> table(
      gov8_external_references_impl::CopyTable(references, reference_count),
      gov8_external_references_impl::DestroyTable);
  if (!table) {
    SetErr("external-reference table allocation failed");
    return kErrNoMemory;
  }
  try {
    wrapper = new gov8_snap_impl::CreatorWrap{};
    wrapper->magic = gov8_snap_impl::kCreatorMagic;
    wrapper->blob = nullptr;
    if (blob_len >= gov8_snap_impl::kMinValidRawSize) {
      wrapper->blob = gov8_snap_impl::HoldBlob(blob_data, blob_len);
    }
    v8::Isolate::CreateParams params;
    gov8_snap_impl::FillCreateParams(&params, wrapper->blob);
    params.external_references =
        gov8_external_references_impl::TableWords(table.get());
    v8__SnapshotCreator__CONSTRUCT(wrapper->sc_storage, params);
    constructed = true;
    wrapper->iso = v8__SnapshotCreator__GetIsolate(wrapper->sc_storage);
    if (wrapper->iso == nullptr) {
      SetErr("SnapshotCreator::GetIsolate failed");
      v8__SnapshotCreator__DESTRUCT(wrapper->sc_storage);
      gov8_snap_impl::FreeBlobHolder(wrapper->blob);
      delete wrapper;
      return kErrState;
    }
    gov8_external_references_impl::AdoptTable(wrapper->iso, table.release());
    *out_wrap = wrapper;
    *out_iso = wrapper->iso;
    return kOk;
  } catch (...) {
    if (wrapper != nullptr) {
      if (constructed) v8__SnapshotCreator__DESTRUCT(wrapper->sc_storage);
      gov8_snap_impl::FreeBlobHolder(wrapper->blob);
      delete wrapper;
    }
    SetErr("C++ exception in external-reference snapshot creator");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_isolate_new_snapshot_external_references(
    const char* blob_data, int64_t blob_len, const intptr_t* references,
    size_t reference_count, void** out_blob, v8::Isolate** out_iso) {
  ClearErr();
  if (out_blob == nullptr || out_iso == nullptr ||
      (blob_data == nullptr && blob_len > 0) ||
      (references == nullptr && reference_count != 0)) {
    SetErr("invalid external-reference snapshot isolate argument");
    return kErrBadArg;
  }
  if (g_platform == nullptr) {
    SetErr("v8 not initialized");
    return kErrState;
  }
  gov8_snap_impl::BlobHolder* holder = nullptr;
  std::unique_ptr<void, void (*)(void*)> table(
      gov8_external_references_impl::CopyTable(references, reference_count),
      gov8_external_references_impl::DestroyTable);
  if (!table) {
    SetErr("external-reference table allocation failed");
    return kErrNoMemory;
  }
  try {
    holder = gov8_snap_impl::HoldSharedBlob(blob_data, blob_len);
    v8::Isolate::CreateParams params;
    gov8_snap_impl::FillCreateParams(&params, holder);
    params.external_references =
        gov8_external_references_impl::TableWords(table.get());
    v8::Isolate* isolate = v8::Isolate::New(params);
    if (isolate == nullptr) {
      gov8_snap_impl::FreeBlobHolder(holder);
      SetErr("Isolate::New failed");
      return kErrState;
    }
    gov8_external_references_impl::AdoptTable(isolate, table.release());
    isolate->Enter();
    *out_blob = holder;
    *out_iso = isolate;
    return kOk;
  } catch (...) {
    gov8_snap_impl::FreeBlobHolder(holder);
    SetErr("C++ exception in external-reference snapshot isolate");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_external_reference_callback_address(
    int32_t kind, intptr_t* out) {
  ClearErr();
  if (out == nullptr || kind < 0 || kind > 13) {
    SetErr("invalid external-reference callback kind");
    return kErrBadArg;
  }
  *out = gov8_external_references_impl::CallbackAddress(kind);
  if (*out == 0) {
    SetErr("external-reference callback is unavailable");
    return kErrCpp;
  }
  return kOk;
}

__declspec(dllexport) void* gov8_external_reference_function_template_new(
    v8::Isolate* isolate, void* scope, intptr_t callback_address,
    void* data_wire) {
  ClearErr();
  if (isolate == nullptr || !ScopeIs(isolate, scope) || data_wire == nullptr ||
      callback_address != reinterpret_cast<intptr_t>(
                              &gov8_external_references_impl::
                                  ExternalPointerFunctionTrampoline)) {
    SetErr("invalid snapshot-portable external-reference function");
    return nullptr;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    v8::Local<v8::Value> data = FromWire<v8::Value>(data_wire);
    if (!data->IsExternal()) {
      SetErr("external-reference function data is not External");
      return nullptr;
    }
    v8::Local<v8::FunctionTemplate> function = v8::FunctionTemplate::New(
        isolate,
        gov8_external_references_impl::ExternalPointerFunctionTrampoline,
        data);
    return function.IsEmpty() ? nullptr : ToWire(function);
  } catch (...) {
    SetErr("C++ exception creating external-reference function template");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_external_reference_table_info(
    v8::Isolate* isolate, size_t* out_length, intptr_t* out_first,
    intptr_t* out_last) {
  ClearErr();
  if (isolate == nullptr || out_length == nullptr || out_first == nullptr ||
      out_last == nullptr) {
    SetErr("invalid external-reference table query");
    return kErrBadArg;
  }
  std::lock_guard<std::mutex> lock(
      gov8_external_references_impl::table_mutex);
  for (auto* state = gov8_external_references_impl::table_head;
       state != nullptr; state = state->next) {
    if (state->isolate == isolate) {
      *out_length = state->length;
      *out_first = state->words[0];
      *out_last = state->words[state->length - 1];
      return 1;
    }
  }
  *out_length = 0;
  *out_first = 0;
  *out_last = 0;
  return 0;
}

}  // extern "C"
