(() => {
  const slots = new WeakMap();
  const state = (receiver) => {
    const result = slots.get(receiver);
    if (!result) throw new TypeError('Illegal invocation');
    return result;
  };
  const nullableString = (value) => (value == null ? null : String(value));
  const StorageEvent = class StorageEvent extends Event {
    constructor(type, init = {}) {
      if (!arguments.length) throw new TypeError('One argument is required');
      super(type, init);
      slots.set(this, {
        key: nullableString(init.key),
        oldValue: nullableString(init.oldValue),
        newValue: nullableString(init.newValue),
        url: init.url === undefined ? '' : String(init.url),
        storageArea: init.storageArea == null ? null : init.storageArea,
      });
    }
    get key() {
      return state(this).key;
    }
    get oldValue() {
      return state(this).oldValue;
    }
    get newValue() {
      return state(this).newValue;
    }
    get url() {
      return state(this).url;
    }
    get storageArea() {
      return state(this).storageArea;
    }
  };
  Object.defineProperty(StorageEvent.prototype, Symbol.toStringTag, {
    value: 'StorageEvent',
    configurable: true,
  });
  Object.defineProperty(globalThis, 'StorageEvent', {
    value: StorageEvent,
    writable: true,
    configurable: true,
  });
  registerBootstrapCallback('installStorageNotifier', (row) => {
    const event = new StorageEvent('storage', {
      ...row,
      storageArea: row.area === 'session' ? sessionStorage : storage,
    });
    dispatchTrusted(window, event);
  });
})();
