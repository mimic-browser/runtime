// Unified snapshot + safe CreateParams constructor for pinned rusty_v8
// 152.2.0. Include after snapshots_handles.inc, external_references.inc and
// isolate_advanced.inc so their holder/table/counter ownership helpers are
// available in this translation unit.

#include <cstdint>
#include <memory>
#include <mutex>

extern "C" {

__declspec(dllexport) int64_t gov8_cps_isolate_new(
    const char* blob_data, int64_t blob_len, uint64_t max_old,
    uint64_t max_young, uint64_t code_range, uint64_t initial_old,
    uint64_t initial_young, int32_t allow_atomics_wait,
    int32_t use_external_references, const intptr_t* external_references,
    size_t external_reference_count, void* counter_host_context,
    void* cpp_heap_wire, int32_t* cpp_heap_consumed,
    void** cpp_heap_identity, void* array_buffer_allocator_wire,
    void** out_blob, v8::Isolate** out_isolate) {
  ClearErr();
  if (cpp_heap_consumed != nullptr) *cpp_heap_consumed = 0;
  if (cpp_heap_identity != nullptr) *cpp_heap_identity = nullptr;
  if (out_blob != nullptr) *out_blob = nullptr;
  if (out_isolate != nullptr) *out_isolate = nullptr;
  if (g_platform == nullptr || blob_data == nullptr || blob_len <= 0 ||
      out_blob == nullptr || out_isolate == nullptr ||
      (allow_atomics_wait != 0 && allow_atomics_wait != 1) ||
      (use_external_references != 0 && use_external_references != 1) ||
      (use_external_references == 0 &&
       (external_references != nullptr || external_reference_count != 0)) ||
      (use_external_references != 0 && external_references == nullptr &&
       external_reference_count != 0) || cpp_heap_consumed == nullptr ||
      cpp_heap_identity == nullptr) {
    SetErr("invalid snapshot CreateParams argument");
    return kErrBadArg;
  }

  gov8_snap_impl::BlobHolder* holder = nullptr;
  v8::Isolate* isolate = nullptr;
  bool external_references_adopted = false;
  bool counter_state_adopted = false;
  std::unique_ptr<void, void (*)(void*)> external_reference_state(
      nullptr, gov8_external_references_impl::DestroyTable);
  std::unique_ptr<gov8_ia_impl::CounterState> counter_state;
  try {
    holder = gov8_snap_impl::HoldSharedBlob(blob_data, blob_len);
    v8::Isolate::CreateParams params;
    gov8_snap_impl::FillCreateParams(&params, holder);
    if (array_buffer_allocator_wire != nullptr &&
        !gov8_array_buffer_allocator_impl::FillCreateParams(
            array_buffer_allocator_wire, &params)) {
      gov8_snap_impl::FreeBlobHolder(holder);
      SetErr("invalid ArrayBuffer allocator");
      return kErrBadArg;
    }
    params.constraints.set_max_old_generation_size_in_bytes(
        static_cast<size_t>(max_old));
    params.constraints.set_max_young_generation_size_in_bytes(
        static_cast<size_t>(max_young));
    params.constraints.set_code_range_size_in_bytes(
        static_cast<size_t>(code_range));
    params.constraints.set_initial_old_generation_size_in_bytes(
        static_cast<size_t>(initial_old));
    params.constraints.set_initial_young_generation_size_in_bytes(
        static_cast<size_t>(initial_young));
    params.allow_atomics_wait = allow_atomics_wait != 0;

    if (use_external_references != 0) {
      external_reference_state.reset(
          gov8_external_references_impl::CopyTable(
              external_references, external_reference_count));
      if (!external_reference_state) {
        gov8_snap_impl::FreeBlobHolder(holder);
        SetErr("external-reference table allocation failed");
        return kErrNoMemory;
      }
      params.external_references = gov8_external_references_impl::TableWords(
          external_reference_state.get());
    }

    if (counter_host_context != nullptr) {
      counter_state = std::make_unique<gov8_ia_impl::CounterState>();
      counter_state->host_context = counter_host_context;
      params.counter_lookup_callback = gov8_ia_impl::CounterLookup;
      gov8_ia_impl::pending_counter_state = counter_state.get();
    }

    // Take the heap only after every avoidable allocation/validation above.
    // Once consumed, the outputs stay set even if Isolate::New fails so Go can
    // settle the original heap wrapper and its nested OS-thread lock exactly.
    if (cpp_heap_wire != nullptr) {
      v8::CppHeap* cpp_heap = nullptr;
      void* identity = nullptr;
      if (!gov8_cppgc_heap_impl::TakeForParams(cpp_heap_wire, &cpp_heap,
                                                &identity)) {
        gov8_ia_impl::pending_counter_state = nullptr;
        gov8_snap_impl::FreeBlobHolder(holder);
        SetErr("custom cppgc heap is not transferable");
        return kErrState;
      }
      params.cpp_heap = cpp_heap;
      *cpp_heap_identity = identity;
      *cpp_heap_consumed = 1;
    }

    isolate = v8::Isolate::New(params);
    gov8_ia_impl::pending_counter_state = nullptr;
    if (isolate == nullptr) {
      gov8_snap_impl::FreeBlobHolder(holder);
      SetErr("Isolate::New failed");
      return kErrState;
    }
    if (external_reference_state) {
      gov8_external_references_impl::AdoptTable(
          isolate, external_reference_state.release());
      external_references_adopted = true;
    }
    if (counter_state) {
      std::lock_guard<std::mutex> lock(gov8_ia_impl::counter_mutex);
      gov8_ia_impl::counter_states.emplace(isolate, std::move(counter_state));
      counter_state_adopted = true;
    }
    isolate->Enter();
    *out_blob = holder;
    *out_isolate = isolate;
    return kOk;
  } catch (...) {
    gov8_ia_impl::pending_counter_state = nullptr;
    if (counter_state_adopted) {
      std::lock_guard<std::mutex> lock(gov8_ia_impl::counter_mutex);
      gov8_ia_impl::counter_states.erase(isolate);
    }
    if (external_references_adopted) {
      gov8_external_references_impl::DropTable(isolate);
    }
    if (isolate != nullptr) isolate->Dispose();
    gov8_snap_impl::FreeBlobHolder(holder);
    SetErr("C++ exception in snapshot CreateParams isolate constructor");
    return kErrCpp;
  }
}

}  // extern "C"
