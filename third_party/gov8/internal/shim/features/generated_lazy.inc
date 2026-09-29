// Snapshot-portable lazy WebAPI data properties. A numeric member ID is V8
// data, never a Go closure or native pointer. Every context owns its factory.
namespace gov8_generated_lazy_impl {

v8::Local<v8::Private> FactoryKey(v8::Isolate* isolate) {
  return v8::Private::ForApi(
      isolate, v8::String::NewFromUtf8Literal(isolate, "gov8.generated-lazy.factory"));
}

void Get(v8::Local<v8::Name> property,
         const v8::PropertyCallbackInfo<v8::Value>& info) {
  auto* isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  v8::Local<v8::Value> factory_value;
  if (!context->Global()->GetPrivate(context, FactoryKey(isolate))
           .ToLocal(&factory_value)) return;
  if (!factory_value->IsFunction()) {
    isolate->ThrowException(v8::Exception::Error(
        v8::String::NewFromUtf8Literal(isolate,
            "lazy property factory is not registered for this context")));
    return;
  }
  v8::Local<v8::Value> args[] = {info.Data(), info.HolderV2(), property};
  v8::Local<v8::Value> result;
  if (factory_value.As<v8::Function>()
          ->Call(context, context->Global(), 3, args).ToLocal(&result)) {
    info.GetReturnValue().Set(result);
  }
}

bool Install(v8::Isolate* isolate, v8::Local<v8::Context> context,
             v8::Local<v8::Object> target,
             v8::Local<v8::Name> name, int32_t id, int32_t attributes) {
  return target->SetLazyDataProperty(
      context, name, Get, v8::Int32::New(isolate, id),
      static_cast<v8::PropertyAttribute>(attributes),
      v8::SideEffectType::kHasSideEffect,
      v8::SideEffectType::kHasSideEffect).FromMaybe(false);
}

void Installer(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto* isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  if (info.Length() == 1 && info[0]->IsFunction()) {
    if (context->Global()->SetPrivate(context, FactoryKey(isolate), info[0])
            .FromMaybe(false)) info.GetReturnValue().Set(true);
    return;
  }
  if (info.Length() == 4 && info[0]->IsObject() && info[1]->IsName() &&
      info[2]->IsInt32() && info[2].As<v8::Int32>()->Value() >= 0 &&
      info[3]->IsInt32() &&
      (info[3].As<v8::Int32>()->Value() & ~7) == 0) {
    const bool installed = Install(
        isolate, context, info[0].As<v8::Object>(), info[1].As<v8::Name>(),
        info[2].As<v8::Int32>()->Value(),
        info[3].As<v8::Int32>()->Value());
    info.GetReturnValue().Set(installed);
    return;
  }
  isolate->ThrowException(v8::Exception::TypeError(
      v8::String::NewFromUtf8Literal(isolate,
          "invalid generated lazy property installation")));
}

}  // namespace gov8_generated_lazy_impl

extern "C" {

__declspec(dllexport) int64_t gov8_generated_lazy_callback_address(
    int32_t index, intptr_t* out) {
  ClearErr();
  if (out == nullptr || index < 0 || index >= 2) {
    SetErr("invalid generated lazy callback index");
    return kErrBadArg;
  }
  *out = index == 0
      ? reinterpret_cast<intptr_t>(&gov8_generated_lazy_impl::Installer)
      : reinterpret_cast<intptr_t>(&gov8_generated_lazy_impl::Get);
  return kOk;
}

__declspec(dllexport) void* gov8_generated_lazy_installer_new(
    v8::Isolate* isolate, void* scope, void* context_handle) {
  ClearErr();
  auto* context_wrap = AsCtx(context_handle);
  if (isolate == nullptr || !ScopeIs(isolate, scope) ||
      context_wrap == nullptr || context_wrap->iso != isolate) {
    SetErr("invalid generated lazy installer context");
    return nullptr;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    auto context = context_wrap->ctx->Get(isolate);
    v8::Context::Scope context_scope(context);
    v8::Local<v8::Function> installer;
    if (!v8::Function::New(context, gov8_generated_lazy_impl::Installer,
                           v8::Local<v8::Value>(), 4,
                           v8::ConstructorBehavior::kThrow)
             .ToLocal(&installer)) {
      SetErr("generated lazy installer creation failed");
      return nullptr;
    }
    return ToWire(installer);
  } catch (...) {
    SetErr("C++ exception creating generated lazy installer");
    return nullptr;
  }
}

__declspec(dllexport) int64_t gov8_generated_lazy_template_set(
    v8::Isolate* isolate, void* scope, void* template_wire, void* key_wire,
    int32_t member_id, int32_t attributes) {
  ClearErr();
  if (isolate == nullptr || !ScopeIs(isolate, scope) ||
      template_wire == nullptr || key_wire == nullptr || member_id < 0 ||
      (attributes & ~7) != 0) {
    SetErr("invalid generated lazy template property");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope isolate_scope(isolate);
    auto key = FromWire<v8::Value>(key_wire);
    if (!key->IsName()) {
      SetErr("generated lazy template key is not a Name");
      return kErrBadArg;
    }
    FromWire<v8::ObjectTemplate>(template_wire)->SetLazyDataProperty(
        key.As<v8::Name>(), gov8_generated_lazy_impl::Get,
        v8::Int32::New(isolate, member_id),
        static_cast<v8::PropertyAttribute>(attributes));
    return kOk;
  } catch (...) {
    SetErr("C++ exception setting generated lazy template property");
    return kErrCpp;
  }
}

}  // extern "C"
