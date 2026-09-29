// Snapshot-portable constructor stubs for generated WebAPI interfaces. Each
// function retains only a V8 integer ID. The implementation dispatcher is
// stored privately on the current context's global object.
namespace gov8_generated_constructor_impl {

v8::Local<v8::Private> DispatcherKey(v8::Isolate* isolate) {
  return v8::Private::ForApi(isolate,
      v8::String::NewFromUtf8Literal(isolate,
          "gov8.generated-constructor.dispatcher"));
}

void Call(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto* isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  v8::Local<v8::Value> dispatcher_value;
  if (!context->Global()->GetPrivate(context, DispatcherKey(isolate))
           .ToLocal(&dispatcher_value)) return;
  if (!dispatcher_value->IsFunction()) {
    isolate->ThrowException(v8::Exception::Error(
        v8::String::NewFromUtf8Literal(isolate,
            "generated constructor dispatcher is not registered")));
    return;
  }
  const int argc = info.Length();
  auto arguments = v8::Array::New(isolate, argc);
  for (int index = 0; index < argc; ++index) {
    if (!arguments->CreateDataProperty(context, index, info[index])
             .FromMaybe(false)) return;
  }
  v8::Local<v8::Value> args[] = {
      info.Data(), info.ThisValue(), arguments,
      info.IsConstructCall() ? info.NewTarget().As<v8::Value>()
                             : v8::Undefined(isolate).As<v8::Value>()};
  v8::Local<v8::Value> result;
  if (dispatcher_value.As<v8::Function>()
          ->Call(context, v8::Undefined(isolate), 4, args)
          .ToLocal(&result)) {
    info.GetReturnValue().Set(result);
  }
}

void Factory(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto* isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  if (info.Length() == 1 && info[0]->IsFunction()) {
    if (context->Global()->SetPrivate(context, DispatcherKey(isolate), info[0])
            .FromMaybe(false)) info.GetReturnValue().Set(true);
    return;
  }
  if (info.Length() == 2 && info[0]->IsString() && info[1]->IsInt32() &&
      info[1].As<v8::Int32>()->Value() >= 0) {
    v8::Local<v8::Function> constructor;
    if (!v8::Function::New(context, Call, info[1], 0,
                           v8::ConstructorBehavior::kAllow)
             .ToLocal(&constructor)) return;
    constructor->SetName(info[0].As<v8::String>());
    info.GetReturnValue().Set(constructor);
    return;
  }
  isolate->ThrowException(v8::Exception::TypeError(
      v8::String::NewFromUtf8Literal(isolate,
          "invalid generated constructor factory call")));
}

}  // namespace gov8_generated_constructor_impl

extern "C" {

__declspec(dllexport) int64_t gov8_generated_constructor_callback_address(
    int32_t index, intptr_t* out) {
  ClearErr();
  if (out == nullptr || index < 0 || index >= 2) {
    SetErr("invalid generated constructor callback index");
    return kErrBadArg;
  }
  *out = index == 0
      ? reinterpret_cast<intptr_t>(&gov8_generated_constructor_impl::Factory)
      : reinterpret_cast<intptr_t>(&gov8_generated_constructor_impl::Call);
  return kOk;
}

__declspec(dllexport) void* gov8_generated_constructor_factory_new(
    v8::Isolate* isolate, void* scope, void* context_handle) {
  ClearErr();
  auto* context_wrap = AsCtx(context_handle);
  if (isolate == nullptr || !ScopeIs(isolate, scope) ||
      context_wrap == nullptr || context_wrap->iso != isolate) {
    SetErr("invalid generated constructor factory context");
    return nullptr;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    auto context = context_wrap->ctx->Get(isolate);
    v8::Context::Scope context_scope(context);
    v8::Local<v8::Function> factory;
    if (!v8::Function::New(context, gov8_generated_constructor_impl::Factory,
                           v8::Local<v8::Value>(), 2,
                           v8::ConstructorBehavior::kThrow)
             .ToLocal(&factory)) {
      SetErr("generated constructor factory creation failed");
      return nullptr;
    }
    return ToWire(factory);
  } catch (...) {
    SetErr("C++ exception creating generated constructor factory");
    return nullptr;
  }
}

}  // extern "C"
