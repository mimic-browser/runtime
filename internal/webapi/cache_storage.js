// Runs in the Fetch closure so cache readbacks use the same response records,
// stream cloning, header guards, and body-consumption rules as network Fetch.
if (typeof CacheStorage === 'function' && typeof Cache === 'function' && host.cacheStorage) {
  const cacheSlots = new WeakMap(),
    storage = Object.create(CacheStorage.prototype);
  Object.defineProperty(globalThis, 'caches', {
    get() {
      return storage;
    },
    enumerable: true,
    configurable: true,
  });
  const call = (name, ...args) => host.cacheStorage(name, ...args);
  const cacheMethod = (proto, name, count, implementation) => {
    const fn = {
      [name](...args) {
        try {
          const id =
            proto === Cache.prototype ? cacheSlots.get(this) : this === storage ? 0 : undefined;
          if (!host.hasStorageAccess())
            throw platformDOMException('Access to CacheStorage is denied.', 'SecurityError');
          if (id === undefined) throw new TypeError('Illegal invocation');
          if (args.length < count)
            throw new TypeError(
              `Failed to execute '${name}' on '${proto === Cache.prototype ? 'Cache' : 'CacheStorage'}': ${count} argument${count === 1 ? '' : 's'} required, but only ${args.length} present.`,
            );
          return implementation(id, ...args);
        } catch (e) {
          return platformPromiseReject(e);
        }
      },
    }[name];
    Object.defineProperty(fn, 'length', { value: count, configurable: true });
    markNative(fn, name);
    Object.defineProperty(proto, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const io = (fn) =>
    new platformPromise((resolve, reject) =>
      host.enqueueWebTask(
        () => {
          try {
            resolve(fn());
          } catch (e) {
            reject(e);
          }
        },
        1,
        0,
        false,
      ),
    );
  const requestRecord = (input) => {
    const r = new FetchRequest(input),
      s = requests.get(r);
    return { ...s, headers: Array.from(s.headers), signal: undefined };
  };
  const urlKey = (value, ignoreSearch) => {
    const u = new globalThis.URL(value);
    u.hash = '';
    if (ignoreSearch) u.search = '';
    return u.href;
  };
  const matches = (entry, request, options = {}) => {
    if (!request) return true;
    if (!options.ignoreMethod && request.method !== 'GET') return false;
    if (
      urlKey(entry.request.url, options.ignoreSearch) !== urlKey(request.url, options.ignoreSearch)
    )
      return false;
    if (!options.ignoreVary) {
      const vary = new BaseHeaders(entry.response.headers).get('vary');
      if (vary) {
        const old = new BaseHeaders(entry.request.headers),
          next = new BaseHeaders(request.headers);
        for (const raw of vary.split(',')) {
          const key = raw.trim();
          if (key === '*' || old.get(key) !== next.get(key)) return false;
        }
      }
    }
    return true;
  };
  const read = (id) => {
    const snapshot = call('read', id);
    return { version: snapshot.version, records: JSON.parse(snapshot.records) };
  };
  const edit = (id, update) => {
    for (;;) {
      const snapshot = read(id),
        result = update(snapshot.records);
      if (call('write', id, snapshot.version, JSON.stringify(result.records))) return result.value;
    }
  };
  const responseFrom = (record) => {
    const list = headers(record.headers, 'response');
    return createResponse(
      { ...record, headers: list },
      {
        stream: record.body === null ? null : streamBytes(new Uint8Array(record.body)),
        type: list.get('content-type'),
      },
    );
  };
  const requestFrom = (record) => {
    const r = new FetchRequest(record.url, record);
    return r;
  };
  const prepare = async (input, response) => {
    const request = requestRecord(input),
      record = slot(responses, response),
      body = slot(bodies, response);
    if (request.method !== 'GET' || !/^https?:/.test(request.url))
      throw new TypeError('Request scheme or method is unsupported');
    if (record.status === 206) throw new TypeError('Partial response is unsupported');
    if (
      record.headers
        .get('vary')
        ?.split(',')
        .some((v) => v.trim() === '*')
    )
      throw new TypeError('Vary header contains *');
    if (unusable(body)) throw new TypeError('Response body is already used');
    const data = body.stream === null ? null : Array.from(await consume(response));
    return { request, response: { ...record, headers: Array.from(record.headers), body: data } };
  };
  const putBatch = (id, entries) =>
    io(() =>
      edit(id, (records) => {
        for (let i = 0; i < entries.length; i++)
          for (let j = 0; j < i; j++)
            if (matches(entries[j], entries[i].request))
              throw platformDOMException('Duplicate requests in batch', 'InvalidStateError');
        for (const entry of entries) {
          records = records.filter((old) => !matches(old, entry.request));
          records.push(entry);
        }
        return { records, value: undefined };
      }),
    );
  cacheMethod(CacheStorage.prototype, 'open', 1, (_id, name) => {
    name = String(name);
    return io(() => {
      const cache = Object.create(Cache.prototype);
      cacheSlots.set(cache, call('open', name));
      return cache;
    });
  });
  cacheMethod(CacheStorage.prototype, 'keys', 0, () => io(() => call('keys')));
  cacheMethod(CacheStorage.prototype, 'has', 1, (_id, name) => {
    name = String(name);
    return io(() => call('has', name));
  });
  cacheMethod(CacheStorage.prototype, 'delete', 1, (_id, name) => {
    name = String(name);
    return io(() => call('delete', name));
  });
  cacheMethod(CacheStorage.prototype, 'match', 1, (_id, input, options = {}) => {
    const request = requestRecord(input);
    options = options ?? {};
    return io(() => {
      const names = options.cacheName === undefined ? call('keys') : [String(options.cacheName)];
      for (const name of names) {
        if (!call('has', name)) continue;
        const found = read(call('open', name)).records.find((entry) =>
          matches(entry, request, options),
        );
        if (found) return responseFrom(found.response);
      }
    });
  });
  cacheMethod(Cache.prototype, 'match', 1, (id, input, options = {}) => {
    const request = requestRecord(input);
    return io(() => {
      const found = read(id).records.find((entry) => matches(entry, request, options ?? {}));
      return found ? responseFrom(found.response) : undefined;
    });
  });
  cacheMethod(Cache.prototype, 'matchAll', 0, (id, input, options = {}) => {
    const request = input === undefined ? null : requestRecord(input);
    return io(() =>
      read(id)
        .records.filter((entry) => matches(entry, request, options ?? {}))
        .map((entry) => responseFrom(entry.response)),
    );
  });
  cacheMethod(Cache.prototype, 'keys', 0, (id, input, options = {}) => {
    const request = input === undefined ? null : requestRecord(input);
    return io(() =>
      read(id)
        .records.filter((entry) => matches(entry, request, options ?? {}))
        .map((entry) => requestFrom(entry.request)),
    );
  });
  cacheMethod(Cache.prototype, 'delete', 1, (id, input, options = {}) => {
    const request = requestRecord(input);
    return io(() =>
      edit(id, (records) => {
        const keep = records.filter((entry) => !matches(entry, request, options ?? {}));
        return { records: keep, value: keep.length !== records.length };
      }),
    );
  });
  cacheMethod(Cache.prototype, 'put', 2, async (id, input, response) =>
    putBatch(id, [await prepare(input, response)]),
  );
  const addAll = async (id, inputs) => {
    const requests = Array.from(inputs, (input) => new FetchRequest(input));
    for (const r of requests)
      if (r.method !== 'GET' || !/^https?:/.test(r.url))
        throw new TypeError('Request scheme or method is unsupported');
    const entries = await platformPromiseAll(
      requests.map(async (r) => {
        const response = await fetch(r);
        if (!response.ok) throw new TypeError('Request failed');
        return prepare(r, response);
      }),
    );
    return putBatch(id, entries);
  };
  cacheMethod(Cache.prototype, 'add', 1, (id, input) => addAll(id, [input]));
  cacheMethod(Cache.prototype, 'addAll', 1, addAll);
}
