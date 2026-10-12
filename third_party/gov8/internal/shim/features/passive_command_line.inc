// Evaluate with V8's native command-line scope, retaining the raw result rather
// than constructing a Runtime.RemoteObject (which can materialize Error.stack).
#include "v8-inspector.h"

extern "C" void v8_inspector__V8InspectorSession__addInspectedObject(
    v8_inspector::V8InspectorSession*,
    v8_inspector::V8InspectorSession::Inspectable*);

namespace gov8_passive_command_line_impl {
class Client final : public v8_inspector::V8InspectorClient {
 public:
  Client(v8::Isolate* isolate, v8::Local<v8::Context> context,
         v8::Local<v8::Value> last)
      : isolate_(isolate), context_(context), last_(last) {}
  v8::Local<v8::Context> ensureDefaultContextInGroup(int) override {
    return context_;
  }
  void installAdditionalCommandLineAPI(v8::Local<v8::Context> context,
                                      v8::Local<v8::Object> api) override {
    if (!last_.IsEmpty()) {
      auto key = v8::String::NewFromUtf8Literal(isolate_, "$_");
      // V8's CLI scope invokes the private $_ function to obtain its value.
      // The callback returns native data without describing or coercing it.
      auto getter = v8::Function::New(
          context,
          [](const v8::FunctionCallbackInfo<v8::Value>& info) {
            info.GetReturnValue().Set(info.Data());
          },
          last_, 0, v8::ConstructorBehavior::kThrow,
          v8::SideEffectType::kHasNoSideEffect).ToLocalChecked();
      api->CreateDataProperty(context, key, getter).Check();
    }
  }
 private:
  v8::Isolate* isolate_;
  v8::Local<v8::Context> context_;
  v8::Local<v8::Value> last_;
};

class Channel final : public v8_inspector::V8Inspector::Channel {
 public:
  void sendResponse(int, std::unique_ptr<v8_inspector::StringBuffer>) override {}
  void sendNotification(std::unique_ptr<v8_inspector::StringBuffer>) override {}
  void flushProtocolNotifications() override {}
};

class Inspectable final : public v8_inspector::V8InspectorSession::Inspectable {
 public:
  explicit Inspectable(v8::Local<v8::Value> value) : value_(value) {}
  v8::Local<v8::Value> get(v8::Local<v8::Context>) override { return value_; }
 private:
  v8::Local<v8::Value> value_;
};
}  // namespace gov8_passive_command_line_impl

extern "C" __declspec(dllexport) int64_t gov8_passive_command_line_eval(
    v8::Isolate* isolate, void* scope, void* context_raw, int32_t source_is8,
    const void* source_data, size_t source_length, void* const* selected,
    size_t selected_length, void* last_wire, int32_t* out_status,
    void** out_value) {
  using namespace gov8_passive_command_line_impl;
  ClearErr();
  if (out_status) *out_status = 0;
  if (out_value) *out_value = nullptr;
  auto* context = AsCtx(context_raw);
  if (!isolate || !ScopeIs(isolate, scope) || !context ||
      context->iso != isolate || !out_status || !out_value ||
      (source_is8 != 0 && source_is8 != 1) ||
      (!source_data && source_length) || (!selected && selected_length) ||
      selected_length > 5) {
    SetErr("invalid passive command-line evaluation argument");
    return kErrBadArg;
  }
  try {
    Gov8IsolateScope enter(isolate);
    auto local_context = context->ctx->Get(isolate);
    auto last = last_wire ? FromWire<v8::Value>(last_wire)
                          : v8::Local<v8::Value>();
    Client client(isolate, local_context, last);
    Channel channel;
    // Use the pinned C bridge for ownership-bearing std::unique_ptr APIs.
    // Windows V8 uses libc++ while this shim uses the MSVC standard library.
    std::unique_ptr<v8_inspector::V8Inspector,
                    decltype(&v8_inspector__V8Inspector__DELETE)> inspector(
        v8_inspector__V8Inspector__create(isolate, &client),
        v8_inspector__V8Inspector__DELETE);
    v8_inspector__V8Inspector__contextCreated(
        inspector.get(), local_context.operator->(), 1,
        v8_inspector::StringView(), v8_inspector::StringView());
    std::unique_ptr<v8_inspector::V8InspectorSession,
                    decltype(&v8_inspector__V8InspectorSession__DELETE)> session(
        v8_inspector__V8Inspector__connect(
            inspector.get(), 1, &channel, v8_inspector::StringView(),
            v8_inspector::V8Inspector::ClientTrustLevel::kFullyTrusted),
        v8_inspector__V8InspectorSession__DELETE);
    for (size_t index = 0; index < selected_length; ++index) {
      auto value = selected[index] ? FromWire<v8::Value>(selected[index])
                                   : v8::Undefined(isolate).As<v8::Value>();
      v8_inspector__V8InspectorSession__addInspectedObject(
          session.get(), new Inspectable(value));
    }
    auto source = source_is8
        ? v8_inspector::StringView(static_cast<const uint8_t*>(source_data),
                                   source_length)
        : v8_inspector::StringView(static_cast<const uint16_t*>(source_data),
                                   source_length);
    auto result = session->evaluate(local_context, source, true);
    *out_status = static_cast<int32_t>(result.type);
    if (!result.value.IsEmpty()) *out_value = ToWire(result.value);
    session.reset();
    v8_inspector__V8Inspector__contextDestroyed(
        inspector.get(), local_context.operator->());
    return kOk;
  } catch (...) {
    SetErr("C++ exception in passive command-line evaluation");
    return kErrCpp;
  }
}
