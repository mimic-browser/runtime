(async () => {
  const result = {};
  const source = `
    const descriptor = Object.getOwnPropertyDescriptor(self, 'name');
    let invalidReceiver = null;
    if (descriptor && descriptor.get) {
      try {
        descriptor.get.call({});
      } catch (error) {
        invalidReceiver = [error.name, error.message];
      }
    }
    const initial = self.name;
    self.name = 'local replacement';
    const replacement = Object.getOwnPropertyDescriptor(self, 'name');
    postMessage({
      initial,
      units: Array.from({ length: initial.length }, (_, index) => initial.charCodeAt(index)),
      own: Object.hasOwn(self, 'name'),
      descriptor: descriptor && {
        enumerable: descriptor.enumerable,
        configurable: descriptor.configurable,
        getter: typeof descriptor.get,
        setter: typeof descriptor.set,
      },
      invalidReceiver,
      replaced: self.name,
      replacement: replacement && {
        value: replacement.value,
        writable: replacement.writable,
        enumerable: replacement.enumerable,
        configurable: replacement.configurable,
      },
    });
  `;
  const url = URL.createObjectURL(new Blob([source], { type: 'text/javascript' }));
  try {
    const names = {
      default: undefined,
      empty: '',
      zero: 0,
      false: false,
      null: null,
      unicode: 'Рабочий 😀',
      surrogate: '\ud800A\udfff',
    };
    for (const [label, name] of Object.entries(names)) {
      result['name.' + label] = await new Promise((resolve, reject) => {
        const worker = new Worker(url, { name });
        worker.onmessage = (event) => {
          worker.terminate();
          resolve(event.data);
        };
        worker.onerror = (event) => {
          worker.terminate();
          reject(new Error(event.message));
        };
      });
    }

    const construct = (label, scriptURL, options) => {
      try {
        const worker = new Worker(scriptURL, options);
        worker.terminate();
        result[label] = 'accepted';
      } catch (error) {
        result[label] = [error.name, error.message];
      }
    };
    for (const [label, options] of Object.entries({
      null: null,
      undefined: undefined,
      boolean: false,
      number: 0,
      string: 'classic',
      function: () => {},
      symbol: Symbol('options'),
      emptyType: { type: '' },
      nullType: { type: null },
      falseType: { type: false },
      invalidType: { type: 'invalid' },
      invalidCredentials: { credentials: 'invalid' },
      symbolName: { name: Symbol('name') },
    }))
      construct('options.' + label, url, options);

    const reads = [];
    const options = {
      get credentials() {
        reads.push('credentials');
        return 'same-origin';
      },
      get name() {
        reads.push('name');
        return {
          toString() {
            reads.push('name-string');
            return 'named';
          },
        };
      },
      get type() {
        reads.push('type');
        return 'classic';
      },
    };
    construct(
      'conversion',
      {
        toString() {
          reads.push('url');
          return url;
        },
      },
      options,
    );
    result.conversionOrder = reads;

    const abruptReads = [];
    const sentinel = {};
    try {
      new Worker(url, {
        get credentials() {
          abruptReads.push('credentials');
          throw sentinel;
        },
        get name() {
          abruptReads.push('name');
          return 'unused';
        },
        get type() {
          abruptReads.push('type');
          return 'classic';
        },
      });
      result.abruptIdentity = false;
    } catch (error) {
      result.abruptIdentity = error === sentinel;
    }
    result.abruptReads = abruptReads;
    return result;
  } finally {
    URL.revokeObjectURL(url);
  }
})()
