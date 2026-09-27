// ===========================================================================
// Advanced Function slice: builder policy, names, script metadata, bound
// function inspection, and the safe CompileFunction code-cache path.
//
// CreateCodeCacheForFunction has a process-fatal precondition upstream: only
// the exact Function returned by ScriptCompiler::CompileFunction is valid.
// Go enforces that provenance before this include's cache export is reached.
// ===========================================================================

#include <vector>

namespace gov8_fa_impl {

extern "C" {
const v8::Function* v8__ScriptCompiler__CompileFunction(
    const v8::Context* context, v8::ScriptCompiler::Source* source,
    size_t arguments_count, const v8::String** arguments,
    size_t context_extensions_count, const v8::Object** context_extensions,
    v8::ScriptCompiler::CompileOptions options,
    v8::ScriptCompiler::NoCacheReason no_cache_reason);
v8::ScriptCompiler::CachedData* v8__ScriptCompiler__CachedData__NEW(
    const uint8_t* data, int length);
void v8__ScriptCompiler__CachedData__DELETE(
    v8::ScriptCompiler::CachedData* self);
const v8::ScriptCompiler::CachedData*
v8__ScriptCompiler__Source__GetCachedData(
    const v8::ScriptCompiler::Source* source);
void v8__ScriptCompiler__Source__CONSTRUCT(
    void* buf, const v8::String& source_string,
    const v8::ScriptOrigin* origin,
    v8::ScriptCompiler::CachedData* cached_data);
void v8__ScriptCompiler__Source__DESTRUCT(void* self);
v8::ScriptCompiler::CachedData* v8__Function__CreateCodeCache(
    const v8::Function& function);
}

bool ResolveTryCatch(v8::Isolate* iso, void* tcw,
                     FallbackTryCatch* fallback, v8::TryCatch** out) {
  if (tcw == nullptr) {
    *out = fallback->Init(iso);
    return true;
  }
  TcWrap* wrapper = AsTc(tcw);
  if (wrapper == nullptr || !OwnedBy(iso, wrapper->iso)) {
    SetErr("invalid trycatch handle");
    return false;
  }
  *out = wrapper->tc();
  return true;
}

bool ValidSideEffectType(int32_t value) {
  return value >= static_cast<int32_t>(v8::SideEffectType::kHasSideEffect) &&
         value <=
             static_cast<int32_t>(v8::SideEffectType::kHasSideEffectToReceiver);
}

v8::ConstructorBehavior ConstructorBehaviorFrom(int32_t value) {
  return value == 1 ? v8::ConstructorBehavior::kThrow
                    : v8::ConstructorBehavior::kAllow;
}

}  // namespace gov8_fa_impl

extern "C" {

using namespace gov8_fa_impl;

__declspec(dllexport) int64_t gov8_fa_function_new(
    v8::Isolate* iso, void* ctxw, void* scope, void* host_ctx,
    int32_t length, int32_t constructor_behavior, int32_t side_effect_type,
    void** out) {
  ClearErr();
  CtxWrap* context_wrapper = AsCtx(ctxw);
  GoCallbackContext* callback_context =
      static_cast<GoCallbackContext*>(host_ctx);
  if (iso == nullptr || context_wrapper == nullptr ||
      !ScopeIs(iso, scope) || callback_context == nullptr || out == nullptr ||
      !OwnedBy(iso, context_wrapper->iso) ||
      constructor_behavior < 0 || constructor_behavior > 2 ||
      !ValidSideEffectType(side_effect_type)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    v8::Local<v8::Context> context = context_wrapper->ctx->Get(iso);
    v8::Context::Scope context_scope(context);
    return RegisterNativeCallbackStatus(
        iso, callback_context,
        [&](v8::Local<v8::Value> data, void** result) -> int64_t {
          v8::Local<v8::Function> function;
          if (!v8::Function::New(
                   context, HostFunctionTrampoline, data, length,
                   ConstructorBehaviorFrom(constructor_behavior),
                   static_cast<v8::SideEffectType>(side_effect_type))
                   .ToLocal(&function)) {
            SetErr("Function::New failed");
            return kErrGeneric;
          }
          *result = ToWire(function);
          return kOk;
        },
        out);
  } catch (...) {
    SetErr("C++ exception in advanced function new");
    return kErrCpp;
  }
}

// Direct-return companion for hot callers. Non-negative results are function
// wires; negative results preserve the legacy export's exact status and
// thread-local error detail. Keep gov8_fa_function_new for ABI compatibility.
__declspec(dllexport) int64_t gov8_fa_function_new_direct(
    v8::Isolate* iso, void* ctxw, void* scope, void* host_ctx,
    int32_t length, int32_t constructor_behavior, int32_t side_effect_type) {
  void* result = nullptr;
  const int64_t status = gov8_fa_function_new(
      iso, ctxw, scope, host_ctx, length, constructor_behavior,
      side_effect_type, &result);
  return status < 0 ? status : reinterpret_cast<int64_t>(result);
}

__declspec(dllexport) void* gov8_fa_function_template_new(
    v8::Isolate* iso, void* scope, void* host_ctx, int32_t length,
    void* signature_wire, int32_t constructor_behavior,
    int32_t side_effect_type) {
  ClearErr();
  GoCallbackContext* callback_context =
      static_cast<GoCallbackContext*>(host_ctx);
  if (iso == nullptr || !ScopeIs(iso, scope) || callback_context == nullptr ||
      constructor_behavior < 0 || constructor_behavior > 2 ||
      !ValidSideEffectType(side_effect_type)) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    return RegisterNativeCallback(
        iso, callback_context, [&](v8::Local<v8::Value> data) -> void* {
          const v8::Local<v8::Signature> signature =
              signature_wire == nullptr
                  ? v8::Local<v8::Signature>()
                  : FromWire<v8::Signature>(signature_wire);
          const v8::Local<v8::FunctionTemplate> function_template =
              v8::FunctionTemplate::New(
                  iso, HostFunctionTrampoline, data, signature, length,
                  ConstructorBehaviorFrom(constructor_behavior),
                  static_cast<v8::SideEffectType>(side_effect_type));
          if (function_template.IsEmpty()) {
            SetErr("FunctionTemplate::New failed");
            return nullptr;
          }
          return ToWire(function_template);
        });
  } catch (...) {
    SetErr("C++ exception in advanced function template new");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_fa_function_set_name(
    v8::Isolate* iso, void* scope, void* function_wire, void* name_wire) {
  ClearErr();
  if (!ScopeIs(iso, scope) || function_wire == nullptr || name_wire == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    FromWire<v8::Function>(function_wire)
        ->SetName(FromWire<v8::String>(name_wire));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in function set name");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_fa_function_metadata(
    v8::Isolate* iso, void* scope, void* function_wire, int32_t* line,
    int32_t* column, int32_t* script_id, void** resource_name,
    void** source_map_url) {
  ClearErr();
  if (!ScopeIs(iso, scope) || function_wire == nullptr || line == nullptr ||
      column == nullptr || script_id == nullptr || resource_name == nullptr ||
      source_map_url == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    const v8::Local<v8::Function> function =
        FromWire<v8::Function>(function_wire);
    const v8::ScriptOrigin origin = function->GetScriptOrigin();
    *line = function->GetScriptLineNumber();
    *column = function->GetScriptColumnNumber();
    *script_id = function->ScriptId();
    *resource_name = ToWire(origin.ResourceName());
    *source_map_url = ToWire(origin.SourceMapUrl());
    return kOk;
  } catch (...) {
    SetErr("C++ exception in function metadata");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_fa_function_bound_target(
    v8::Isolate* iso, void* scope, void* function_wire, void** out) {
  ClearErr();
  if (!ScopeIs(iso, scope) || function_wire == nullptr || out == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    *out = ToWire(
        FromWire<v8::Function>(function_wire)->GetBoundFunction());
    return kOk;
  } catch (...) {
    SetErr("C++ exception in function bound target");
    return kErrCpp;
  }
}

extern "C" const v8::Function* v8__Mimic__CompilePlatformFunction(
    const v8::Context*, v8::ScriptCompiler::Source*, size_t, const v8::String**,
    size_t, const v8::Object**, v8::ScriptCompiler::CompileOptions,
    v8::ScriptCompiler::NoCacheReason);
static int64_t CompileFunctionAdvanced(
    v8::Isolate* iso, void* ctxw, void* scope, void* tcw, const char* source,
    int64_t source_length, const char* const* parameters,
    int64_t parameter_count, const char* cache, int64_t cache_length,
    int32_t consume_cache, void** out, int32_t* cache_rejected,
    bool platform_bootstrap) {
  ClearErr();
  CtxWrap* context_wrapper = AsCtx(ctxw);
  if (iso == nullptr || context_wrapper == nullptr || !ScopeIs(iso, scope) ||
      !OwnedBy(iso, context_wrapper->iso) || out == nullptr ||
      cache_rejected == nullptr || source_length < 0 || parameter_count < 0 ||
      cache_length < 0 || (source == nullptr && source_length > 0) ||
      (parameters == nullptr && parameter_count > 0) ||
      (cache == nullptr && cache_length > 0) ||
      (consume_cache != 0 && consume_cache != 1) ||
      (consume_cache != 0 && cache_length == 0)) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    v8::Local<v8::Context> context = context_wrapper->ctx->Get(iso);
    v8::Context::Scope context_scope(context);
    FallbackTryCatch fallback;
    v8::TryCatch* try_catch = nullptr;
    if (!ResolveTryCatch(iso, tcw, &fallback, &try_catch)) {
      return kErrBadArg;
    }
    (void)try_catch;

    v8::Local<v8::String> source_string;
    if (!v8::String::NewFromUtf8(
             iso, source, v8::NewStringType::kNormal,
             static_cast<int>(source_length))
             .ToLocal(&source_string)) {
      SetErr("source string allocation failed");
      return kErrGeneric;
    }
    std::vector<v8::Local<v8::String>> args;
    args.reserve(static_cast<size_t>(parameter_count));
    for (int64_t index = 0; index < parameter_count; ++index) {
      if (parameters[index] == nullptr) {
        SetErr("null parameter name");
        return kErrBadArg;
      }
      v8::Local<v8::String> argument;
      if (!v8::String::NewFromUtf8(iso, parameters[index],
                                   v8::NewStringType::kNormal, -1)
               .ToLocal(&argument)) {
        SetErr("parameter name allocation failed");
        return kErrGeneric;
      }
      args.push_back(argument);
    }

    v8::ScriptCompiler::CachedData* cached_data = nullptr;
    if (consume_cache != 0) {
      cached_data = v8__ScriptCompiler__CachedData__NEW(
          reinterpret_cast<const uint8_t*>(cache),
          static_cast<int>(cache_length));
      if (cached_data == nullptr) {
        SetErr("CachedData allocation failed");
        return kErrGeneric;
      }
    }
    alignas(v8::ScriptCompiler::Source)
        unsigned char source_storage[sizeof(v8::ScriptCompiler::Source)];
    v8__ScriptCompiler__Source__CONSTRUCT(
        source_storage, gov8_snap_impl::DerefLocal(source_string), nullptr,
        cached_data);
    const auto compile = platform_bootstrap
        ? v8__Mimic__CompilePlatformFunction
        : gov8_fa_impl::v8__ScriptCompiler__CompileFunction;
    const v8::Function* compiled = compile(
        &gov8_snap_impl::DerefLocal(context),
        reinterpret_cast<v8::ScriptCompiler::Source*>(source_storage),
        args.size(), reinterpret_cast<const v8::String**>(args.data()), 0,
        nullptr,
        consume_cache != 0 ? v8::ScriptCompiler::kConsumeCodeCache
                           : v8::ScriptCompiler::kNoCompileOptions,
        v8::ScriptCompiler::kNoCacheNoReason);
    const v8::ScriptCompiler::CachedData* consumed =
        v8__ScriptCompiler__Source__GetCachedData(
            reinterpret_cast<v8::ScriptCompiler::Source*>(source_storage));
    *cache_rejected =
        consume_cache != 0 && (consumed == nullptr || consumed->rejected) ? 1
                                                                          : 0;
    v8__ScriptCompiler__Source__DESTRUCT(source_storage);
    if (compiled == nullptr) {
      SetErr("compile function failed");
      return kErrException;
    }
    *out = const_cast<void*>(static_cast<const void*>(compiled));
    return kOk;
  } catch (...) {
    SetErr("C++ exception in advanced compile function");
    return kErrCpp;
  }
}

__declspec(dllexport) int64_t gov8_fa_compile_function(
    v8::Isolate* iso, void* ctxw, void* scope, void* tcw, const char* source,
    int64_t source_length, const char* const* parameters,
    int64_t parameter_count, const char* cache, int64_t cache_length,
    int32_t consume_cache, void** out, int32_t* cache_rejected) {
  return CompileFunctionAdvanced(
      iso, ctxw, scope, tcw, source, source_length, parameters, parameter_count,
      cache, cache_length, consume_cache, out, cache_rejected, false);
}

__declspec(dllexport) int64_t gov8_fa_compile_platform_function(
    v8::Isolate* iso, void* ctxw, void* scope, void* tcw, const char* source,
    int64_t source_length, const char* const* parameters,
    int64_t parameter_count, const char* cache, int64_t cache_length,
    int32_t consume_cache, void** out, int32_t* cache_rejected) {
  return CompileFunctionAdvanced(
      iso, ctxw, scope, tcw, source, source_length, parameters, parameter_count,
      cache, cache_length, consume_cache, out, cache_rejected, true);
}

__declspec(dllexport) void* gov8_fa_function_code_cache_new(
    v8::Isolate* iso, void* scope, void* function_wire) {
  ClearErr();
  if (!ScopeIs(iso, scope) || function_wire == nullptr) {
    SetErr("invalid argument");
    return nullptr;
  }
  try {
    Gov8IsolateScope isolate_scope(iso);
    return v8__Function__CreateCodeCache(
        gov8_snap_impl::DerefLocal(FromWire<v8::Function>(function_wire)));
  } catch (...) {
    SetErr("C++ exception in function code cache create");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_fa_function_code_cache_read_delete(
    void* cache, char* buffer, int64_t capacity, int64_t* out_length) {
  ClearErr();
  if (cache == nullptr || buffer == nullptr || capacity < 0 ||
      out_length == nullptr) {
    SetErr("invalid argument");
    return kErrBadArg;
  }
  v8::ScriptCompiler::CachedData* cached_data =
      static_cast<v8::ScriptCompiler::CachedData*>(cache);
  *out_length = static_cast<int64_t>(cached_data->length);
  if (static_cast<int64_t>(cached_data->length) > capacity) {
    v8__ScriptCompiler__CachedData__DELETE(cached_data);
    SetErr("output buffer too small");
    return kErrNoMemory;
  }
  if (cached_data->length > 0 && cached_data->data != nullptr) {
    std::memcpy(buffer, cached_data->data,
                static_cast<size_t>(cached_data->length));
  }
  v8__ScriptCompiler__CachedData__DELETE(cached_data);
  return kOk;
}

}  // extern "C"
