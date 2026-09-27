// StorageManager belongs to the worker navigator; quota and permission state
// come from the same Context/origin store used by Window.
(() => {
  if (!host.isSecureContext() || !globalThis.StorageManager) return;
  const owner = navigator,
    manager = Object.create(StorageManager.prototype);
  const get = function () {
    if (this !== owner) throw new TypeError('Illegal invocation');
    return manager;
  };
  markNative(get, 'storage', 'get ');
  Object.defineProperty(WorkerNavigator.prototype, 'storage', {
    get,
    enumerable: true,
    configurable: true,
  });
  for (const name of ['estimate', 'persisted']) {
    const value = {
      [name]() {
        if (this !== manager) return platformPromiseReject(new TypeError('Illegal invocation'));
        const state = host.workerStorageState();
        if (state.opaque)
          return platformPromiseReject(
            new TypeError('Storage is unavailable for an opaque origin'),
          );
        return platformPromiseResolve(name === 'persisted' ? state.persisted : state.estimate);
      },
    }[name];
    markNative(value, name);
    Object.defineProperty(StorageManager.prototype, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
})();
