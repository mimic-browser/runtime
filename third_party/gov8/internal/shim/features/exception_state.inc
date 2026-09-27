// Initialize fresh platform exceptions without altering stack formatting.
extern "C" bool v8__Mimic__ClearExceptionOriginalMessage(
    v8::Isolate* isolate, const v8::Object* object);

namespace gov8_exception_state_impl {
void Initialize(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto isolate = info.GetIsolate();
  if (info.Length() != 1 || !info[0]->IsNativeError() ||
      !v8__Mimic__ClearExceptionOriginalMessage(isolate, *info[0].As<v8::Object>())) {
    isolate->ThrowException(v8::Exception::TypeError(v8::String::NewFromUtf8Literal(
        isolate, "Platform exception initialization failed")));
    return;
  }
  info.GetReturnValue().Set(info[0]);
}
}  // namespace gov8_exception_state_impl

extern "C" {
__declspec(dllexport) int64_t gov8_exception_state_callback_address(
    int32_t index, intptr_t* out) {
  ClearErr();
  if (out == nullptr || index < 0 || index >= 1) {
    SetErr("invalid exception state callback index");
    return kErrBadArg;
  }
  *out = reinterpret_cast<intptr_t>(&gov8_exception_state_impl::Initialize);
  return kOk;
}

__declspec(dllexport) void* gov8_exception_state_factory_new(
    v8::Isolate* isolate, void* scope, void* context_handle) {
  ClearErr();
  auto context_wrap = AsCtx(context_handle);
  if (isolate == nullptr || !ScopeIs(isolate, scope) || context_wrap == nullptr ||
      context_wrap->iso != isolate) {
    SetErr("invalid exception state factory context");
    return nullptr;
  }
  Gov8IsolateScope isolate_scope(isolate);
  auto context = context_wrap->ctx->Get(isolate);
  v8::Context::Scope context_scope(context);
  v8::Local<v8::Function> function;
  if (!v8::Function::New(context, gov8_exception_state_impl::Initialize,
                        v8::Local<v8::Value>(), 1,
                        v8::ConstructorBehavior::kThrow).ToLocal(&function)) {
    SetErr("exception state factory creation failed");
    return nullptr;
  }
  return ToWire(function);
}
}
