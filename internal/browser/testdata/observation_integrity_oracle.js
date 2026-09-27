// Each integrity operation receives a fresh child realm, preserving isolation
// between the platform objects affected by freeze, seal and preventExtensions.
// This is a local semantic probe; it does not exercise a live site's server.
// prettier-ignore
(() => {
  const result = {};
  for (const operation of ['preventExtensions', 'seal', 'freeze']) {
    const frame = document.createElement('iframe');
    document.body.appendChild(frame);
    const realm = frame.contentWindow;
    const targets = {
      navigator: realm.navigator,
      screen: realm.screen,
      history: realm.history,
      crypto: realm.crypto,
      document: realm.document,
      element: realm.document.createElement('div'),
    };
    for (const [label, object] of Object.entries(targets)) {
      const key = operation + '.' + label;
      const prototype = Object.getPrototypeOf(object);
      const symbol = Symbol('probe');
      Object.defineProperty(object, 'integrityProbe', {
        value: 7, writable: true, enumerable: true, configurable: true,
      });
      Object.defineProperty(object, symbol, {
        value: 11, writable: true, configurable: true,
      });
      try {
        const returned = Object[operation](object);
        const descriptor = Object.getOwnPropertyDescriptor(object, 'integrityProbe');
        result[key] = {
          identity: returned === object,
          prototype: Object.getPrototypeOf(object) === prototype,
          extensible: Object.isExtensible(object),
          sealed: Object.isSealed(object),
          frozen: Object.isFrozen(object),
          value: object.integrityProbe,
          symbol: object[symbol],
          descriptor: {
            writable: descriptor.writable,
            enumerable: descriptor.enumerable,
            configurable: descriptor.configurable,
          },
          add: Reflect.set(object, 'integrityNewProbe', 13),
          define: Reflect.defineProperty(object, 'integrityNewProbe', { value: 13 }),
          write: Reflect.set(object, 'integrityProbe', 17),
          remove: Reflect.deleteProperty(object, 'integrityProbe'),
        };
      } catch (error) {
        result[key] = { error: { name: error.name, message: error.message } };
      }
    }
    frame.remove();
  }
  return result;
})()
