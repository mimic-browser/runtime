// Blink supplies the streaming WebAssembly operations around V8's core API.
// Install them after Response exists in each realm. Keep their own-property
// position ahead of V8's JSPI additions, as in Chrome 152.
const installWebAssemblyStreaming = () => {
  const wasm = globalThis.WebAssembly;
  if (typeof wasm !== 'object' || typeof globalThis.Response !== 'function') return;

  const readModuleBytes = async (source, operation) => {
    const response = await source;
    if (!(response instanceof globalThis.Response)) {
      throw new TypeError(
        `Failed to execute '${operation}' on 'WebAssembly': An argument must be provided, which must be a Response or Promise<Response> object`,
      );
    }
    if (response.headers.get('content-type')?.toLowerCase() !== 'application/wasm') {
      throw new TypeError(
        `Failed to execute '${operation}' on 'WebAssembly': Incorrect response MIME type. Expected 'application/wasm'.`,
      );
    }
    return response.arrayBuffer();
  };

  const compileStreaming = (source) =>
    readModuleBytes(source, 'compile').then((bytes) => wasm.compile(bytes));
  const instantiateStreaming = (source, imports = undefined) =>
    readModuleBytes(source, 'instantiate').then((bytes) => wasm.instantiate(bytes, imports));
  markNative(compileStreaming, 'compileStreaming');
  markNative(instantiateStreaming, 'instantiateStreaming');

  const laterNames = ['Suspending', 'promising', 'SuspendError'];
  const later = laterNames.map((name) => [name, Object.getOwnPropertyDescriptor(wasm, name)]);
  for (const [name, descriptor] of later) {
    if (descriptor?.configurable) delete wasm[name];
  }
  for (const [name, value] of [
    ['compileStreaming', compileStreaming],
    ['instantiateStreaming', instantiateStreaming],
  ]) {
    Object.defineProperty(wasm, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  for (const [name, descriptor] of later) {
    if (descriptor?.configurable) Object.defineProperty(wasm, name, descriptor);
  }
};
installWebAssemblyStreaming();
if (typeof bootstrapRestoreHooks !== 'undefined')
  bootstrapRestoreHooks.push(installWebAssemblyStreaming);
