(async () => {
  const observations = {};
  const order = [];
  for (const method of [
    'log',
    'debug',
    'info',
    'warn',
    'error',
    'dir',
    'dirxml',
    'table',
    'trace',
    'group',
    'groupCollapsed',
    'groupEnd',
  ]) {
    const reads = [];
    const record = (name) => {
      reads.push(name);
      queueMicrotask(() => order.push('hook:' + name));
    };
    const error = new Error('passive');
    error.toString = () => {
      record('text');
      return 'custom';
    };
    for (const key of ['name', 'message']) {
      Object.defineProperty(error, key, {
        get() {
          record(key);
          return key;
        },
      });
    }
    console[method](error);
    const authorStack = new Error('author');
    authorStack.toString = () => 'custom';
    Object.defineProperty(authorStack, 'stack', {
      get() {
        record('stack');
        throw new Error('stack getter');
      },
    });
    console[method](authorStack);
    const objectStack = new Error('object');
    objectStack.toString = () => 'custom';
    objectStack.stack = {
      toString() {
        record('stack.toString');
        throw new Error('stack coercion');
      },
    };
    console[method](objectStack);
    const object = {
      get toString() {
        record('object.toString');
        throw new Error('object getter');
      },
      get secret() {
        record('secret');
        return 1;
      },
    };
    console[method](object);
    const handler = {
      get() {
        record('proxy.get');
        throw new Error('proxy get');
      },
      getPrototypeOf() {
        record('proxy.getPrototypeOf');
        throw new Error('proxy prototype');
      },
      ownKeys() {
        record('proxy.ownKeys');
        throw new Error('proxy keys');
      },
      getOwnPropertyDescriptor() {
        record('proxy.descriptor');
        throw new Error('proxy descriptor');
      },
    };
    console[method](new Proxy({}, handler));
    console[method](new Proxy(function () {}, handler));
    const revoked = Proxy.revocable({}, handler);
    revoked.revoke();
    console[method](revoked.proxy);
    const prototypeProxy = Object.create(new Proxy({}, handler));
    console[method](prototypeProxy);
    const regexp = /passive/gi;
    regexp.toString = () => 'custom';
    for (const key of ['source', 'flags', 'global', 'ignoreCase']) {
      Object.defineProperty(regexp, key, {
        get() {
          record('regexp.' + key);
          throw new Error('regexp getter');
        },
      });
    }
    console[method](regexp);
    const element = document.createElement('div');
    for (const key of ['nodeType', 'nodeName', 'nodeValue', 'id', 'className', 'host']) {
      Object.defineProperty(element, key, {
        get() {
          record('node.' + key);
          throw new Error('node getter');
        },
      });
    }
    console[method](element);
    const oldPrepare = Error.prepareStackTrace;
    try {
      Error.prepareStackTrace = () => {
        record('prepareStackTrace');
        return 'custom stack';
      };
      const lazy = new Error('lazy');
      lazy.toString = () => 'custom';
      console[method](lazy);
    } finally {
      if (oldPrepare === undefined) delete Error.prepareStackTrace;
      else Error.prepareStackTrace = oldPrepare;
    }
    observations[method] = reads;
  }
  order.push('sync');
  queueMicrotask(() => {
    order.push('queueMicrotask');
    observations.commandLineMicrotask = ['$0', '$1', '$_', 'keys', 'values'].map((name) =>
      Object.hasOwn(globalThis, name),
    );
  });
  Promise.resolve().then(() => order.push('promise'));
  await Promise.resolve();
  order.push('continuation');
  return JSON.stringify({ observations, order });
})();
