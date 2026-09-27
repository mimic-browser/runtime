// The Context host owns committed records and transaction arbitration. These
// objects are realm projections; only immutable graph encodings cross the host.
if (typeof IDBFactory === 'function' && host.indexedDB) {
  const prototypes = new Map(
    [
      'IDBFactory',
      'IDBRequest',
      'IDBOpenDBRequest',
      'IDBDatabase',
      'IDBTransaction',
      'IDBObjectStore',
      'IDBIndex',
      'IDBKeyRange',
      'IDBCursor',
      'IDBCursorWithValue',
    ].map((n) => [n, globalThis[n].prototype]),
  );
  const slots = new WeakMap(),
    pending = new Map(),
    operations = new Map();
  let sequence = 0;
  const operationKey = (type, name, kind) => type + ':' + kind + ':' + name;
  const remote = (receiver, type, name, kind, args = []) =>
    callRealmBinding(receiver, requireRealmBinding(receiver, 'IndexedDB'), 'invoke', [
      type,
      name,
      kind,
      args,
    ]);
  const fail = (name) => {
      throw platformDOMException(name, name);
    },
    need = (args, n) => {
      if (args.length < n) throw new TypeError('Not enough arguments');
    };
  const get = (o, type) => {
    const s = slots.get(o);
    if (!s || s.type !== type) throw new TypeError('Illegal invocation');
    return s;
  };
  const put = (o, type, s) => {
    slots.set(o, { ...s, type });
    registerRealmBinding(o, 'IndexedDB', {
      invoke: (expected, name, kind, args) => {
        const fn = operations.get(operationKey(expected, name, kind));
        if (!fn) throw new TypeError('Illegal invocation');
        return fn.apply(o, args);
      },
    });
    return o;
  };
  const make = (type, s) => put(Object.create(prototypes.get(type)), type, s);
  const method = (type, name, n, fn) => {
    const f = {
      [name](...args) {
        if (!slots.has(this)) return remote(this, type, name, 'method', args);
        get(this, type);
        need(args, n);
        return fn.apply(this, args);
      },
    }[name];
    operations.set(operationKey(type, name, 'method'), f);
    Object.defineProperty(f, 'length', { value: n });
    markNative(f, name);
    Object.defineProperty(globalThis[type].prototype, name, {
      value: f,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const attr = (type, name, read, write) => {
    const d = {
      get() {
        if (!slots.has(this)) return remote(this, type, name, 'get');
        return read(get(this, type), this);
      },
      enumerable: true,
      configurable: true,
    };
    if (write)
      d.set = function (v) {
        if (!slots.has(this)) return remote(this, type, name, 'set', [v]);
        write(get(this, type), v, this);
      };
    operations.set(operationKey(type, name, 'get'), d.get);
    if (d.set) operations.set(operationKey(type, name, 'set'), d.set);
    Object.defineProperty(globalThis[type].prototype, name, d);
  };
  const handler = (type, event) =>
    attr(
      type,
      'on' + event,
      (_, o) => eventHandlerRecord(o, event)?.value || null,
      (_, v, o) => setEventHandlerValue(o, event, v),
    );
  const task = (fn) => host.indexedDBTask(fn),
    taskID = () => host.indexedDBTaskIdentity(),
    call = (op, name, ...args) => host.indexedDB(op, name, ...args);
  const emit = (o, type, init = {}, versions) => {
    const e = new Event(type, init);
    if (versions) {
      Object.setPrototypeOf(e, NativeVersionChange.prototype);
      versionEvents.set(e, {
        oldVersion: versions.oldVersion,
        newVersion: versions.newVersion === undefined ? versions.version : versions.newVersion,
      });
    }
    dispatchNative(o, e);
    return e;
  };
  const versionEvents = new WeakMap();
  // Version-change events share the ordinary Event slots and event path.
  const NativeVersionChange = class IDBVersionChangeEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      versionEvents.set(this, {
        oldVersion: Number(init.oldVersion) || 0,
        newVersion: init.newVersion == null ? null : Number(init.newVersion),
      });
    }
  };
  Object.defineProperty(NativeVersionChange.prototype, Symbol.toStringTag, {
    value: 'IDBVersionChangeEvent',
    configurable: true,
  });
  markNative(NativeVersionChange, 'IDBVersionChangeEvent');
  globalThis.IDBVersionChangeEvent = NativeVersionChange;
  for (const k of ['oldVersion', 'newVersion'])
    Object.defineProperty(NativeVersionChange.prototype, k, {
      get() {
        const s = versionEvents.get(this);
        if (!s) throw new TypeError('Illegal invocation');
        return s[k];
      },
      enumerable: true,
      configurable: true,
    });
  const list = (read) => createDOMStringList(read());
  // IDB ordering: numbers, dates, strings, binary keys, arrays (lexicographic).
  const key = (value) => {
    const seen = new Set(),
      convert = (v) => {
        if (typeof v === 'number' && !Number.isNaN(v)) return [0, v === 0 ? 0 : v];
        if (v instanceof Date && !Number.isNaN(v.getTime())) return [1, v.getTime()];
        if (typeof v === 'string') return [2, v];
        if (v instanceof ArrayBuffer || ArrayBuffer.isView(v)) {
          try {
            return [
              3,
              Array.from(
                v instanceof ArrayBuffer
                  ? new Uint8Array(v)
                  : new Uint8Array(v.buffer, v.byteOffset, v.byteLength),
              ),
            ];
          } catch {
            fail('DataError');
          }
        }
        if (Array.isArray(v) && !seen.has(v)) {
          seen.add(v);
          const a = [];
          for (let i = 0; i < v.length; i++) {
            if (!(i in v)) fail('DataError');
            a.push(convert(v[i]));
          }
          seen.delete(v);
          return [4, a];
        }
        fail('DataError');
      };
    return convert(value);
  };
  const compare = (a, b) => {
    if (a[0] !== b[0]) return a[0] < b[0] ? -1 : 1;
    if (a[0] < 3) return a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : 0;
    for (let i = 0; i < Math.min(a[1].length, b[1].length); i++) {
      const d = a[0] === 4 ? compare(a[1][i], b[1][i]) : Math.sign(a[1][i] - b[1][i]);
      if (d) return d;
    }
    return Math.sign(a[1].length - b[1].length);
  };
  const keyValue = (k) =>
    k[0] === 1
      ? new Date(k[1])
      : k[0] === 3
        ? new Uint8Array(k[1]).buffer
        : k[0] === 4
          ? k[1].map(keyValue)
          : k[1];
  const range = (q) =>
    q == null
      ? null
      : slots.get(q)?.type === 'IDBKeyRange'
        ? get(q, 'IDBKeyRange')
        : { lower: key(q), upper: key(q), lowerOpen: false, upperOpen: false };
  const matches = (k, r) =>
    !r ||
    ((!r.lower || compare(k, r.lower) > 0 || (!r.lowerOpen && compare(k, r.lower) === 0)) &&
      (!r.upper || compare(k, r.upper) < 0 || (!r.upperOpen && compare(k, r.upper) === 0)));
  const newRange = (lower, upper, lowerOpen = false, upperOpen = false) => {
    if (
      lower &&
      upper &&
      (compare(lower, upper) > 0 || (compare(lower, upper) === 0 && (lowerOpen || upperOpen)))
    )
      fail('DataError');
    return make('IDBKeyRange', { lower, upper, lowerOpen, upperOpen });
  };
  for (const name of ['only', 'lowerBound', 'upperBound', 'bound']) {
    const fn = {
      [name](a, b, c, d) {
        need(arguments, name === 'bound' ? 2 : 1);
        return name === 'only'
          ? newRange(key(a), key(a))
          : name === 'lowerBound'
            ? newRange(key(a), null, !!b, false)
            : name === 'upperBound'
              ? newRange(null, key(a), false, !!b)
              : newRange(key(a), key(b), !!c, !!d);
      },
    }[name];
    markNative(fn, name);
    Object.defineProperty(IDBKeyRange, name, {
      value: fn,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  }
  for (const n of ['lower', 'upper'])
    attr('IDBKeyRange', n, (s) => (s[n] ? keyValue(s[n]) : undefined));
  for (const n of ['lowerOpen', 'upperOpen']) attr('IDBKeyRange', n, (s) => s[n]);
  method('IDBKeyRange', 'includes', 1, function (v) {
    return matches(key(v), get(this, 'IDBKeyRange'));
  });
  // Graph serialization stores cycles, shared references, sparse arrays, bigint,
  // binary views, Map/Set, dates, regexp and platform Blob/File values intact.
  const encode = (value) => {
    const seen = new Map(),
      nodes = [],
      foreign = new Map();
    const visit = (v) => {
      const reference = referenceGet(v);
      if (reference) {
        if (!foreign.has(v)) {
          const reply = host.indexedDBCloneReference(
            reference.frame,
            reference.handle,
            reference.realm,
          );
          if (!reply[0]) throw platformDOMException(reply[2], reply[1]);
          foreign.set(v, decode(reply[1]));
        }
        v = foreign.get(v);
      }
      if (v === undefined) return ['u'];
      if (typeof v === 'number')
        return [
          'n',
          Number.isNaN(v)
            ? 'NaN'
            : v === Infinity
              ? 'Infinity'
              : v === -Infinity
                ? '-Infinity'
                : Object.is(v, -0)
                  ? '-0'
                  : v,
        ];
      if (typeof v === 'bigint') return ['b', String(v)];
      if (v === null || typeof v === 'string' || typeof v === 'boolean') return ['p', v];
      if (typeof v !== 'object') fail('DataCloneError');
      if (host.historyCloneIsProxy?.(v)) fail('DataCloneError');
      if (seen.has(v)) return ['r', seen.get(v)];
      const id = nodes.length;
      seen.set(v, id);
      nodes.push(null);
      let row;
      const platform = cloneCodec.platformEncode(v);
      if (platform !== undefined) row = ['Platform', platform];
      else if (blobSlots.has(v)) {
        const b = blobSlots.get(v),
          f = fileSlots.get(v);
        row = ['Blob', Array.from(b.bytes), b.type, f || null];
      } else if (Array.isArray(v)) {
        row = ['Array', v.length, Object.keys(v).map((k) => [k, visit(v[k])])];
      } else if (v instanceof Number || v instanceof String || v instanceof Boolean)
        row = ['Box', typeof v.valueOf(), visit(v.valueOf())];
      else if (Object.prototype.toString.call(v) === '[object BigInt]')
        row = ['Box', 'bigint', visit(v.valueOf())];
      else if (v instanceof Date) row = ['Date', visit(v.getTime())];
      else if (v instanceof RegExp) row = ['RegExp', v.source, v.flags];
      else if (v instanceof ArrayBuffer) {
        try {
          row = ['ArrayBuffer', Array.from(new Uint8Array(v))];
        } catch {
          fail('DataCloneError');
        }
      } else if (ArrayBuffer.isView(v))
        row = [
          'View',
          v.constructor.name,
          visit(v.buffer),
          v.byteOffset,
          v instanceof DataView ? v.byteLength : v.length,
        ];
      else if (v instanceof Map) row = ['Map', Array.from(v, ([k, x]) => [visit(k), visit(x)])];
      else if (v instanceof Set) row = ['Set', Array.from(v, visit)];
      else if (v instanceof DOMException) row = ['DOMException', v.name, v.message];
      else if (v instanceof Error)
        row = ['Error', v.name, v.message, v.stack, 'cause' in v ? visit(v.cause) : null];
      else if (
        !elementData.has(v) &&
        !documentWrappers.has(v) &&
        !eventSlots.has(v) &&
        !bindingGet(v) &&
        v !== globalThis &&
        v !== document &&
        !(v instanceof Promise) &&
        !(v instanceof WeakMap) &&
        !(v instanceof WeakSet) &&
        (typeof WeakRef !== 'function' || !(v instanceof WeakRef)) &&
        (typeof FinalizationRegistry !== 'function' || !(v instanceof FinalizationRegistry)) &&
        (typeof globalThis.WebAssembly?.Module !== 'function' ||
          !(v instanceof WebAssembly.Module)) &&
        (Object.prototype.toString.call(v) === '[object Object]' ||
          Object.getPrototypeOf(v) === Object.prototype ||
          Object.getPrototypeOf(v) === null)
      )
        row = ['Object', Object.keys(v).map((k) => [k, visit(v[k])])];
      else fail('DataCloneError');
      nodes[id] = row;
      return ['r', id];
    };
    return JSON.stringify([visit(value), nodes]);
  };
  const decode = (encoded) => {
    const [root, nodes] = JSON.parse(encoded),
      seen = new Map();
    const read = (x) => {
      if (x[0] === 'u') return undefined;
      if (x[0] === 'p') return x[1];
      if (x[0] === 'n') return x[1] === '-0' ? -0 : Number(x[1]);
      if (x[0] === 'b') return BigInt(x[1]);
      const i = x[1];
      if (seen.has(i)) return seen.get(i);
      const n = nodes[i];
      let v;
      switch (n[0]) {
        case 'Platform':
          v = cloneCodec.platformDecode(n[1]);
          break;
        case 'Box':
          v = Object(read(n[2]));
          break;
        case 'DOMException':
          v = platformDOMException(n[2], n[1]);
          break;
        case 'Array':
          v = new Array(n[1]);
          break;
        case 'Object':
          v = {};
          break;
        case 'Date':
          v = new Date(read(n[1]));
          break;
        case 'RegExp':
          v = new RegExp(n[1], n[2]);
          break;
        case 'ArrayBuffer':
          v = new Uint8Array(n[1]).buffer;
          break;
        case 'View':
          v = new globalThis[n[1]](read(n[2]), n[3], n[4]);
          break;
        case 'Map':
          v = new Map();
          break;
        case 'Set':
          v = new Set();
          break;
        case 'Blob':
          v = n[3]
            ? new File([new Uint8Array(n[1])], n[3].name, {
                type: n[2],
                lastModified: n[3].lastModified,
              })
            : new Blob([new Uint8Array(n[1])], { type: n[2] });
          break;
        case 'Error':
          v = new (
            globalThis[n[1]] && globalThis[n[1]].prototype instanceof Error
              ? globalThis[n[1]]
              : Error
          )(n[2]);
          v.stack = n[3];
          break;
        default:
          fail('DataCloneError');
      }
      seen.set(i, v);
      if (n[0] === 'Array' || n[0] === 'Object')
        for (const [k, x] of n[n[0] === 'Array' ? 2 : 1])
          Object.defineProperty(v, k, {
            value: read(x),
            writable: true,
            enumerable: true,
            configurable: true,
          });
      if (n[0] === 'Map') for (const [k, x] of n[1]) v.set(read(k), read(x));
      if (n[0] === 'Set') for (const x of n[1]) v.add(read(x));
      if (n[0] === 'Error' && n[4]) v.cause = read(n[4]);
      return v;
    };
    return read(root);
  };
  const pathValue = (v, path) => {
    if (Array.isArray(path)) {
      const out = [];
      for (const p of path) {
        const x = pathValue(v, p);
        if (x === undefined) return undefined;
        out.push(x);
      }
      return out;
    }
    if (path === '') return v;
    for (const p of path.split('.')) {
      if (v == null || !(p in Object(v))) return undefined;
      v = v[p];
    }
    return v;
  };
  const validPath = (p) => host.indexedDBValidKeyPath(p);
  const parsePath = (p) => {
    p = Array.isArray(p) ? p.map(String) : String(p);
    if (Array.isArray(p) ? p.some((x) => !validPath(x)) : !validPath(p)) fail('SyntaxError');
    return p;
  };
  const inject = (v, path, k) => {
    const ps = path.split('.');
    for (let i = 0; i < ps.length - 1; i++) {
      if (v === null || typeof v !== 'object') fail('DataError');
      if (v[ps[i]] === undefined) v[ps[i]] = {};
      v = v[ps[i]];
    }
    if (v === null || typeof v !== 'object') fail('DataError');
    v[ps.at(-1)] = k;
  };
  const factory = make('IDBFactory', {});
  Object.defineProperty(globalThis, 'indexedDB', {
    get() {
      return factory;
    },
    enumerable: true,
    configurable: true,
  });
  method('IDBFactory', 'cmp', 2, function (a, b) {
    return compare(key(a), key(b));
  });
  method('IDBFactory', 'databases', 0, function () {
    return new platformPromise((resolve, reject) => {
      if (!host.hasStorageAccess()) {
        reject(platformDOMException('Access denied', 'SecurityError'));
        return;
      }
      task(() => resolve(call('databases', '')));
    });
  });
  const newRequest = (source = null, transaction = null, open = false) => {
    const r = make(open ? 'IDBOpenDBRequest' : 'IDBRequest', {
      source,
      transaction,
      result: undefined,
      error: null,
      readyState: 'pending',
    });
    if (transaction) eventParents.set(r, transaction);
    return r;
  };
  // OpenDBRequest inherits the request slots and accessors.
  const requestState = (o) => {
    const s = slots.get(o);
    if (!s || !['IDBRequest', 'IDBOpenDBRequest'].includes(s.type))
      throw new TypeError('Illegal invocation');
    return s;
  };
  for (const n of ['source', 'transaction', 'readyState', 'result', 'error'])
    Object.defineProperty(IDBRequest.prototype, n, {
      get() {
        if (!slots.has(this)) return remote(this, 'IDBRequest', n, 'get');
        const s = requestState(this);
        if ((n === 'result' || n === 'error') && s.readyState === 'pending')
          fail('InvalidStateError');
        return s[n];
      },
      enumerable: true,
      configurable: true,
    });
  for (const n of ['success', 'error'])
    Object.defineProperty(IDBRequest.prototype, 'on' + n, {
      get() {
        requestState(this);
        return eventHandlerRecord(this, n)?.value || null;
      },
      set(v) {
        requestState(this);
        setEventHandlerValue(this, n, v);
      },
      enumerable: true,
      configurable: true,
    });
  for (const n of ['blocked', 'upgradeneeded']) handler('IDBOpenDBRequest', n);
  const completeRequest = (r, result, error = null) => {
    const s = requestState(r);
    s.readyState = 'done';
    s.result = result;
    s.error = error;
    return emit(r, error ? 'error' : 'success', { bubbles: !!error, cancelable: !!error });
  };
  const open = (name, version, remove) => {
    if (!host.hasStorageAccess()) fail('SecurityError');
    name = String(name);
    if (version !== undefined) {
      version = Number(version);
      if (!Number.isFinite(version) || version < 1 || version > Number.MAX_SAFE_INTEGER)
        throw new TypeError('Invalid database version');
      version = Math.floor(version);
    }
    const r = newRequest(null, null, true),
      token = ++sequence;
    pending.set(token, { kind: 'open', request: r, name, remove });
    call(remove ? 'delete' : 'open', name, token, version || 0);
    return r;
  };
  method('IDBFactory', 'open', 1, function (name, version) {
    return open(name, version, false);
  });
  method('IDBFactory', 'deleteDatabase', 1, function (name) {
    return open(name, undefined, true);
  });
  const parseData = (s) => (s ? decode(s) : { stores: [] });
  const dbState = (o) => get(o, 'IDBDatabase'),
    txState = (o) => get(o, 'IDBTransaction');
  const schema = (s) => s.data.stores,
    storeBy = (s, name) => schema(s).find((x) => x.name === name);
  const dbObject = (row) => {
    const db = make('IDBDatabase', {
      name: row.name,
      version: row.version,
      connection: row.connection,
      token: row.token,
      data: parseData(row.data),
      transactions: new Set(),
      closed: false,
      closing: false,
      upgrade: null,
    });
    return db;
  };
  for (const n of ['name', 'version']) attr('IDBDatabase', n, (s) => s[n]);
  attr('IDBDatabase', 'objectStoreNames', (s) =>
    list(() =>
      schema(s)
        .map((x) => x.name)
        .sort(),
    ),
  );
  for (const n of ['abort', 'error', 'close', 'versionchange']) handler('IDBDatabase', n);
  const maybeClose = (db) => {
    const s = dbState(db);
    if (s.closing && !s.transactions.size && !s.closed) {
      s.closed = true;
      call('close', s.name, s.connection);
      pending.delete(s.token);
    }
  };
  method('IDBDatabase', 'close', 0, function () {
    dbState(this).closing = true;
    maybeClose(this);
  });
  const active = (tx) => {
    const s = txState(tx);
    if (s.finished || s.committing || s.activeTask !== taskID()) fail('TransactionInactiveError');
    return s;
  };
  const writable = (tx) => {
    const s = active(tx);
    if (s.mode === 'readonly') fail('ReadOnlyError');
    return s;
  };
  const newTransaction = (db, names, mode, durability, token, upgrade = false) => {
    const d = dbState(db),
      tx = make('IDBTransaction', {
        db,
        scope: names,
        mode,
        durability,
        token,
        upgrade,
        data: upgrade ? d.data : null,
        started: upgrade,
        finished: false,
        committing: false,
        activeTask: taskID(),
        queue: [],
        wrappers: new Map(),
        error: null,
        scheduled: false,
      });
    const s = txState(tx);
    d.transactions.add(tx);
    eventParents.set(tx, db);
    pending.set(token, { kind: 'transaction', tx });
    if (!upgrade) call('begin', d.name, token, d.connection);
    return tx;
  };
  method('IDBDatabase', 'transaction', 1, function (names, mode = 'readonly', options = {}) {
    const d = dbState(this);
    if (d.closing || d.closed || d.upgrade) fail('InvalidStateError');
    names = typeof names === 'string' ? [names] : Array.from(names, String);
    names = [...new Set(names)];
    if (!names.length) fail('InvalidAccessError');
    for (const n of names) if (!storeBy(d, n)) fail('NotFoundError');
    mode = String(mode);
    if (!['readonly', 'readwrite'].includes(mode)) throw new TypeError('Invalid transaction mode');
    const durability = options?.durability === undefined ? 'default' : String(options.durability);
    if (!['default', 'strict', 'relaxed'].includes(durability))
      throw new TypeError('Invalid durability');
    return newTransaction(this, names, mode, durability, ++sequence);
  });
  for (const n of ['db', 'mode', 'durability', 'error']) attr('IDBTransaction', n, (s) => s[n]);
  attr('IDBTransaction', 'objectStoreNames', (s) =>
    list(() =>
      s.upgrade
        ? schema(s)
            .map((x) => x.name)
            .sort()
        : s.scope.slice().sort(),
    ),
  );
  for (const n of ['complete', 'abort', 'error']) handler('IDBTransaction', n);
  const finish = (tx, commit, error = null) => {
    const s = txState(tx);
    if (s.finished) return;
    let encoded;
    try {
      encoded = encode(s.data);
    } catch (cause) {
      commit = false;
      error = cause;
      encoded = '';
    }
    s.finished = true;
    s.error = error;
    s.activeTask = -1;
    const d = dbState(s.db);
    if (commit && s.mode !== 'readonly') d.data = s.data;
    if (s.upgrade) {
      if (!commit) {
        d.data = parseData(s.original || '');
        d.version = s.oldVersion;
        d.closing = true;
      }
      d.upgrade = null;
    }
    call('finish', d.name, s.token, commit, encoded);
    if (!s.upgrade || !commit) pending.delete(s.token);
    d.transactions.delete(tx);
    task(() => {
      emit(tx, commit ? 'complete' : 'abort', { bubbles: !commit });
      maybeClose(s.db);
      if (s.openRequest) {
        const r = s.openRequest;
        requestState(r).transaction = null;
        if (commit) completeRequest(r, s.db);
        else completeRequest(r, undefined, platformDOMException('Upgrade aborted', 'AbortError'));
      }
      call('release', d.name, s.token);
    });
  };
  const abort = (tx, error = null) => {
    const s = txState(tx);
    if (s.finished) fail('InvalidStateError');
    s.committing = true;
    const requests = s.queue.splice(0);
    for (const op of requests)
      if (op.request)
        task(() =>
          completeRequest(
            op.request,
            undefined,
            platformDOMException('Transaction aborted', 'AbortError'),
          ),
        );
    finish(tx, false, error);
  };
  method('IDBTransaction', 'abort', 0, function () {
    abort(this);
  });
  method('IDBTransaction', 'commit', 0, function () {
    const s = active(this);
    s.committing = true;
    pump(this);
  });
  const pump = (tx) => {
    const s = txState(tx);
    if (!s.started || s.finished || s.scheduled) return;
    s.scheduled = true;
    task(() => {
      s.scheduled = false;
      if (s.finished) return;
      const op = s.queue.shift();
      if (!op) {
        finish(tx, true);
        return;
      }
      s.activeTask = taskID();
      try {
        const value = op.run();
        const e = op.request ? completeRequest(op.request, value) : null;
        if (e && eventSlots.get(e).listenerException && !s.finished)
          abort(tx, platformDOMException('A request listener threw', 'AbortError'));
      } catch (error) {
        const failure =
          error instanceof DOMException
            ? error
            : platformDOMException(String(error), 'UnknownError');
        if (!op.request) {
          if (!s.finished) abort(tx, failure);
        } else {
          const e = completeRequest(op.request, undefined, failure);
          if ((!e.defaultPrevented || eventSlots.get(e).listenerException) && !s.finished)
            abort(tx, failure);
        }
      }
      pump(tx);
    });
  };
  const enqueue = (tx, source, run, existing) => {
    active(tx);
    const s = txState(tx),
      request = existing || newRequest(source, tx);
    if (existing) {
      const r = requestState(request);
      r.readyState = 'pending';
      r.result = undefined;
      r.error = null;
    }
    s.queue.push({ request, run });
    pump(tx);
    return request;
  };
  const storeObject = (tx, name) => {
    const s = txState(tx);
    if (s.wrappers.has(name)) return s.wrappers.get(name);
    const obj = make('IDBObjectStore', { tx, name, indexes: new Map(), deleted: false });
    s.wrappers.set(name, obj);
    return obj;
  };
  const storeState = (o) => {
    const s = get(o, 'IDBObjectStore'),
      tx = txState(s.tx),
      row = storeBy(tx.data ? tx : dbState(tx.db), s.name);
    if (s.deleted || !row) fail('InvalidStateError');
    return { s, tx, row };
  };
  method('IDBTransaction', 'objectStore', 1, function (name) {
    const s = txState(this);
    if (s.finished) fail('InvalidStateError');
    name = String(name);
    if (!(s.upgrade ? schema(s).some((x) => x.name === name) : s.scope.includes(name)))
      fail('NotFoundError');
    return storeObject(this, name);
  });
  const upgradeState = (db) => {
    const d = dbState(db);
    if (!d.upgrade) fail('InvalidStateError');
    return active(d.upgrade);
  };
  method('IDBDatabase', 'createObjectStore', 1, function (name, options = {}) {
    const s = upgradeState(this);
    name = String(name);
    if (storeBy(s, name)) fail('ConstraintError');
    let keyPath = options?.keyPath == null ? null : parsePath(options.keyPath);
    const autoIncrement = !!options?.autoIncrement;
    if (autoIncrement && (keyPath === '' || Array.isArray(keyPath))) fail('InvalidAccessError');
    schema(s).push({ name, keyPath, autoIncrement, next: 1, indexes: [], records: [] });
    return storeObject(dbState(this).upgrade, name);
  });
  method('IDBDatabase', 'deleteObjectStore', 1, function (name) {
    const s = upgradeState(this);
    name = String(name);
    const i = schema(s).findIndex((x) => x.name === name);
    if (i < 0) fail('NotFoundError');
    schema(s).splice(i, 1);
    if (s.wrappers.has(name)) {
      get(s.wrappers.get(name), 'IDBObjectStore').deleted = true;
      s.wrappers.delete(name);
    }
  });
  attr(
    'IDBObjectStore',
    'name',
    (s) => s.name,
    (s, name, o) => {
      const t = active(s.tx);
      if (!t.upgrade) fail('InvalidStateError');
      const { row } = storeState(o);
      name = String(name);
      if (name === s.name) return;
      if (storeBy(t, name)) fail('ConstraintError');
      t.wrappers.delete(s.name);
      s.name = row.name = name;
      t.wrappers.set(name, o);
    },
  );
  attr('IDBObjectStore', 'transaction', (s) => s.tx);
  for (const n of ['keyPath', 'autoIncrement'])
    attr('IDBObjectStore', n, (s, o) => {
      const v = storeState(o).row[n];
      if (n === 'keyPath' && Array.isArray(v)) return s.keyPath || (s.keyPath = v.slice());
      return v;
    });
  attr('IDBObjectStore', 'indexNames', (s, o) =>
    list(() =>
      storeState(o)
        .row.indexes.map((x) => x.name)
        .sort(),
    ),
  );
  const indexKeys = (value, index) => {
    const v = pathValue(value, index.keyPath);
    if (v === undefined) return [];
    const out = [];
    for (const x of index.multiEntry && Array.isArray(v) ? v : [v])
      try {
        const k = key(x);
        if (!out.some((y) => compare(k, y) === 0)) out.push(k);
      } catch {}
    return out;
  };
  const checkUnique = (row, k, value) => {
    for (const idx of row.indexes.filter((x) => x.unique)) {
      const keys = indexKeys(value, idx);
      for (const record of row.records) {
        if (compare(record.key, k) === 0) continue;
        const prior = indexKeys(decode(record.value), idx);
        if (keys.some((x) => prior.some((y) => compare(x, y) === 0))) fail('ConstraintError');
      }
    }
  };
  const write = (o, value, explicit, add) => {
    const { s, row } = storeState(o);
    writable(s.tx);
    let k;
    if (row.keyPath !== null) {
      if (explicit !== undefined) fail('DataError');
      const v = pathValue(value, row.keyPath);
      if (v !== undefined) k = key(v);
      else if (!row.autoIncrement) fail('DataError');
    } else if (explicit !== undefined) k = key(explicit);
    else if (!row.autoIncrement) fail('DataError');
    let encoded = encode(value);
    const copied = decode(encoded);
    if (!k && row.keyPath !== null) {
      inject(copied, row.keyPath, 0);
      encoded = encode(copied);
    }
    return enqueue(s.tx, o, () => {
      const current = storeState(o).row;
      let chosen = k;
      if (!chosen) {
        if (current.next > Number.MAX_SAFE_INTEGER) fail('ConstraintError');
        chosen = [0, current.next];
        if (current.keyPath !== null) {
          inject(copied, current.keyPath, current.next);
          encoded = encode(copied);
        }
      }
      const i = current.records.findIndex((r) => compare(r.key, chosen) === 0);
      if (add && i >= 0) fail('ConstraintError');
      checkUnique(current, chosen, decode(encoded));
      if (current.autoIncrement && chosen[0] === 0 && chosen[1] >= current.next)
        current.next = Math.min(Math.floor(chosen[1]) + 1, Number.MAX_SAFE_INTEGER + 1);
      const record = { key: chosen, value: encoded };
      if (i >= 0) current.records[i] = record;
      else current.records.push(record);
      current.records.sort((a, b) => compare(a.key, b.key));
      return keyValue(chosen);
    });
  };
  for (const n of ['put', 'add'])
    method('IDBObjectStore', n, 1, function (v, k) {
      return write(this, v, k, n === 'add');
    });
  const queryRows = (o, q, direction = 'next') => {
    const type = slots.get(o).type;
    let store, idx;
    if (type === 'IDBObjectStore') store = storeState(o).row;
    else {
      const x = indexState(o);
      store = x.row;
      idx = x.index;
    }
    let rows = [];
    for (const record of store.records) {
      if (idx) {
        for (const k of indexKeys(decode(record.value), idx))
          if (matches(k, q)) rows.push({ key: k, primaryKey: record.key, value: record.value });
      } else if (matches(record.key, q))
        rows.push({ key: record.key, primaryKey: record.key, value: record.value });
    }
    rows.sort((a, b) => compare(a.key, b.key) || compare(a.primaryKey, b.primaryKey));
    if (direction.startsWith('prev')) rows.reverse();
    if (direction.endsWith('unique')) {
      const unique = [];
      for (const row of rows)
        if (!unique.length || compare(unique.at(-1).key, row.key) !== 0) unique.push(row);
      rows = unique;
    }
    return rows;
  };
  const sourceTx = (o) =>
    slots.get(o).type === 'IDBObjectStore' ? storeState(o).s.tx : indexState(o).store.s.tx;
  const countArg = (count) => {
    if (count === undefined) return Infinity;
    count = Number(count);
    if (!Number.isFinite(count) || count < 0 || count > 4294967295)
      throw new TypeError('Invalid count');
    count = Math.trunc(count);
    return count === 0 ? Infinity : count;
  };
  for (const type of ['IDBObjectStore', 'IDBIndex']) {
    for (const name of ['get', 'getKey', 'getAll', 'getAllKeys', 'count', 'getAllRecords'])
      method(type, name, ['get', 'getKey'].includes(name) ? 1 : 0, function (query, count) {
        const tx = sourceTx(this);
        active(tx);
        let direction = 'next';
        if (
          query &&
          typeof query === 'object' &&
          !Array.isArray(query) &&
          !(query instanceof Date) &&
          !ArrayBuffer.isView(query) &&
          !(query instanceof ArrayBuffer) &&
          !(query instanceof IDBKeyRange) &&
          ['getAll', 'getAllKeys', 'getAllRecords'].includes(name)
        ) {
          direction = String(query.direction || 'next');
          count = query.count;
          query = query.query;
        }
        if (!['next', 'prev', 'nextunique', 'prevunique'].includes(direction))
          throw new TypeError('Invalid direction');
        if (['get', 'getKey'].includes(name) && query == null) fail('DataError');
        const q = range(query),
          limit = countArg(count);
        return enqueue(tx, this, () => {
          const rows = queryRows(this, q, direction);
          if (name === 'count') return rows.length;
          if (name === 'get') return rows.length ? decode(rows[0].value) : undefined;
          if (name === 'getKey') return rows.length ? keyValue(rows[0].primaryKey) : undefined;
          return rows.slice(0, limit).map((r) =>
            name === 'getAll'
              ? decode(r.value)
              : name === 'getAllKeys'
                ? keyValue(r.primaryKey)
                : {
                    key: keyValue(r.key),
                    primaryKey: keyValue(r.primaryKey),
                    value: decode(r.value),
                  },
          );
        });
      });
  }
  method('IDBObjectStore', 'delete', 1, function (query) {
    const { s } = storeState(this);
    writable(s.tx);
    if (query == null) fail('DataError');
    const q = range(query);
    return enqueue(s.tx, this, () => {
      const r = storeState(this).row;
      r.records = r.records.filter((x) => !matches(x.key, q));
    });
  });
  method('IDBObjectStore', 'clear', 0, function () {
    const { s } = storeState(this);
    writable(s.tx);
    return enqueue(s.tx, this, () => {
      storeState(this).row.records = [];
    });
  });
  const indexState = (o) => {
    const s = get(o, 'IDBIndex'),
      store = storeState(s.store),
      index = store.row.indexes.find((x) => x.name === s.name);
    if (s.deleted || !index) fail('InvalidStateError');
    return { s, store, row: store.row, index };
  };
  const indexObject = (o, name) => {
    const { s, row } = storeState(o);
    name = String(name);
    if (!row.indexes.some((x) => x.name === name)) fail('NotFoundError');
    if (!s.indexes.has(name))
      s.indexes.set(name, make('IDBIndex', { store: o, name, deleted: false }));
    return s.indexes.get(name);
  };
  method('IDBObjectStore', 'index', 1, function (name) {
    const { tx } = storeState(this);
    if (tx.finished) fail('InvalidStateError');
    return indexObject(this, name);
  });
  method('IDBObjectStore', 'createIndex', 2, function (name, keyPath, options = {}) {
    const { s, row } = storeState(this),
      tx = active(s.tx);
    if (!tx.upgrade) fail('InvalidStateError');
    name = String(name);
    keyPath = parsePath(keyPath);
    if (row.indexes.some((x) => x.name === name)) fail('ConstraintError');
    const index = { name, keyPath, unique: !!options?.unique, multiEntry: !!options?.multiEntry };
    if (index.multiEntry && Array.isArray(keyPath)) fail('InvalidAccessError');
    row.indexes.push(index);
    if (index.unique) {
      tx.queue.push({
        request: null,
        run: () => {
          if (!row.indexes.includes(index)) return;
          const seen = [];
          for (const r of row.records)
            for (const k of indexKeys(decode(r.value), index)) {
              if (seen.some((x) => compare(x, k) === 0)) fail('ConstraintError');
              seen.push(k);
            }
        },
      });
      pump(s.tx);
    }
    return indexObject(this, name);
  });
  method('IDBObjectStore', 'deleteIndex', 1, function (name) {
    const { s, row } = storeState(this),
      tx = active(s.tx);
    if (!tx.upgrade) fail('InvalidStateError');
    name = String(name);
    const i = row.indexes.findIndex((x) => x.name === name);
    if (i < 0) fail('NotFoundError');
    row.indexes.splice(i, 1);
    if (s.indexes.has(name)) {
      get(s.indexes.get(name), 'IDBIndex').deleted = true;
      s.indexes.delete(name);
    }
  });
  attr('IDBIndex', 'objectStore', (s) => s.store);
  for (const n of ['keyPath', 'multiEntry', 'unique'])
    attr('IDBIndex', n, (s, o) => {
      const v = indexState(o).index[n];
      if (n === 'keyPath' && Array.isArray(v)) return s.keyPath || (s.keyPath = v.slice());
      return v;
    });
  attr(
    'IDBIndex',
    'name',
    (s) => s.name,
    (s, name, o) => {
      const x = indexState(o),
        tx = active(x.store.s.tx);
      if (!tx.upgrade) fail('InvalidStateError');
      name = String(name);
      if (name === s.name) return;
      if (x.row.indexes.some((i) => i.name === name)) fail('ConstraintError');
      x.store.s.indexes.delete(s.name);
      s.name = x.index.name = name;
      x.store.s.indexes.set(name, o);
    },
  );
  const cursorState = (o) => {
    const s = slots.get(o);
    if (!s || !['IDBCursor', 'IDBCursorWithValue'].includes(s.type))
      throw new TypeError('Illegal invocation');
    return s;
  };
  for (const n of ['source', 'direction', 'request', 'key', 'primaryKey'])
    Object.defineProperty(IDBCursor.prototype, n, {
      get() {
        if (!slots.has(this)) return remote(this, 'IDBCursor', n, 'get');
        const s = cursorState(this);
        return n === 'key' || n === 'primaryKey'
          ? s[n] === undefined
            ? undefined
            : keyValue(s[n])
          : s[n];
      },
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(IDBCursorWithValue.prototype, 'value', {
    get() {
      return cursorState(this).value;
    },
    enumerable: true,
    configurable: true,
  });
  const cursorStep = (cursor, skip = 1, target = null, primary = null) => {
    const c = cursorState(cursor),
      tx = sourceTx(c.source);
    active(tx);
    return enqueue(
      tx,
      c.source,
      () => {
        let rows = queryRows(c.source, c.query, c.direction);
        if (c.position) {
          const reverse = c.direction.startsWith('prev');
          rows = rows.filter((r) => {
            const d = compare(r.key, c.key) || compare(r.primaryKey, c.primaryKey);
            return reverse ? d < 0 : d > 0;
          });
        }
        if (target)
          rows = rows.filter((r) => {
            const d = compare(r.key, target) || (primary ? compare(r.primaryKey, primary) : 0);
            return c.direction.startsWith('prev') ? d <= 0 : d >= 0;
          });
        const r = rows[skip - 1];
        c.got = !!r;
        if (!r) return null;
        c.position = true;
        c.key = r.key;
        c.primaryKey = r.primaryKey;
        c.value = decode(r.value);
        return cursor;
      },
      c.request,
    );
  };
  for (const type of ['IDBObjectStore', 'IDBIndex'])
    for (const name of ['openCursor', 'openKeyCursor'])
      method(type, name, 0, function (query, direction = 'next') {
        const tx = sourceTx(this);
        active(tx);
        direction = String(direction);
        if (!['next', 'prev', 'nextunique', 'prevunique'].includes(direction))
          throw new TypeError('Invalid direction');
        const queryRange = range(query),
          request = newRequest(this, tx),
          cursor = make(name === 'openCursor' ? 'IDBCursorWithValue' : 'IDBCursor', {
            source: this,
            direction,
            request,
            query: queryRange,
            got: false,
            position: false,
            key: undefined,
            primaryKey: undefined,
            value: undefined,
          });
        cursorStep(cursor);
        return request;
      });
  const cursorMethod = (name, n, fn) => {
    const f = {
      [name](...a) {
        if (!slots.has(this)) return remote(this, 'IDBCursor', name, 'method', a);
        need(a, n);
        return fn.call(this, cursorState(this), ...a);
      },
    }[name];
    operations.set(operationKey('IDBCursor', name, 'method'), f);
    markNative(f, name);
    Object.defineProperty(IDBCursor.prototype, name, {
      value: f,
      enumerable: true,
      writable: true,
      configurable: true,
    });
  };
  const cursorReady = (c) => {
    active(sourceTx(c.source));
    if (!c.got) fail('InvalidStateError');
  };
  cursorMethod('continue', 0, function (c, v) {
    cursorReady(c);
    const k = v === undefined ? null : key(v);
    if (k) {
      const d = compare(k, c.key);
      if (c.direction.startsWith('prev') ? d >= 0 : d <= 0) fail('DataError');
    }
    c.got = false;
    cursorStep(this, 1, k);
  });
  cursorMethod('advance', 1, function (c, n) {
    n = Number(n);
    if (!Number.isFinite(n) || n < 1 || n > 4294967295) throw new TypeError('Invalid count');
    cursorReady(c);
    c.got = false;
    cursorStep(this, Math.floor(n));
  });
  cursorMethod('continuePrimaryKey', 2, function (c, k, p) {
    cursorReady(c);
    if (slots.get(c.source).type !== 'IDBIndex' || c.direction.endsWith('unique'))
      fail('InvalidAccessError');
    k = key(k);
    p = key(p);
    const d = compare(k, c.key) || compare(p, c.primaryKey);
    if (c.direction.startsWith('prev') ? d >= 0 : d <= 0) fail('DataError');
    c.got = false;
    cursorStep(this, 1, k, p);
  });
  cursorMethod('delete', 0, function (c) {
    cursorReady(c);
    writable(sourceTx(c.source));
    if (c.type !== 'IDBCursorWithValue') fail('InvalidStateError');
    const store =
      slots.get(c.source).type === 'IDBIndex' ? get(c.source, 'IDBIndex').store : c.source;
    return IDBObjectStore.prototype.delete.call(store, keyValue(c.primaryKey));
  });
  cursorMethod('update', 1, function (c, value) {
    cursorReady(c);
    writable(sourceTx(c.source));
    if (c.type !== 'IDBCursorWithValue') fail('InvalidStateError');
    const store =
        slots.get(c.source).type === 'IDBIndex' ? get(c.source, 'IDBIndex').store : c.source,
      { row } = storeState(store);
    if (row.keyPath !== null) {
      const k = key(pathValue(value, row.keyPath));
      if (compare(k, c.primaryKey) !== 0) fail('DataError');
      return write(store, value, undefined, false);
    }
    return write(store, value, keyValue(c.primaryKey), false);
  });
  for (const type of ['IDBRequest', 'IDBCursor', 'IDBCursorWithValue'])
    for (const name of Object.getOwnPropertyNames(globalThis[type].prototype)) {
      const d = Object.getOwnPropertyDescriptor(globalThis[type].prototype, name);
      if (d.get && !operations.has(operationKey(type, name, 'get')))
        operations.set(operationKey(type, name, 'get'), d.get);
      if (d.set && !operations.has(operationKey(type, name, 'set')))
        operations.set(operationKey(type, name, 'set'), d.set);
    }
  registerBootstrapCallback('installIndexedDBEncoder', (value) => {
    try {
      return [true, encode(value)];
    } catch (error) {
      return [false, error.name, error.message];
    }
  });
  registerBootstrapCallback('installIndexedDBNotifier', (row) => {
    const p = pending.get(row.token);
    if (!p) return;
    if (row.kind === 'versionchange') {
      if (p.db && !dbState(p.db).closed) emit(p.db, 'versionchange', {}, row);
      return;
    }
    if (p.kind === 'transaction') {
      const s = txState(p.tx);
      if (row.kind === 'error') {
        abort(p.tx, platformDOMException(row.error, row.error));
        return;
      }
      s.data = parseData(row.data);
      s.started = true;
      pump(p.tx);
      return;
    }
    if (row.kind === 'blocked') {
      emit(p.request, 'blocked', {}, row);
      return;
    }
    if (row.kind === 'error') {
      pending.delete(row.token);
      completeRequest(p.request, undefined, platformDOMException(row.error, row.error));
      return;
    }
    if (row.kind === 'deleted') {
      pending.delete(row.token);
      requestState(p.request).readyState = 'done';
      emit(p.request, 'success', {}, { oldVersion: row.oldVersion, newVersion: null });
      return;
    }
    const db = dbObject(row);
    p.db = db;
    if (row.kind === 'opened') {
      completeRequest(p.request, db);
      return;
    }
    // An open request owns both its connection token and upgrade transaction.
    // The host uses the open token as its exclusive transaction identity.
    const tx = newTransaction(db, [], 'versionchange', 'default', row.token, true),
      s = txState(tx);
    pending.set(row.token, p);
    s.openRequest = p.request;
    s.original = row.data;
    s.oldVersion = row.oldVersion;
    dbState(db).upgrade = tx;
    const rs = requestState(p.request);
    rs.readyState = 'done';
    rs.result = db;
    rs.transaction = tx;
    const e = emit(p.request, 'upgradeneeded', {}, row);
    if (eventSlots.get(e).listenerException && !s.finished)
      abort(tx, platformDOMException('Upgrade listener threw', 'AbortError'));
    pump(tx);
  });
}
