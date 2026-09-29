// Snapshot-portable, non-intercepting property observation. Callback data is
// a realm-owned JS function, never a Go registry handle or a native allocation.
namespace gov8_observation_impl {

// Each Mimic isolate runs on a pinned owner thread. Keep the diagnostic gate
// there rather than in a process-wide table on every property access. The
// unconfigured/default state preserves standalone gov8 observation behavior.
thread_local v8::Isolate* gated_isolate = nullptr;
thread_local bool enabled = true;

// Inspect only real properties on ordinary objects. An author's prototype
// Proxy must not receive extra has/getOwnPropertyDescriptor traps just because
// diagnostics classify an access before its actual lookup.
bool Supported(v8::Local<v8::Object> object, v8::Local<v8::Name> key,
               v8::Isolate* isolate) {
  auto context = isolate->GetCurrentContext();
  v8::Local<v8::Value> cursor = object;
  while (cursor->IsObject() && !cursor->IsProxy()) {
    auto current = cursor.As<v8::Object>();
    if (current->HasRealNamedProperty(context, key).FromMaybe(false)) return true;
    cursor = current->GetPrototype();
  }
  return false;
}

bool Observe(v8::Local<v8::Value> data, v8::Local<v8::Object> receiver,
             v8::Local<v8::Name> key, bool write, v8::Isolate* isolate) {
  if (gated_isolate == isolate && !enabled) return true;
  v8::Local<v8::Value> args[] = {
      receiver, key, v8::Boolean::New(isolate, write),
      v8::Boolean::New(isolate, Supported(receiver, key, isolate))};
  return !data.As<v8::Function>()
              ->Call(isolate->GetCurrentContext(), v8::Undefined(isolate), 4,
                     args)
              .IsEmpty();
}

v8::Intercepted Get(v8::Local<v8::Name> key,
                    const v8::PropertyCallbackInfo<v8::Value>& info) {
  auto holder = info.Holder();
  return Observe(holder->GetInternalField(0).As<v8::Value>(), holder, key,
                 false, info.GetIsolate())
             ? v8::Intercepted::kNo
             : v8::Intercepted::kYes;
}

v8::Intercepted Set(v8::Local<v8::Name> key, v8::Local<v8::Value>,
                    const v8::PropertyCallbackInfo<void>& info) {
  auto holder = info.Holder();
  return Observe(holder->GetInternalField(0).As<v8::Value>(), holder, key,
                 true, info.GetIsolate())
             ? v8::Intercepted::kNo
             : v8::Intercepted::kYes;
}

v8::Intercepted GetIndex(uint32_t index,
                         const v8::PropertyCallbackInfo<v8::Value>& info) {
  auto key = v8::Integer::NewFromUnsigned(info.GetIsolate(), index)
                 ->ToString(info.GetIsolate()->GetCurrentContext());
  v8::Local<v8::String> string;
  if (!key.ToLocal(&string)) return v8::Intercepted::kYes;
  return Get(string, info);
}

v8::Intercepted SetIndex(uint32_t index, v8::Local<v8::Value> value,
                         const v8::PropertyCallbackInfo<void>& info) {
  auto key = v8::Integer::NewFromUnsigned(info.GetIsolate(), index)
                 ->ToString(info.GetIsolate()->GetCurrentContext());
  v8::Local<v8::String> string;
  if (!key.ToLocal(&string)) return v8::Intercepted::kYes;
  return Set(string, value, info);
}

// An explicit query fallthrough prevents V8 from using the getter to answer
// Reflect.has. Observation's own support check must not observe itself.
v8::Intercepted Query(v8::Local<v8::Name>,
                      const v8::PropertyCallbackInfo<v8::Integer>&) {
  return v8::Intercepted::kNo;
}

v8::Intercepted QueryIndex(uint32_t,
                           const v8::PropertyCallbackInfo<v8::Integer>&) {
  return v8::Intercepted::kNo;
}

void Create(const v8::FunctionCallbackInfo<v8::Value>& info) {
  auto isolate = info.GetIsolate();
  if (info.Length() != 1 || !info[0]->IsFunction()) {
    isolate->ThrowException(v8::Exception::TypeError(
        v8::String::NewFromUtf8Literal(isolate, "Property observer must be callable")));
    return;
  }
  auto object = v8::ObjectTemplate::New(isolate);
  // InterceptorInfo belongs to V8's shared startup cache. Realm-owned callback
  // functions must live on the instance, where snapshot cloning remaps their
  // context and captured state independently for each restored realm.
  object->SetInternalFieldCount(1);
  object->SetHandler(v8::NamedPropertyHandlerConfiguration(
      Get, Set, Query, nullptr, nullptr, v8::Local<v8::Value>(),
      static_cast<v8::PropertyHandlerFlags>(
          static_cast<int>(v8::PropertyHandlerFlags::kOnlyInterceptStrings) |
          static_cast<int>(v8::PropertyHandlerFlags::kObservationOnly))));
  object->SetHandler(v8::IndexedPropertyHandlerConfiguration(
      GetIndex, SetIndex, QueryIndex, nullptr, nullptr,
      v8::Local<v8::Value>(), v8::PropertyHandlerFlags::kObservationOnly));
  v8::Local<v8::Object> instance;
  if (object->NewInstance(isolate->GetCurrentContext()).ToLocal(&instance)) {
    instance->SetInternalField(0, info[0]);
    info.GetReturnValue().Set(instance);
  }
}

intptr_t Address(int32_t index) {
  switch (index) {
    case 0: return reinterpret_cast<intptr_t>(&Create);
    case 1: return reinterpret_cast<intptr_t>(&Get);
    case 2: return reinterpret_cast<intptr_t>(&Set);
    case 3: return reinterpret_cast<intptr_t>(&GetIndex);
    case 4: return reinterpret_cast<intptr_t>(&SetIndex);
    case 5: return reinterpret_cast<intptr_t>(&Query);
    case 6: return reinterpret_cast<intptr_t>(&QueryIndex);
    default: return 0;
  }
}
}  // namespace gov8_observation_impl

extern "C" {
__declspec(dllexport) int64_t gov8_observation_set_enabled(
    v8::Isolate* isolate, int32_t value) {
  ClearErr();
  if (isolate == nullptr || (value != 0 && value != 1)) {
    SetErr("invalid property observation gate");
    return kErrBadArg;
  }
  gov8_observation_impl::gated_isolate = isolate;
  gov8_observation_impl::enabled = value != 0;
  return kOk;
}

__declspec(dllexport) int64_t gov8_observation_callback_address(
    int32_t index, intptr_t* out) {
  ClearErr();
  if (out == nullptr || index < 0 || index >= 7) {
    SetErr("invalid property observation callback index");
    return kErrBadArg;
  }
  *out = gov8_observation_impl::Address(index);
  return kOk;
}

__declspec(dllexport) void* gov8_observation_factory_new(
    v8::Isolate* isolate, void* scope, void* context_handle) {
  ClearErr();
  auto context_wrap = AsCtx(context_handle);
  if (isolate == nullptr || !ScopeIs(isolate, scope) || context_wrap == nullptr ||
      context_wrap->iso != isolate) {
    SetErr("invalid property observation factory context");
    return nullptr;
  }
  Gov8IsolateScope isolate_scope(isolate);
  auto context = context_wrap->ctx->Get(isolate);
  v8::Context::Scope context_scope(context);
  v8::Local<v8::Function> function;
  if (!v8::Function::New(context, gov8_observation_impl::Create,
                         v8::Local<v8::Value>(), 1,
                         v8::ConstructorBehavior::kThrow).ToLocal(&function)) {
    SetErr("property observation factory creation failed");
    return nullptr;
  }
  return ToWire(function);
}
}
