const nativeFunctionNames = new WeakMap();
const nativeFunctionRecordGet = WeakMap.prototype.get.bind(nativeFunctionNames),
  nativeFunctionNameSet = WeakMap.prototype.set.bind(nativeFunctionNames),
  functionSourceApply = Reflect.apply;
const nativeFunctionNameGet = (fn) => {
  const record = nativeFunctionRecordGet(fn);
  return typeof record === 'string' ? record : record?.name;
};
const nativeFunctionSourceGet = (fn) => {
  const record = nativeFunctionRecordGet(fn);
  if (record === undefined) return undefined;
  if (typeof record === 'string') return 'function ' + record + '() { [native code] }';
  return record.source;
};
const markForeignFunctionSource = (fn, source) =>
  nativeFunctionNameSet(fn, { __proto__: null, source });
const engineFunctionToString = Function.prototype.toString;
// Only explicitly marked platform functions override the engine's source.
// Reading callable properties here invokes user getters/proxy traps, and
// inspecting source text also misclassifies ordinary user function bodies.
// Keep binding names and imported immutable intrinsic sources in one registry.
// Source observation never reads public callable properties. The concise
// fallback has ordinary prototype-cycle behavior; V8 replaces it after restore
// with an engine-owned, nonconstructible callback.
Function.prototype.toString = {
  toString() {
    const source = nativeFunctionSourceGet(this);
    return source === undefined ? functionSourceApply(engineFunctionToString, this, []) : source;
  },
}.toString;
const nativeFunctionSourceState = [nativeFunctionSourceGet, engineFunctionToString];
const markNative = (fn, name, prefix = '') => {
  if (typeof fn === 'function')
    nativeFunctionNameSet(fn, prefix ? prefix + String(name) : String(name));
};
markNative(Function.prototype.toString, 'toString');

// Public platform operations enter through an engine-owned native callable.
// The implementation remains private realm-owned JS, including its validation,
// result and thrown value. This is only used while installing platform bindings;
// arbitrary author functions are never candidates for replacement.
const nativeOperationCallables = new WeakMap();
const nativeOperationReceiver = { accept() {} }.accept;
const platformOperation = (implementation, name, length = implementation.length) => {
  if (typeof host.createReceiverDispatch !== 'function') return implementation;
  // The native factory preserves engine intrinsics by Script provenance.
  // Source text cannot distinguish them from trusted platform implementations.
  let callable = nativeOperationCallables.get(implementation);
  if (!callable) {
    callable = host.createReceiverDispatch(
      nativeOperationReceiver,
      implementation,
      implementation,
      name,
      length,
    );
    nativeOperationCallables.set(implementation, callable);
  }
  return callable;
};

// Web IDL numeric conversion uses the engine's ToNumber operation, not the
// replaceable Number constructor (which also accepts BigInt).
const webIDLNumber = (value) => +value;
