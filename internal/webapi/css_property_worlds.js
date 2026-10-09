// A Document's registration authority belongs to its main realm. Isolated
// worlds register through it and observe the same computed-style projection.
(() => {
  const register = CSS.registerProperty;
  registerBootstrapCallback('installCSSPropertyRegistration', (encoded) => {
    try {
      Reflect.apply(register, CSS, [JSON.parse(encoded)]);
      return '';
    } catch (error) {
      return JSON.stringify({ name: error.name, message: error.message });
    }
  });
  Object.defineProperty(CSS, 'registerProperty', {
    value: function registerProperty(descriptor) {
      if (!styleObservationIsolated) return Reflect.apply(register, CSS, [descriptor]);
      if (!descriptor || descriptor.name === undefined || descriptor.inherits === undefined)
        throw new TypeError('Name and inherits are required');
      const record = {
        name: String(descriptor.name),
        syntax: descriptor.syntax === undefined ? '*' : String(descriptor.syntax),
        inherits: Boolean(descriptor.inherits),
      };
      if (descriptor.initialValue !== undefined)
        record.initialValue = String(descriptor.initialValue);
      const failure = host.registerCSSProperty(JSON.stringify(record));
      if (failure) {
        const error = JSON.parse(failure);
        if (error.name === 'TypeError') throw new TypeError(error.message);
        throw platformDOMException(error.message, error.name);
      }
    },
    writable: true,
    enumerable: true,
    configurable: true,
  });
})();
