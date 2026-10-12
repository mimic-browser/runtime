(() => {
  const record = (name) => {
    effects.push(name);
    queueMicrotask(() => effects.push('job:' + name));
  };
  const traps = {
    get() {
      record('get');
      throw new Error('author get');
    },
    getPrototypeOf() {
      record('prototype');
      throw new Error('author prototype');
    },
    ownKeys() {
      record('keys');
      throw new Error('author keys');
    },
    getOwnPropertyDescriptor() {
      record('descriptor');
      throw new Error('author descriptor');
    },
  };
  globalThis.runtimeInspectionProbes = {
    proxy() {
      return new Proxy({}, traps);
    },
    callableProxy() {
      return new Proxy(function () {}, traps);
    },
    revokedProxy() {
      const value = Proxy.revocable({}, traps);
      value.revoke();
      return value.proxy;
    },
    accessor() {
      return {
        get value() {
          record('getter');
          throw new Error('author getter');
        },
      };
    },
    arrayAccessor() {
      return Object.defineProperty([1], '0', {
        get() {
          record('array getter');
          throw new Error('author array getter');
        },
      });
    },
    nestedAccessor() {
      return { nested: runtimeInspectionProbes.accessor() };
    },
    prototypeProxy() {
      return Object.create(new Proxy({}, traps));
    },
    constructorProxy() {
      return Object.create({ constructor: new Proxy(function () {}, traps) });
    },
    inheritedArrayAccessor() {
      const value = new Array(1);
      const parent = Object.create(Array.prototype);
      Object.defineProperty(parent, '0', {
        get() {
          record('inherited array getter');
          return 1;
        },
      });
      Object.setPrototypeOf(value, parent);
      return value;
    },
    promise() {
      const value = Promise.resolve(42);
      for (const key of ['then', 'constructor', Symbol.toStringTag]) {
        Object.defineProperty(value, key, {
          get() {
            record('promise.' + String(key));
            throw new Error('promise getter');
          },
        });
      }
      return value;
    },
    poisonInfrastructure() {
      for (const key of ['value', 'objectGroup']) {
        const descriptor = Object.create(null);
        descriptor.configurable = true;
        descriptor.get = () => {
          record('prototype.' + key);
          throw new Error('prototype getter');
        };
        Object.defineProperty(Object.prototype, key, descriptor);
      }
      const descriptor = Object.create(null);
      descriptor.configurable = true;
      descriptor.get = () => {
        record('iterator');
        throw new Error('author iterator');
      };
      Object.defineProperty(Array.prototype, Symbol.iterator, descriptor);
      return new Error('passive descriptor');
    },
    plainException() {
      return {
        toString() {
          record('toString');
          return 'author';
        },
        get name() {
          record('name');
          return 'Error';
        },
        get message() {
          record('message');
          return 'author';
        },
        get stack() {
          record('stack');
          return 'author';
        },
      };
    },
    error() {
      const value = new Error('author');
      value.toString = () => {
        record('toString');
        return 'author';
      };
      for (const key of ['name', 'message', 'stack'])
        Object.defineProperty(value, key, {
          get() {
            record(key);
            return 'author';
          },
        });
      return value;
    },
    lazyError() {
      Error.prepareStackTrace = () => {
        record('prepareStackTrace');
        return 'author';
      };
      const value = new Error('author');
      for (const key of ['name', 'message'])
        Object.defineProperty(value, key, {
          get() {
            record(key);
            return 'author';
          },
        });
      return value;
    },
  };
  return true;
})();
