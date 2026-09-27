// OPFS objects are realm-local projections of the Context's origin-owned store.
// Bytes and access locks live in Go; no host filesystem or engine object escapes.
// StorageManager is the owning interface; partial bundles may omit it.
if (globalThis.StorageManager?.prototype) {
  const slots = new WeakMap(),
    access = new WeakMap(),
    writables = new WeakMap(),
    token = {};
  const identityStringify = JSON.stringify,
    identityParse = JSON.parse;
  const messages = {
    NotFoundError:
      'A requested file or directory could not be found at the time an operation was processed.',
    TypeMismatchError: 'The path supplied exists, but was not an entry of requested type.',
    InvalidModificationError: 'The object can not be modified in this way.',
  };
  const invoke = (op, id, p, method, type) => {
    const result = host.fileSystem(op, id, p);
    if (result && result.error) {
      const name = result.error;
      let message = messages[name];
      if (!message) {
        if (name === 'InvalidStateError')
          message = ['read', 'write'].includes(method)
            ? 'The access handle was already closed'
            : 'The file was already closed';
        else if (name === 'NoModificationAllowedError') {
          message =
            method === 'createSyncAccessHandle'
              ? 'Access Handles cannot be created if there is another open Access Handle or Writable stream associated with the same file.'
              : type === 'FileSystemSyncAccessHandle'
                ? "Cannot write to access handle in 'read-only' mode"
                : 'An attempt was made to modify an object where modifications are not allowed.';
        } else message = 'The requested operation is not supported.';
        message = `Failed to execute '${method}' on '${type}': ${message}`;
      }
      throw platformDOMException(message, name);
    }
    return result;
  };
  const check = (v, type) => {
    let s = slots.get(v);
    if (!s && typeof requireRealmBinding === 'function')
      s = identityParse(
        callRealmBinding(v, requireRealmBinding(v, 'FileSystemHandle'), 'identity', []),
      );
    if (!s || (type && s.kind !== type)) throw new TypeError('Illegal invocation');
    return s;
  };
  const handle = (s) => {
    Object.setPrototypeOf(s, null);
    return new (s.kind === 'directory' ? FileSystemDirectoryHandle : FileSystemFileHandle)(
      token,
      s,
    );
  };
  const nameArg = (value, method) => {
    const name = String(value);
    if (!name || name === '.' || name === '..' || /[\\/\0]/.test(name))
      throw new TypeError(
        `Failed to execute '${method}' on 'FileSystemDirectoryHandle': Name is not allowed.`,
      );
    return name;
  };
  const integer = (v, method) => {
    const n = Number(v);
    if (!Number.isFinite(n) || n < 0 || n > Number.MAX_SAFE_INTEGER)
      throw new TypeError(
        `Failed to execute '${method}' on 'FileSystemSyncAccessHandle': Value is outside the 'unsigned long long' value range.`,
      );
    return Math.trunc(n);
  };
  class FileSystemHandle {
    constructor(t, s) {
      if (t !== token) throw new TypeError('Illegal constructor');
      slots.set(this, s);
      if (typeof registerRealmBinding === 'function')
        registerRealmBinding(this, 'FileSystemHandle', { identity: () => identityStringify(s) });
    }
    get kind() {
      return check(this).kind;
    }
    get name() {
      return check(this).name;
    }
    async isSameEntry(other) {
      const s = check(this),
        o = check(other);
      return s.store === o.store && s.id === o.id;
    }
    async queryPermission() {
      check(this);
      return 'granted';
    }
    async requestPermission() {
      check(this);
      return 'granted';
    }
  }
  class FileSystemDirectoryHandle extends FileSystemHandle {
    async getFileHandle(name, options = {}) {
      const s = check(this, 'directory');
      name = nameArg(name, 'getFileHandle');
      return handle(
        invoke(
          'child',
          s.id,
          { name, kind: 'file', create: !!options?.create },
          'getFileHandle',
          'FileSystemDirectoryHandle',
        ),
      );
    }
    async getDirectoryHandle(name, options = {}) {
      const s = check(this, 'directory');
      name = nameArg(name, 'getDirectoryHandle');
      return handle(
        invoke(
          'child',
          s.id,
          { name, kind: 'directory', create: !!options?.create },
          'getDirectoryHandle',
          'FileSystemDirectoryHandle',
        ),
      );
    }
    async removeEntry(name, options = {}) {
      const s = check(this, 'directory');
      name = nameArg(name, 'removeEntry');
      invoke(
        'remove',
        s.id,
        { name, recursive: !!options?.recursive },
        'removeEntry',
        'FileSystemDirectoryHandle',
      );
    }
    async resolve(possibleDescendant) {
      const s = check(this, 'directory'),
        other = check(possibleDescendant);
      return invoke(
        'resolve',
        s.id,
        { target: other.id, store: other.store },
        'resolve',
        'FileSystemDirectoryHandle',
      );
    }
    entries() {
      return directoryIterator(check(this, 'directory'), 'entries');
    }
    keys() {
      return directoryIterator(check(this, 'directory'), 'keys');
    }
    values() {
      return directoryIterator(check(this, 'directory'), 'values');
    }
  }
  const directoryIterator = (s, kind) => {
    let rows,
      index = 0;
    return {
      async next() {
        if (!rows) rows = invoke('entries', s.id, {}, kind, 'FileSystemDirectoryHandle');
        if (index >= rows.length) return { done: true, value: undefined };
        const child = rows[index++];
        return {
          done: false,
          value:
            kind === 'keys'
              ? child.name
              : kind === 'values'
                ? handle(child)
                : [child.name, handle(child)],
        };
      },
      [Symbol.asyncIterator]() {
        return this;
      },
    };
  };
  Object.defineProperty(FileSystemDirectoryHandle.prototype, Symbol.asyncIterator, {
    value: FileSystemDirectoryHandle.prototype.entries,
    writable: true,
    configurable: true,
  });
  const mime = (name) =>
    ({
      txt: 'text/plain',
      json: 'application/json',
      html: 'text/html',
      htm: 'text/html',
      js: 'text/javascript',
      css: 'text/css',
      png: 'image/png',
      jpg: 'image/jpeg',
      jpeg: 'image/jpeg',
      pdf: 'application/pdf',
    })[name.split('.').pop().toLowerCase()] || '';
  class FileSystemFileHandle extends FileSystemHandle {
    async createWritable(options = {}) {
      const s = check(this, 'file'),
        mode = options?.mode === undefined ? 'siloed' : String(options.mode);
      if (!['siloed', 'exclusive'].includes(mode)) throw new TypeError('Invalid writable mode');
      const id = invoke(
        'openWritable',
        s.id,
        { mode, keep: !!options?.keepExistingData },
        'createWritable',
        'FileSystemFileHandle',
      );
      return new FileSystemWritableFileStream(token, { id, mode, cursor: 0 });
    }
    async getFile() {
      const s = check(this, 'file'),
        f = invoke('file', s.id, {}, 'getFile', 'FileSystemFileHandle');
      return new File([new Uint8Array(f.data)], s.name, {
        type: mime(s.name),
        lastModified: f.modified,
      });
    }
  }
  class FileSystemWritableFileStream extends globalThis.WritableStream {
    constructor(t, s) {
      if (t !== token) throw new TypeError('Illegal constructor');
      super({
        write: (chunk) => writeChunk(s, chunk),
        close: () => invoke('close', s.id, {}, 'close', 'FileSystemWritableFileStream'),
        abort: () => invoke('abort', s.id, {}, 'abort', 'FileSystemWritableFileStream'),
      });
      writables.set(this, s);
    }
    get mode() {
      const s = writables.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.mode;
    }
    async write(data) {
      const writer = this.getWriter();
      try {
        await writer.write(data);
      } finally {
        writer.releaseLock();
      }
    }
    async seek(position) {
      return this.write({ type: 'seek', position });
    }
    async truncate(size) {
      return this.write({ type: 'truncate', size });
    }
  }
  const writeChunk = async (s, chunk) => {
    if (
      chunk &&
      typeof chunk === 'object' &&
      !ArrayBuffer.isView(chunk) &&
      !(chunk instanceof ArrayBuffer) &&
      !(chunk instanceof Blob)
    ) {
      const type = String(chunk.type);
      if (type === 'seek') {
        s.cursor = integer(chunk.position, 'seek');
        return;
      }
      if (type === 'truncate') {
        const size = integer(chunk.size, 'truncate');
        invoke('truncate', s.id, { size }, 'truncate', 'FileSystemWritableFileStream');
        s.cursor = Math.min(s.cursor, size);
        return;
      }
      if (type !== 'write') throw new TypeError('Invalid write command');
      if (chunk.position !== undefined) s.cursor = integer(chunk.position, 'write');
      chunk = chunk.data;
    }
    let bytes;
    if (typeof chunk === 'string') bytes = new TextEncoder().encode(chunk);
    else if (chunk instanceof Blob) bytes = new Uint8Array(await chunk.arrayBuffer());
    else bytes = view(chunk);
    const n = invoke(
      'write',
      s.id,
      { at: s.cursor, data: Array.from(bytes) },
      'write',
      'FileSystemWritableFileStream',
    );
    s.cursor += n;
  };
  class FileSystemSyncAccessHandle {
    constructor(t, s) {
      if (t !== token) throw new TypeError('Illegal constructor');
      access.set(this, s);
    }
    get mode() {
      return accessCheck(this).mode;
    }
    close() {
      const s = accessCheck(this);
      invoke('close', s.id, {}, 'close', 'FileSystemSyncAccessHandle');
    }
    flush() {
      const s = accessCheck(this);
      invoke('flush', s.id, {}, 'flush', 'FileSystemSyncAccessHandle');
    }
    getSize() {
      const s = accessCheck(this);
      return invoke('size', s.id, {}, 'getSize', 'FileSystemSyncAccessHandle');
    }
    truncate(size) {
      const s = accessCheck(this);
      size = integer(size, 'truncate');
      invoke('truncate', s.id, { size }, 'truncate', 'FileSystemSyncAccessHandle');
      s.cursor = Math.min(s.cursor, size);
    }
    read(buffer, options = {}) {
      const s = accessCheck(this),
        bytes = view(buffer),
        at = options?.at === undefined ? s.cursor : integer(options.at, 'read'),
        data = invoke(
          'read',
          s.id,
          { at, size: bytes.byteLength },
          'read',
          'FileSystemSyncAccessHandle',
        );
      bytes.set(data);
      s.cursor = at + data.length;
      return data.length;
    }
    write(buffer, options = {}) {
      const s = accessCheck(this),
        bytes = view(buffer),
        at = options?.at === undefined ? s.cursor : integer(options.at, 'write'),
        n = invoke(
          'write',
          s.id,
          { at, data: Array.from(bytes) },
          'write',
          'FileSystemSyncAccessHandle',
        );
      s.cursor = at + n;
      return n;
    }
  }
  const accessCheck = (v) => {
    const s = access.get(v);
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const view = (v) => {
    if (ArrayBuffer.isView(v)) return new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
    if (
      Object.prototype.toString.call(v) === '[object ArrayBuffer]' ||
      Object.prototype.toString.call(v) === '[object SharedArrayBuffer]'
    )
      return new Uint8Array(v);
    throw new TypeError('The provided value is not of type ArrayBuffer or ArrayBufferView.');
  };
  if (typeof document === 'undefined')
    Object.defineProperty(FileSystemFileHandle.prototype, 'createSyncAccessHandle', {
      value: async function createSyncAccessHandle(options = {}) {
        const s = check(this, 'file'),
          mode = options?.mode === undefined ? 'readwrite' : String(options.mode);
        if (!['readwrite', 'read-only', 'readwrite-unsafe'].includes(mode))
          throw new TypeError(
            `Failed to execute 'createSyncAccessHandle' on 'FileSystemFileHandle': Failed to read the 'mode' property from 'FileSystemCreateSyncAccessHandleOptions': The provided value '${mode}' is not a valid enum value of type FileSystemSyncAccessHandleMode.`,
          );
        const id = invoke('open', s.id, { mode }, 'createSyncAccessHandle', 'FileSystemFileHandle');
        return new FileSystemSyncAccessHandle(token, { id, mode, cursor: 0 });
      },
      writable: true,
      enumerable: true,
      configurable: true,
    });
  (typeof clonePlatformCodecs !== 'undefined'
    ? clonePlatformCodecs
    : globalThis.__mimicClonePlatforms
  ).push({
    tag: 'opfs-handle',
    encode(value) {
      if (access.has(value) || writables.has(value))
        throw platformDOMException('The platform object could not be cloned.', 'DataCloneError');
      return slots.get(value);
    },
    decode(data) {
      return handle(invoke('restore', data.id, { store: data.store }, 'structuredClone', 'Window'));
    },
  });
  if (globalThis.isSecureContext) {
    const constructors = {
      FileSystemHandle,
      FileSystemDirectoryHandle,
      FileSystemFileHandle,
      FileSystemWritableFileStream,
    };
    if (typeof document === 'undefined')
      constructors.FileSystemSyncAccessHandle = FileSystemSyncAccessHandle;
    for (const [name, C] of Object.entries(constructors)) {
      Object.defineProperty(C.prototype, Symbol.toStringTag, { value: name, configurable: true });
      for (const key of Reflect.ownKeys(C.prototype)) {
        if (key === 'constructor' || typeof key !== 'string') continue;
        const d = Object.getOwnPropertyDescriptor(C.prototype, key);
        d.enumerable = true;
        if (d.value) {
          const fn = d.value;
          d.value = {
            [key](...a) {
              return Reflect.apply(fn, this, a);
            },
          }[key];
          Object.defineProperty(d.value, 'length', { value: fn.length, configurable: true });
          markNative(d.value, key);
        }
        for (const kind of ['get', 'set']) if (d[kind]) markNative(d[kind], key, kind + ' ');
        Object.defineProperty(C.prototype, key, d);
      }
      Object.defineProperty(C, 'length', { value: 0, configurable: true });
      if (C === FileSystemDirectoryHandle)
        Object.defineProperty(C.prototype, Symbol.asyncIterator, {
          value: C.prototype.entries,
          writable: true,
          configurable: true,
        });
      markNative(C, name);
      Object.defineProperty(globalThis, name, { value: C, writable: true, configurable: true });
    }
    let manager = globalThis.navigator.storage;
    if (!manager) {
      manager = Object.create(globalThis.StorageManager.prototype);
      Object.defineProperty(Object.getPrototypeOf(globalThis.navigator), 'storage', {
        get() {
          return manager;
        },
        enumerable: true,
        configurable: true,
      });
    }
    const getDirectory = {
      getDirectory() {
        if (this !== manager) return platformPromiseReject(new TypeError('Illegal invocation'));
        return platformPromiseResolve(
          handle(invoke('root', 0, {}, 'getDirectory', 'StorageManager')),
        );
      },
    }.getDirectory;
    markNative(getDirectory, 'getDirectory');
    Object.defineProperty(globalThis.StorageManager.prototype, 'getDirectory', {
      value: getDirectory,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
}
