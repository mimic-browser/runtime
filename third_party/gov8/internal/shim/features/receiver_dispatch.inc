// A semantic receiver gate with realm-owned JS data.
// The native public method handles brand failures itself. No JavaScript guard
// executes on that path and no exception/stack text is reconstructed.
namespace gov8_receiver_dispatch_impl {

void Call(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  auto data = info.Data().As<v8::Array>();
  v8::Local<v8::Value> validator_value, original_value, implementation_value;
  if (!data->Get(context, 0).ToLocal(&validator_value) ||
      !data->Get(context, 1).ToLocal(&original_value) ||
      !data->Get(context, 2).ToLocal(&implementation_value)) return;

  auto receiver = info.ThisValue();
  v8::TryCatch caught(isolate);
  v8::Local<v8::Value> ignored;
  bool valid = validator_value.As<v8::Function>()
                   ->Call(context, receiver, 0, nullptr).ToLocal(&ignored);
  if (!valid) {
    if (!caught.CanContinue()) {
      caught.ReThrow();
      return;
    }
    caught.Reset();
  }
  auto selected = (valid ? implementation_value : original_value)
                      .As<v8::Function>();
  std::vector<v8::Local<v8::Value>> arguments;
  arguments.reserve(info.Length());
  for (int index = 0; index < info.Length(); ++index) arguments.push_back(info[index]);
  v8::Local<v8::Value> result;
  if (selected->Call(context, receiver, info.Length(), arguments.data())
          .ToLocal(&result)) {
    info.GetReturnValue().Set(result);
  } else if (caught.HasCaught()) {
    caught.ReThrow();
  }
}

void Create(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto isolate = info.GetIsolate();
  auto context = isolate->GetCurrentContext();
  if (info.Length() != 5 || !info[0]->IsFunction() || !info[1]->IsFunction() ||
      !info[2]->IsFunction() || !info[3]->IsString() || !info[4]->IsInt32() ||
      info[4].As<v8::Int32>()->Value() < 0) {
    isolate->ThrowException(v8::Exception::TypeError(v8::String::NewFromUtf8Literal(
        isolate, "Receiver dispatch requires three functions, a name and a length")));
    return;
  }
  // Genuine intrinsics/API callbacks have no source Script. Platform JS has a
  // Script even when compiled as non-user code; preserve only actual intrinsics.
  if (info[1]->StrictEquals(info[2]) &&
      info[2].As<v8::Function>()->ScriptId() == v8::UnboundScript::kNoScriptId) {
    info.GetReturnValue().Set(info[2]);
    return;
  }
  auto data = v8::Array::New(isolate, 3);
  for (uint32_t index = 0; index < 3; ++index) {
    if (!data->CreateDataProperty(context, index, info[index]).FromMaybe(false)) return;
  }
  v8::Local<v8::Function> function;
  if (!v8::Function::NewStrict(context, Call, data,
                               info[4].As<v8::Int32>()->Value()).ToLocal(&function)) return;
  function->SetName(info[3].As<v8::String>());
  info.GetReturnValue().Set(function);
}

}  // namespace gov8_receiver_dispatch_impl

extern "C" {
__declspec(dllexport) int64_t gov8_receiver_dispatch_callback_address(
    int32_t index, intptr_t* out) {
  ClearErr();
  if (out == nullptr || index < 0 || index >= 2) {
    SetErr("invalid receiver dispatch callback index");
    return kErrBadArg;
  }
  *out = index == 0 ? reinterpret_cast<intptr_t>(&gov8_receiver_dispatch_impl::Create)
                   : reinterpret_cast<intptr_t>(&gov8_receiver_dispatch_impl::Call);
  return kOk;
}

__declspec(dllexport) void* gov8_receiver_dispatch_factory_new(
    v8::Isolate* isolate, void* scope, void* context_handle) {
  ClearErr();
  auto context_wrap = AsCtx(context_handle);
  if (isolate == nullptr || !ScopeIs(isolate, scope) || context_wrap == nullptr ||
      context_wrap->iso != isolate) {
    SetErr("invalid receiver dispatch factory context");
    return nullptr;
  }
  Gov8IsolateScope isolate_scope(isolate);
  auto context = context_wrap->ctx->Get(isolate);
  v8::Context::Scope context_scope(context);
  v8::Local<v8::Function> function;
  if (!v8::Function::New(context, gov8_receiver_dispatch_impl::Create,
                        v8::Local<v8::Value>(), 5,
                        v8::ConstructorBehavior::kThrow).ToLocal(&function)) {
    SetErr("receiver dispatch factory creation failed");
    return nullptr;
  }
  return ToWire(function);
}
}
