// Window and Worker expose the same environment projection, with realm-local
// wrappers and fresh frozen brand arrays. No UA fields are copied into workers.
(() => {
  const worker = typeof WorkerNavigator === 'function';
  const owner = globalThis.navigator;
  const prototype = worker ? WorkerNavigator.prototype : Navigator.prototype;
  const secure = worker ? host.isSecureContext() : host.documentSecurity().secureContext;
  if (!secure || !globalThis.NavigatorUAData) return;
  const objects = new WeakSet();
  const check = (value) => {
    if (!objects.has(value)) throw new TypeError('Illegal invocation');
    return host.navigator();
  };
  const getter = (prototype, name, read) => {
    markNative(read, name, 'get ');
    Object.defineProperty(prototype, name, { get: read, enumerable: true, configurable: true });
  };
  const method = (name, fn) => {
    markNative(fn, name);
    Object.defineProperty(NavigatorUAData.prototype, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const brands = (data) =>
    Object.freeze(data.uaBrands.map(({ brand, version }) => ({ brand, version })));
  getter(prototype, 'userAgentData', function () {
    if (this !== owner) throw new TypeError('Illegal invocation');
    const object = Object.create(NavigatorUAData.prototype);
    objects.add(object);
    return object;
  });
  getter(NavigatorUAData.prototype, 'brands', function () {
    return brands(check(this));
  });
  getter(NavigatorUAData.prototype, 'mobile', function () {
    return check(this).uaMobile;
  });
  getter(NavigatorUAData.prototype, 'platform', function () {
    return check(this).uaPlatform;
  });
  const low = (data) => ({
    brands: brands(data),
    mobile: data.uaMobile,
    platform: data.uaPlatform,
  });
  method('toJSON', function toJSON() {
    return low(check(this));
  });
  method('getHighEntropyValues', function getHighEntropyValues(hints) {
    try {
      const data = check(this);
      if (
        hints == null ||
        typeof hints !== 'object' ||
        typeof hints[Symbol.iterator] !== 'function'
      )
        throw new TypeError('Expected sequence');
      const out = low(data);
      for (const item of hints) {
        if (typeof item === 'symbol') throw new TypeError('Cannot convert a Symbol to a string');
        const hint = String(item);
        if (
          [
            'architecture',
            'bitness',
            'model',
            'platformVersion',
            'uaFullVersion',
            'wow64',
          ].includes(hint)
        )
          out[hint] = data[hint];
        if (hint === 'fullVersionList')
          out.fullVersionList = data.uaFullVersionList.map(({ brand, version }) => ({
            brand,
            version,
          }));
        if (hint === 'formFactors') out.formFactors = data.formFactors.slice();
      }
      return platformPromiseResolve(out);
    } catch (error) {
      return platformPromiseReject(error);
    }
  });
})();
