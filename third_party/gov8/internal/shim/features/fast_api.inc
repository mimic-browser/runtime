// ===========================================================================
// fast_api.inc -- safe native descriptor ownership and fast FunctionTemplate
// construction for pinned V8 15.2.
//
// Include after templates_callbacks.inc: this feature reuses
// RegisterNativeCallback and HostFunctionTrampoline for the ordinary slow
// fallback. V8 retains the std::span<CFunction> storage passed to
// NewWithCFunctionOverloads, so DescriptorSet owns the CFunction array, every
// CFunctionInfo and every argument CTypeInfo array until explicit cleanup
// after isolate disposal. The cleanup export treats the old isolate address as
// an opaque map key and never dereferences it. No Go pointer is retained.
// ===========================================================================

#include "v8-fast-api-calls.h"
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <memory>
#include <mutex>
#include <unordered_map>
#include <vector>

// Call the flat binding compiled into the pinned rusty_v8 artifact. The
// artifact was built while this API used v8::MemorySpan; the extracted V8
// headers now spell the parameter std::span, whose MSVC mangled name is not
// exported by that library. This C binding is the same route build_fast uses
// and keeps the shim independent of that C++ signature drift.
extern "C" const v8::FunctionTemplate* v8__FunctionTemplate__New(
    v8::Isolate* isolate, v8::FunctionCallback callback,
    const v8::Value* data_or_null,
    const v8::Signature* signature_or_null, int length,
    v8::ConstructorBehavior constructor_behavior,
    v8::SideEffectType side_effect_type, const v8::CFunction* c_functions,
    size_t c_functions_len);

namespace gov8_fast_api_impl {

struct FastTypeSpec {
  uint8_t type;
  uint8_t flags;
};

struct FastFunctionSpec {
  uintptr_t address;
  uint32_t arg_offset;
  uint32_t arg_count;
  uint8_t return_type;
  uint8_t return_flags;
  uint8_t repr;
  uint8_t reserved;
};

static_assert(sizeof(FastTypeSpec) == 2, "FastTypeSpec ABI");
static_assert(offsetof(FastFunctionSpec, address) == 0,
              "FastFunctionSpec address ABI");
static_assert(offsetof(FastFunctionSpec, arg_offset) == sizeof(uintptr_t),
              "FastFunctionSpec arg offset ABI");
static_assert(offsetof(FastFunctionSpec, return_type) ==
                  sizeof(uintptr_t) + sizeof(uint32_t) * 2,
              "FastFunctionSpec return type ABI");
static_assert(sizeof(FastFunctionSpec) == 24,
              "FastFunctionSpec win/amd64 Go slice stride ABI");

struct DescriptorSet {
  std::vector<std::unique_ptr<std::vector<v8::CTypeInfo>>> argument_storage;
  std::vector<std::unique_ptr<v8::CFunctionInfo>> type_infos;
  std::vector<v8::CFunction> functions;
};

std::mutex g_descriptor_mutex;
std::unordered_map<v8::Isolate*,
                   std::vector<std::unique_ptr<DescriptorSet>>>
    g_descriptor_sets;

std::atomic<size_t> g_test_add_calls{0};
std::atomic<size_t> g_test_one_calls{0};
std::atomic<size_t> g_test_two_calls{0};

struct ClockState {
  int64_t base_unix_micros = 0;
  int64_t base_steady_nanos = 0;
  double scale = 1.0;
};

thread_local ClockState g_clock_state;

int64_t SteadyNanos() {
  return std::chrono::duration_cast<std::chrono::nanoseconds>(
             std::chrono::steady_clock::now().time_since_epoch())
      .count();
}

int64_t ClockMicros() {
  const int64_t elapsed = SteadyNanos() - g_clock_state.base_steady_nanos;
  return g_clock_state.base_unix_micros +
         static_cast<int64_t>((static_cast<double>(elapsed) / 1000.0) *
                              g_clock_state.scale);
}

int64_t ClampMicros(int64_t micros, uint64_t seed, bool isolated) {
  const bool negative = micros < 0;
  if (negative) micros = -micros;
  const int64_t step = isolated ? 5 : 100;
  const int64_t low = micros % 10000000000LL;
  int64_t bucket = low - low % step;
  uint64_t hash = static_cast<uint64_t>(bucket) ^ seed;
  hash ^= hash >> 33;
  hash *= 0xff51afd7ed558ccdULL;
  hash ^= hash >> 33;
  hash *= 0xc4ceb9fe1a85ec53ULL;
  hash ^= hash >> 33;
  const double threshold =
      static_cast<double>(hash & ((uint64_t{1} << 52) - 1)) /
      static_cast<double>(uint64_t{1} << 52);
  if (static_cast<double>(low - bucket) >= threshold * step) bucket += step;
  const int64_t result = micros - low + bucket;
  return negative ? -result : result;
}

struct ClockConfig {
  int32_t kind;
  int64_t origin_micros;
  uint64_t seed;
  double precision_base_millis;
  bool isolated;
};

std::mutex g_clock_config_mutex;
std::unordered_map<v8::Isolate*, std::vector<std::unique_ptr<ClockConfig>>>
    g_clock_configs;

void NativeClockCallback(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto config = static_cast<ClockConfig*>(
      v8::Local<v8::External>::Cast(info.Data())
          ->Value(v8::kExternalPointerTypeTagDefault));
  if (config->kind == 0) {
    info.GetReturnValue().Set(static_cast<double>(ClockMicros()) / 1000.0);
    return;
  }
  int64_t delta = ClampMicros(ClockMicros(), config->seed, config->isolated) -
                  ClampMicros(config->origin_micros, config->seed,
                              config->isolated);
  if (delta < 0) delta = 0;
  const double millis = static_cast<double>(delta) / 1000.0;
  info.GetReturnValue().Set(
      (config->precision_base_millis + millis) -
      config->precision_base_millis);
}

uint32_t TestFastAdd(v8::Local<v8::Object>, uint32_t a, uint32_t b) {
  g_test_add_calls.fetch_add(1, std::memory_order_seq_cst);
  return a + b;
}

uint32_t TestFastOne(v8::Local<v8::Object>, uint32_t a) {
  g_test_one_calls.fetch_add(1, std::memory_order_seq_cst);
  return 100 + a;
}

uint32_t TestFastTwo(v8::Local<v8::Object>, uint32_t a, uint32_t b) {
  g_test_two_calls.fetch_add(1, std::memory_order_seq_cst);
  return 200 + a + b;
}

bool ValidType(uint8_t type) { return type <= 13 || type == 255; }

bool IntegralType(uint8_t type) {
  return type == 2 || type == 3 || type == 4 || type == 5 || type == 6;
}

bool FloatingType(uint8_t type) { return type == 7 || type == 8; }

bool ValidTypeSpec(uint8_t type, uint8_t flags) {
  if (!ValidType(type) || (flags & ~uint8_t{0x0f}) != 0) return false;
  if (type == 255 && flags != 0) return false;
  if ((flags & uint8_t{0x06}) != 0 && !IntegralType(type)) return false;
  if ((flags & uint8_t{0x08}) != 0 && !FloatingType(type)) return false;
  return true;
}

template <class T>
v8::Local<T> PointerToLocal(T* const& pointer) {
  return *reinterpret_cast<const v8::Local<T>*>(&pointer);
}

void RemoveDescriptorSet(v8::Isolate* isolate, DescriptorSet* target) {
  std::lock_guard<std::mutex> lock(g_descriptor_mutex);
  auto map_it = g_descriptor_sets.find(isolate);
  if (map_it == g_descriptor_sets.end()) return;
  auto& sets = map_it->second;
  sets.erase(std::remove_if(sets.begin(), sets.end(),
                            [target](const auto& item) {
                              return item.get() == target;
                            }),
             sets.end());
  if (sets.empty()) g_descriptor_sets.erase(map_it);
}

}  // namespace gov8_fast_api_impl

extern "C" {

__declspec(dllexport) int64_t gov8_fast_clock_publish(
    int64_t unix_micros, uint64_t scale_bits) {
  double scale = 1.0;
  static_assert(sizeof(scale) == sizeof(scale_bits));
  std::memcpy(&scale, &scale_bits, sizeof(scale));
  if (!std::isfinite(scale) || scale < 0) {
    SetErr("invalid fast clock scale");
    return kErrBadArg;
  }
  gov8_fast_api_impl::g_clock_state = {
      unix_micros, gov8_fast_api_impl::SteadyNanos(), scale};
  return kOk;
}

__declspec(dllexport) int64_t gov8_native_clock_function_new(
    v8::Isolate* isolate, void* scope, void* context_wire, int32_t kind,
    int64_t origin_micros, uint64_t seed, uint64_t precision_base_bits,
    int32_t isolated) {
  using namespace gov8_fast_api_impl;
  ClearErr();
  auto context_wrap = AsCtx(context_wire);
  if (isolate == nullptr || !ScopeIs(isolate, scope) ||
      context_wrap == nullptr || context_wrap->iso != isolate ||
      (kind != 0 && kind != 1) ||
      (isolated != 0 && isolated != 1)) {
    SetErr("invalid native clock function argument");
    return kErrBadArg;
  }
  double precision_base = 0;
  std::memcpy(&precision_base, &precision_base_bits, sizeof(precision_base));
  auto config = std::make_unique<ClockConfig>(ClockConfig{
      kind, origin_micros, seed, precision_base, isolated != 0});
  ClockConfig* retained = config.get();
  std::lock_guard<std::mutex> lock(g_clock_config_mutex);
  g_clock_configs[isolate].push_back(std::move(config));
  Gov8IsolateScope isolate_scope(isolate);
  auto context = context_wrap->ctx->Get(isolate);
  v8::Context::Scope context_scope(context);
  auto data = v8::External::New(isolate, retained,
                                v8::kExternalPointerTypeTagDefault);
  v8::Local<v8::Function> function;
  if (!v8::Function::New(context, NativeClockCallback, data, 0,
                         v8::ConstructorBehavior::kThrow,
                         v8::SideEffectType::kHasNoSideEffect)
           .ToLocal(&function)) {
    g_clock_configs[isolate].pop_back();
    if (g_clock_configs[isolate].empty()) g_clock_configs.erase(isolate);
    SetErr("native clock function construction failed");
    return kErrGeneric;
  }
  return reinterpret_cast<int64_t>(ToWire(function));
}

__declspec(dllexport) int64_t gov8_fast_api_function_template_new(
    v8::Isolate* isolate, void* scope, void* host_context, int32_t length,
    void* signature_wire, int32_t side_effect_type,
    const gov8_fast_api_impl::FastFunctionSpec* function_specs,
    size_t function_count,
    const gov8_fast_api_impl::FastTypeSpec* argument_specs,
    size_t argument_count) {
  using namespace gov8_fast_api_impl;
  ClearErr();
  if (isolate == nullptr || !ScopeIs(isolate, scope) ||
      host_context == nullptr ||
      (function_specs == nullptr && function_count != 0) ||
      (argument_specs == nullptr && argument_count != 0) ||
      side_effect_type < static_cast<int32_t>(v8::SideEffectType::kHasSideEffect) ||
      side_effect_type >
          static_cast<int32_t>(v8::SideEffectType::kHasSideEffectToReceiver)) {
    SetErr("invalid fast FunctionTemplate argument");
    return kErrBadArg;
  }

  try {
    auto descriptors = std::make_unique<DescriptorSet>();
    descriptors->argument_storage.reserve(function_count);
    descriptors->type_infos.reserve(function_count);
    descriptors->functions.reserve(function_count);
    for (size_t index = 0; index < function_count; ++index) {
      const FastFunctionSpec& function = function_specs[index];
      if (function.address == 0 || function.reserved != 0 ||
          function.repr > 1 ||
          !ValidTypeSpec(function.return_type, function.return_flags) ||
          function.return_type == 255 ||
          function.arg_offset > argument_count ||
          function.arg_count > argument_count - function.arg_offset) {
        SetErr("invalid fast function descriptor");
        return kErrBadArg;
      }

      auto arguments = std::make_unique<std::vector<v8::CTypeInfo>>();
      arguments->reserve(function.arg_count);
      bool seen_options = false;
      for (uint32_t arg_index = 0; arg_index < function.arg_count;
           ++arg_index) {
        const FastTypeSpec& argument =
            argument_specs[function.arg_offset + arg_index];
        if (!ValidTypeSpec(argument.type, argument.flags) || seen_options ||
            (argument.type == 255 && arg_index + 1 != function.arg_count)) {
          SetErr("invalid fast function argument descriptor");
          return kErrBadArg;
        }
        seen_options = argument.type == 255;
        arguments->emplace_back(
            argument.type == 255
                ? v8::CTypeInfo::kCallbackOptionsType
                : static_cast<v8::CTypeInfo::Type>(argument.type),
            static_cast<v8::CTypeInfo::Flags>(argument.flags));
      }

      const v8::CTypeInfo return_info(
          static_cast<v8::CTypeInfo::Type>(function.return_type),
          static_cast<v8::CTypeInfo::Flags>(function.return_flags));
      auto type_info = std::make_unique<v8::CFunctionInfo>(
          return_info, function.arg_count, arguments->data(),
          static_cast<v8::CFunctionInfo::Int64Representation>(function.repr));
      descriptors->argument_storage.push_back(std::move(arguments));
      descriptors->type_infos.push_back(std::move(type_info));
      descriptors->functions.emplace_back(
          reinterpret_cast<const void*>(function.address),
          descriptors->type_infos.back().get());
    }

    DescriptorSet* retained = descriptors.get();
    const bool published = function_count != 0;
    if (published) {
      // Publish ownership before V8 sees the span. If a later operation fails,
      // RemoveDescriptorSet rolls the exact allocation back.
      std::lock_guard<std::mutex> lock(g_descriptor_mutex);
      g_descriptor_sets[isolate].push_back(std::move(descriptors));
    }

    void* result = nullptr;
    try {
      Gov8IsolateScope isolate_scope(isolate);
      result = RegisterNativeCallback(
          isolate, static_cast<GoCallbackContext*>(host_context),
          [&](v8::Local<v8::Value> data) -> void* {
            const v8::Local<v8::Signature> signature =
                signature_wire == nullptr
                    ? v8::Local<v8::Signature>()
                    : FromWire<v8::Signature>(signature_wire);
            const v8::FunctionTemplate* raw = v8__FunctionTemplate__New(
                isolate, HostFunctionTrampoline,
                data.IsEmpty() ? nullptr : data.operator->(),
                signature.IsEmpty() ? nullptr : signature.operator->(), length,
                v8::ConstructorBehavior::kThrow,
                static_cast<v8::SideEffectType>(side_effect_type),
                retained->functions.data(), retained->functions.size());
            if (raw == nullptr) return nullptr;
            // The flat binding returns an object pointer, not a Local's slot
            // address. Materialize it into the caller's open HandleScope
            // before returning a wire value to Go.
            auto raw_mutable = const_cast<v8::FunctionTemplate*>(raw);
            const v8::Local<v8::FunctionTemplate> function_template =
                v8::Local<v8::FunctionTemplate>::New(
                    isolate, PointerToLocal(raw_mutable));
            return ToWire(function_template);
          });
    } catch (...) {
      if (published) RemoveDescriptorSet(isolate, retained);
      throw;
    }
    if (result == nullptr) {
      if (published) RemoveDescriptorSet(isolate, retained);
      SetErr("fast FunctionTemplate construction failed");
      return kErrGeneric;
    }
    return reinterpret_cast<int64_t>(result);
  } catch (const std::bad_alloc&) {
    SetErr("out of memory constructing fast API descriptors");
    return kErrNoMemory;
  } catch (...) {
    SetErr("C++ exception constructing fast FunctionTemplate");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_fast_api_after_isolate_dispose(
    uintptr_t isolate_address) {
  using namespace gov8_fast_api_impl;
  ClearErr();
  if (isolate_address == 0) {
    SetErr("null isolate");
    return kErrBadArg;
  }
  // The isolate has already been disposed. Convert its former address only
  // for unordered_map key equality; never invoke a V8 method through it.
  v8::Isolate* isolate = reinterpret_cast<v8::Isolate*>(isolate_address);
  std::lock_guard<std::mutex> lock(g_descriptor_mutex);
  g_descriptor_sets.erase(isolate);
  {
    std::lock_guard<std::mutex> clock_lock(g_clock_config_mutex);
    g_clock_configs.erase(isolate);
  }
  return kOk;
}

__declspec(dllexport) int64_t gov8_fast_api_descriptor_count(
    v8::Isolate* isolate, size_t* out) {
  using namespace gov8_fast_api_impl;
  ClearErr();
  if (isolate == nullptr || out == nullptr) {
    SetErr("invalid descriptor count argument");
    return kErrBadArg;
  }
  *out = 0;
  std::lock_guard<std::mutex> lock(g_descriptor_mutex);
  auto it = g_descriptor_sets.find(isolate);
  if (it != g_descriptor_sets.end()) *out = it->second.size();
  return kOk;
}

// Conformance-only native entry addresses and counters. These keep the Go
// fixture executable without ever treating a Go callback as a fast C ABI.
__declspec(dllexport) uintptr_t gov8_fast_api_test_address(int32_t kind) {
  using namespace gov8_fast_api_impl;
  switch (kind) {
    case 0:
      return reinterpret_cast<uintptr_t>(&TestFastAdd);
    case 1:
      return reinterpret_cast<uintptr_t>(&TestFastOne);
    case 2:
      return reinterpret_cast<uintptr_t>(&TestFastTwo);
    default:
      return 0;
  }
}

__declspec(dllexport) int64_t gov8_fast_api_test_reset_counters(void) {
  using namespace gov8_fast_api_impl;
  g_test_add_calls.store(0, std::memory_order_seq_cst);
  g_test_one_calls.store(0, std::memory_order_seq_cst);
  g_test_two_calls.store(0, std::memory_order_seq_cst);
  return kOk;
}

__declspec(dllexport) uintptr_t gov8_fast_api_test_counter(int32_t kind) {
  using namespace gov8_fast_api_impl;
  switch (kind) {
    case 0:
      return g_test_add_calls.load(std::memory_order_seq_cst);
    case 1:
      return g_test_one_calls.load(std::memory_order_seq_cst);
    case 2:
      return g_test_two_calls.load(std::memory_order_seq_cst);
    default:
      return 0;
  }
}

__declspec(dllexport) uintptr_t gov8_fast_api_test_layout(int32_t kind) {
  switch (kind) {
    case 0:
      return sizeof(v8::CTypeInfo);
    case 1:
      return sizeof(v8::CFunctionInfo);
    case 2:
      return sizeof(v8::CFunction);
    case 3:
      return alignof(v8::CFunction);
    default:
      return 0;
  }
}

}  // extern "C"

#include "fast_api_residual.inc"
