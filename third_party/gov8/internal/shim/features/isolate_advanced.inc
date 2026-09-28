// ===========================================================================
// Advanced isolate/CreateParams/statistics slice.
// ===========================================================================

#include <mutex>
#include <unordered_map>

namespace gov8_external_references_impl {
void* CopyTable(const intptr_t* words, size_t length);
const intptr_t* TableWords(void* opaque);
void AdoptTable(v8::Isolate* isolate, void* opaque) noexcept;
void DestroyTable(void* opaque) noexcept;
void DropTable(v8::Isolate* isolate) noexcept;
}  // namespace gov8_external_references_impl

namespace gov8_cppgc_heap_impl {
bool TakeForParams(void* wire, v8::CppHeap** heap, void** identity) noexcept;
}

namespace gov8_array_buffer_allocator_impl {
bool FillCreateParams(void* wire,
                      v8::Isolate::CreateParams* params) noexcept;
}

namespace gov8_ia_impl {

extern "C" {
void v8__CpuProfiler__CollectSample(v8::Isolate* isolate,
                                    const uint64_t* trace_id);
void v8__CpuProfiler__UseDetailedSourcePositionsForProfiling(
    v8::Isolate* isolate);
}

using GoCounterDispatcher = uintptr_t (*)(void*, const char*, int64_t);

struct CounterState {
  void* host_context;
  std::unordered_map<std::string, std::unique_ptr<int32_t>> counters;
};

std::mutex counter_mutex;
std::unordered_map<v8::Isolate*, std::unique_ptr<CounterState>> counter_states;
thread_local CounterState* pending_counter_state = nullptr;
GoCounterDispatcher counter_dispatcher = nullptr;

int32_t* CounterLookup(const char* name) {
  CounterState* state = nullptr;
  int32_t* result = nullptr;
  void* host_context = nullptr;
  v8::Isolate* isolate = v8::Isolate::TryGetCurrent();
  {
    std::lock_guard<std::mutex> lock(counter_mutex);
    if (isolate != nullptr) {
      auto found = counter_states.find(isolate);
      if (found != counter_states.end()) state = found->second.get();
    }
    if (state == nullptr) state = pending_counter_state;
    if (state == nullptr || name == nullptr) return nullptr;
    auto& value = state->counters[std::string(name)];
    if (!value) value = std::make_unique<int32_t>(0);
    result = value.get();
    host_context = state->host_context;
  }
  if (counter_dispatcher != nullptr) {
    counter_dispatcher(host_context, name,
                       static_cast<int64_t>(std::strlen(name)));
  }
  return result;
}

void FillConstraints(const v8::ResourceConstraints& constraints,
                     uint64_t* out) {
  out[0] = constraints.max_old_generation_size_in_bytes();
  out[1] = constraints.max_young_generation_size_in_bytes();
  out[2] = constraints.code_range_size_in_bytes();
  out[3] = constraints.initial_old_generation_size_in_bytes();
  out[4] = constraints.initial_young_generation_size_in_bytes();
}

}  // namespace gov8_ia_impl

extern "C" {

using namespace gov8_ia_impl;

__declspec(dllexport) int64_t gov8_ia_set_counter_dispatcher(void* dispatcher) {
  ClearErr();
  if (dispatcher == nullptr) {
    SetErr("null counter dispatcher");
    return kErrBadArg;
  }
  counter_dispatcher = reinterpret_cast<GoCounterDispatcher>(dispatcher);
  return kOk;
}

__declspec(dllexport) int64_t gov8_ia_derive_constraints(
    int32_t mode, uint64_t first, uint64_t second, uint64_t* out) {
  ClearErr();
  if ((mode != 0 && mode != 1) || out == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    v8::ResourceConstraints constraints;
    if (mode == 0) {
      constraints.ConfigureDefaultsFromHeapSize(static_cast<size_t>(first),
                                                static_cast<size_t>(second));
    } else {
      constraints.ConfigureDefaults(first, second);
    }
    FillConstraints(constraints, out);
    return kOk;
  } catch (...) {
    SetErr("C++ exception deriving resource constraints");
    return kErrCpp;
  }
}

__declspec(dllexport) void* gov8_ia_isolate_new(
    uint64_t max_old, uint64_t max_young, uint64_t code_range,
    uint64_t initial_old, uint64_t initial_young, int32_t allow_atomics_wait,
    int32_t use_external_references, const intptr_t* external_references,
    size_t external_reference_count, void* counter_host_context,
    void* cpp_heap_wire, int32_t* cpp_heap_consumed, void** cpp_heap_identity,
    void* array_buffer_allocator_wire) {
  ClearErr();
  if (cpp_heap_consumed != nullptr) *cpp_heap_consumed = 0;
  if (cpp_heap_identity != nullptr) *cpp_heap_identity = nullptr;
  if (g_platform == nullptr || (allow_atomics_wait != 0 && allow_atomics_wait != 1) ||
      (use_external_references != 0 && use_external_references != 1) ||
      (use_external_references == 0 &&
       (external_references != nullptr || external_reference_count != 0)) ||
      (use_external_references != 0 && external_references == nullptr &&
       external_reference_count != 0) || cpp_heap_consumed == nullptr ||
      cpp_heap_identity == nullptr) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    v8::Isolate::CreateParams params;
    if (array_buffer_allocator_wire != nullptr) {
      if (!gov8_array_buffer_allocator_impl::FillCreateParams(
              array_buffer_allocator_wire, &params)) {
        SetErr("invalid ArrayBuffer allocator");
        return nullptr;
      }
    } else {
      params.array_buffer_allocator = g_allocator.get();
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
    std::unique_ptr<void, void (*)(void*)> external_reference_state(
        nullptr, gov8_external_references_impl::DestroyTable);
    if (use_external_references != 0) {
      external_reference_state.reset(gov8_external_references_impl::CopyTable(
          external_references, external_reference_count));
      if (!external_reference_state) {
        SetErr("external-reference table allocation failed");
        return nullptr;
      }
      params.external_references =
          gov8_external_references_impl::TableWords(
              external_reference_state.get());
    }

    std::unique_ptr<CounterState> counter_state;
    if (counter_host_context != nullptr) {
      counter_state = std::make_unique<CounterState>();
      counter_state->host_context = counter_host_context;
      params.counter_lookup_callback = CounterLookup;
      pending_counter_state = counter_state.get();
    }
    if (cpp_heap_wire != nullptr) {
      v8::CppHeap* cpp_heap = nullptr;
      void* identity = nullptr;
      if (!gov8_cppgc_heap_impl::TakeForParams(cpp_heap_wire, &cpp_heap,
                                                &identity)) {
        pending_counter_state = nullptr;
        SetErr("custom cppgc heap is not transferable");
        return nullptr;
      }
      params.cpp_heap = cpp_heap;
      *cpp_heap_identity = identity;
      *cpp_heap_consumed = 1;
    }
    v8::Isolate* isolate = Gov8NewIndependentIsolate(params);
    pending_counter_state = nullptr;
    if (isolate == nullptr) {
      SetErr("Isolate::New failed");
      return nullptr;
    }
    if (external_reference_state) {
      gov8_external_references_impl::AdoptTable(
          isolate, external_reference_state.release());
    }
    if (counter_state) {
      std::lock_guard<std::mutex> lock(counter_mutex);
      counter_states.emplace(isolate, std::move(counter_state));
    }
    isolate->Enter();
    return isolate;
  } catch (...) {
    pending_counter_state = nullptr;
    SetErr("C++ exception in advanced isolate new");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_ia_after_isolate_dispose(v8::Isolate* isolate) {
  ClearErr();
  std::lock_guard<std::mutex> lock(counter_mutex);
  counter_states.erase(isolate);
  gov8_external_references_impl::DropTable(isolate);
  return kOk;
}

__declspec(dllexport) int64_t gov8_ia_counter_value(
    v8::Isolate* isolate, const char* name, int64_t name_length,
    int32_t* value, int32_t* found) {
  ClearErr();
  if (isolate == nullptr || (name == nullptr && name_length != 0) || name_length < 0 ||
      value == nullptr || found == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  std::lock_guard<std::mutex> lock(counter_mutex);
  auto state = counter_states.find(isolate);
  if (state == counter_states.end()) {
    *value = 0;
    *found = 0;
    return kOk;
  }
  const std::string key = name == nullptr
                              ? std::string()
                              : std::string(name, static_cast<size_t>(name_length));
  auto counter = state->second->counters.find(key);
  if (counter == state->second->counters.end()) {
    *value = 0;
    *found = 0;
    return kOk;
  }
  *value = *counter->second;
  *found = 1;
  return kOk;
}

__declspec(dllexport) int64_t gov8_ia_heap_space_statistics(
    v8::Isolate* isolate, uint64_t index, char* name, int64_t name_capacity,
    int64_t* name_length, uint64_t* values, int32_t* found) {
  ClearErr();
  if (isolate == nullptr || name == nullptr || name_length == nullptr ||
      values == nullptr || found == nullptr || name_capacity < 0) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    v8::HeapSpaceStatistics statistics;
    if (!isolate->GetHeapSpaceStatistics(&statistics,
                                         static_cast<size_t>(index))) {
      *found = 0;
      *name_length = 0;
      return kOk;
    }
    const char* space_name = statistics.space_name();
    const size_t length = std::strlen(space_name);
    *name_length = static_cast<int64_t>(length);
    if (static_cast<int64_t>(length) > name_capacity) {
      SetErr("heap space name buffer too small");
      return kErrNoMemory;
    }
    std::memcpy(name, space_name, length);
    values[0] = statistics.space_size();
    values[1] = statistics.space_used_size();
    values[2] = statistics.space_available_size();
    values[3] = statistics.physical_space_size();
    *found = 1;
    return kOk;
  } catch (...) {
    SetErr("C++ exception reading heap space statistics");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ia_heap_code_statistics(
    v8::Isolate* isolate, uint64_t* values, int32_t* available) {
  ClearErr();
  if (isolate == nullptr || values == nullptr || available == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    v8::HeapCodeStatistics statistics;
    if (!isolate->GetHeapCodeAndMetadataStatistics(&statistics)) {
      *available = 0;
      return kOk;
    }
    values[0] = statistics.code_and_metadata_size();
    values[1] = statistics.bytecode_and_metadata_size();
    values[2] = statistics.external_script_source_size();
    values[3] = statistics.cpu_profiler_metadata_size();
    *available = 1;
    return kOk;
  } catch (...) {
    SetErr("C++ exception reading heap code statistics");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_ia_has_cpp_heap(v8::Isolate* isolate,
                                                   int32_t* present) {
  ClearErr();
  if (isolate == nullptr || present == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  *present = isolate->GetCppHeap() != nullptr ? 1 : 0;
  return kOk;
}

__declspec(dllexport) int64_t gov8_ia_profiler_controls(
    v8::Isolate* isolate, int32_t operation, uint64_t trace_id) {
  ClearErr();
  if (isolate == nullptr || operation < 0 || operation > 2) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    if (operation == 0) {
      v8__CpuProfiler__UseDetailedSourcePositionsForProfiling(isolate);
    } else if (operation == 2) {
      v8__CpuProfiler__CollectSample(isolate, &trace_id);
    } else {
      v8__CpuProfiler__CollectSample(isolate, nullptr);
    }
    return kOk;
  } catch (...) {
    SetErr("C++ exception in profiler controls");
    return kErrCpp;
  }
}

}  // extern "C"
