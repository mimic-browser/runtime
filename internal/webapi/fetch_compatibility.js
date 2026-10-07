// Fetch Body semantics over canonical transport and the pinned streams implementation.
// This file runs inside either realm closure after shared primitives and streams_vendor.
{
  const fetchBaseURL = () =>
    typeof document === 'undefined' ? globalThis.location.href : document.baseURI || document.URL;
  const Streams = globalThis.ReadableStream;
  const BaseHeaders = globalThis.Headers;
  const NativeBlob = globalThis.Blob;
  const Encoder = globalThis.TextEncoder;
  const Decoder = globalThis.TextDecoder;
  const bodies = new WeakMap(),
    requests = new WeakMap(),
    responses = new WeakMap(),
    guards = new WeakMap();
  let multipartSequence = 0;
  const bytes = (value) =>
    value instanceof ArrayBuffer
      ? new Uint8Array(value).slice()
      : new Uint8Array(value.buffer, value.byteOffset, value.byteLength).slice();
  const streamBytes = (value) =>
    new Streams({
      type: 'bytes',
      start(controller) {
        if (value.length) controller.enqueue(value.slice());
        controller.close();
      },
    });
  // Pinned web-streams-polyfill exposes this internal slot. Keep this adapter in
  // one place and test read/release/cancel/tee when upgrading the vendor version.
  const disturbed = (stream) => stream !== null && stream._disturbed === true;
  const unusable = (record) =>
    record.stream !== null && (record.stream.locked || disturbed(record.stream));
  const slot = (map, value) => {
    const result = map.get(value);
    if (!result) throw new TypeError('Illegal invocation');
    return result;
  };
  const forbidden = (name) =>
    /^(accept-charset|accept-encoding|access-control-request-headers|access-control-request-method|connection|content-length|cookie|cookie2|date|dnt|expect|host|keep-alive|origin|referer|te|trailer|transfer-encoding|upgrade|via)$/.test(
      name,
    ) ||
    name.startsWith('proxy-') ||
    name.startsWith('sec-');
  class FetchHeaders extends BaseHeaders {
    append(name, value) {
      headersState(this, arguments.length, 2);
      name = headerByteString(name);
      value = headerByteString(value);
      name = headerName(name);
      value = headerValue(value);
      const guard = guards.get(this);
      if (guard === 'immutable') throw new TypeError('Headers are immutable');
      if (
        (guard === 'request' && forbidden(name)) ||
        (guard === 'response' && ['set-cookie', 'set-cookie2'].includes(name))
      )
        return;
      super.append(name, value);
    }
    set(name, value) {
      headersState(this, arguments.length, 2);
      name = headerByteString(name);
      value = headerByteString(value);
      name = headerName(name);
      value = headerValue(value);
      const guard = guards.get(this);
      if (guard === 'immutable') throw new TypeError('Headers are immutable');
      if (
        (guard === 'request' && forbidden(name)) ||
        (guard === 'response' && ['set-cookie', 'set-cookie2'].includes(name))
      )
        return;
      super.set(name, value);
    }
    delete(name) {
      headersState(this, arguments.length, 1);
      name = headerName(name);
      const guard = guards.get(this);
      if (guard === 'immutable') throw new TypeError('Headers are immutable');
      if (
        (guard === 'request' && forbidden(name)) ||
        (guard === 'response' && ['set-cookie', 'set-cookie2'].includes(name))
      )
        return;
      super.delete(name);
    }
  }
  Object.defineProperty(FetchHeaders, 'name', { value: 'Headers' });
  function headers(init, guard) {
    const result = new FetchHeaders();
    guards.set(result, guard);
    if (init != null) {
      const source = new BaseHeaders(init);
      for (const [name, value] of source) result.append(name, value);
    }
    return result;
  }
  function extract(input) {
    if (input == null) return { stream: null, type: null, bytes: new Uint8Array() };
    if (input instanceof Streams) {
      if (input.locked || disturbed(input))
        throw new TypeError('Body stream is locked or disturbed');
      return { stream: input, type: null };
    }
    if (ArrayBuffer.isView(input) || input instanceof ArrayBuffer) {
      const value = bytes(input);
      return { stream: streamBytes(value), type: null, bytes: value };
    }
    if (input instanceof NativeBlob)
      return {
        stream: input.stream(),
        type: input.type || null,
        bytes: new Uint8Array(blobState(input).bytes).slice(),
      };
    if (input instanceof globalThis.FormData) {
      const boundary =
          '----WebKitFormBoundaryMimic' + (++multipartSequence).toString(16).padStart(8, '0'),
        parts = [];
      const quoted = (value) =>
        String(value).replace(/\r|\n/g, ' ').replace(/\\/g, '\\\\').replace(/"/g, '%22');
      for (const [name, value] of input) {
        let header =
          '--' + boundary + '\r\nContent-Disposition: form-data; name="' + quoted(name) + '"';
        if (value instanceof NativeBlob) {
          header +=
            '; filename="' +
            quoted(value instanceof globalThis.File ? value.name : 'blob') +
            '"\r\nContent-Type: ' +
            (value.type || 'application/octet-stream');
          parts.push(
            new Encoder().encode(header + '\r\n\r\n'),
            new Uint8Array(blobState(value).bytes),
            new Encoder().encode('\r\n'),
          );
        } else parts.push(new Encoder().encode(header + '\r\n\r\n' + String(value) + '\r\n'));
      }
      parts.push(new Encoder().encode('--' + boundary + '--\r\n'));
      const size = parts.reduce((total, part) => total + part.byteLength, 0),
        value = new Uint8Array(size);
      let offset = 0;
      for (const part of parts) {
        value.set(part, offset);
        offset += part.byteLength;
      }
      return {
        stream: streamBytes(value),
        type: 'multipart/form-data; boundary=' + boundary,
        bytes: value,
      };
    }
    const type =
      input instanceof globalThis.URLSearchParams
        ? 'application/x-www-form-urlencoded;charset=UTF-8'
        : 'text/plain;charset=UTF-8';
    if (typeof input === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    const value = new Encoder().encode(String(input));
    return { stream: streamBytes(value), type, bytes: value };
  }
  if (typeof document !== 'undefined' && typeof host.sendBeacon === 'function') {
    const prefix = "Failed to execute 'sendBeacon' on 'Navigator': ";
    navigatorBeaconBindings = {
      beaconURL(raw) {
        let url;
        try {
          url = new globalThis.URL(raw, fetchBaseURL());
        } catch {
          throw new TypeError(prefix + 'The URL argument is ill-formed or unsupported.');
        }
        if (!['http:', 'https:'].includes(url.protocol))
          throw new TypeError(prefix + 'Beacons are only supported over HTTP(S).');
        return url.href;
      },
      sendBeacon(url, body, type) {
        // A borrowed operation may receive a foreign-realm array proxy. Copy
        // its numeric observations into the owner's ordinary host argument.
        return host.sendBeacon(url, Array.from(body), type);
      },
    };
    const sendBeacon = {
      sendBeacon(url, data) {
        const binding = requireRealmBinding(this, 'Navigator');
        if (arguments.length === 0)
          throw new TypeError(prefix + '1 argument required, but only 0 present.');
        if (typeof url === 'symbol')
          throw new TypeError(prefix + 'Cannot convert a Symbol value to a string');
        const resolved = callRealmBinding(this, binding, 'beaconURL', [String(url)]);
        if (data instanceof Streams)
          throw new TypeError(prefix + 'sendBeacon cannot have a ReadableStream body.');
        if (typeof data === 'symbol')
          throw new TypeError(prefix + 'Cannot convert a Symbol value to a string');
        const body = extract(data);
        return callRealmBinding(this, binding, 'sendBeacon', [
          resolved,
          Array.from(body.bytes),
          body.type,
        ]);
      },
    }.sendBeacon;
    Object.defineProperty(Navigator.prototype, 'sendBeacon', {
      value: sendBeacon,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  }
  function responseBody(value, signal, type, unavailable) {
    let cleanup = () => {};
    const stream = new Streams({
      type: 'bytes',
      start(controller) {
        const abort = () => {
          controller.error(platformDOMException('The user aborted a request.', 'AbortError'));
          cleanup();
        };
        cleanup = () => signal.removeEventListener('abort', abort);
        if (signal.aborted) {
          abort();
          return;
        }
        if (unavailable) {
          controller.error(new TypeError('Response body unavailable under execution policy'));
          return;
        }
        signal.addEventListener('abort', abort, { once: true });
        // Transport supplies a fresh, engine-owned buffer. The stream owns this
        // view; cloning a Response still uses the byte stream's independent tee.
        if (value.length) controller.enqueue(value);
        controller.close();
      },
      cancel() {
        cleanup();
      },
    });
    return { stream, type, cleanup };
  }
  async function consume(object) {
    const body = slot(bodies, object);
    if (unusable(body)) throw new TypeError('Body is locked or already used');
    if (body.stream === null) return new Uint8Array();
    const reader = body.stream.getReader(),
      parts = [];
    let size = 0;
    try {
      for (;;) {
        const next = await reader.read();
        if (next.done) break;
        if (!(next.value instanceof Uint8Array))
          throw new TypeError('Body stream chunk must be Uint8Array');
        parts.push(next.value);
        size += next.value.byteLength;
      }
    } finally {
      reader.releaseLock();
      if (body.cleanup) body.cleanup();
    }
    const result = new Uint8Array(size);
    let offset = 0;
    for (const part of parts) {
      result.set(part, offset);
      offset += part.byteLength;
    }
    return result;
  }
  function cloneBody(object) {
    const body = slot(bodies, object);
    if (unusable(body)) throw new TypeError('Body is locked or already used');
    if (body.stream === null) return { ...body, stream: null };
    const [left, right] = body.stream.tee();
    body.stream = left;
    return { ...body, stream: right };
  }
  function bodyMethods(prototype) {
    Object.defineProperties(prototype, {
      body: {
        get() {
          return slot(bodies, this).stream;
        },
        configurable: true,
        enumerable: true,
      },
      bodyUsed: {
        get() {
          return disturbed(slot(bodies, this).stream);
        },
        configurable: true,
        enumerable: true,
      },
      bytes: {
        value: function bytes() {
          return consume(this);
        },
        writable: true,
        configurable: true,
        enumerable: true,
      },
      arrayBuffer: {
        value: function arrayBuffer() {
          return consume(this).then((value) => value.buffer);
        },
        writable: true,
        configurable: true,
        enumerable: true,
      },
      text: {
        value: function text() {
          return consume(this).then((value) => new Decoder().decode(value));
        },
        writable: true,
        configurable: true,
        enumerable: true,
      },
      json: {
        value: function json() {
          return this.text().then(JSON.parse);
        },
        writable: true,
        configurable: true,
        enumerable: true,
      },
      blob: {
        value: function blob() {
          return consume(this).then(
            (value) => new NativeBlob([value], { type: this.headers.get('content-type') || '' }),
          );
        },
        writable: true,
        configurable: true,
        enumerable: true,
      },
    });
  }
  const nullBodyStatus = (status) => [101, 103, 204, 205, 304].includes(status);
  function createResponse(record, body) {
    const response = Object.create(FetchResponse.prototype);
    responses.set(response, record);
    bodies.set(response, body);
    return response;
  }
  class FetchResponse {
    constructor(body = null, init = {}) {
      const status = init.status === undefined ? 200 : (Number(init.status) >>> 0) & 65535;
      if (status < 200 || status > 599) throw new RangeError('Invalid response status');
      const statusText = String(init.statusText || '');
      if (/[^\x09\x20-\x7e\x80-\xff]/.test(statusText)) throw new TypeError('Invalid status text');
      if (body != null && nullBodyStatus(status))
        throw new TypeError('Response status cannot have a body');
      const extracted = extract(body),
        list = headers(init.headers, 'response');
      if (extracted.type && !list.has('content-type')) list.set('content-type', extracted.type);
      responses.set(this, {
        status,
        statusText,
        headers: list,
        url: '',
        type: 'default',
        redirected: false,
      });
      bodies.set(this, extracted);
    }
    get ok() {
      const status = slot(responses, this).status;
      return status >= 200 && status <= 299;
    }
    clone() {
      const record = slot(responses, this),
        body = cloneBody(this),
        list = headers(record.headers, 'response');
      guards.set(list, guards.get(record.headers));
      return createResponse({ ...record, headers: list }, body);
    }
    static error() {
      const list = headers();
      guards.set(list, 'immutable');
      return createResponse(
        { status: 0, statusText: '', headers: list, url: '', type: 'error', redirected: false },
        { stream: null, type: null },
      );
    }
    static redirect(url, status = 302) {
      status = (Number(status) >>> 0) & 65535;
      if (![301, 302, 303, 307, 308].includes(status))
        throw new RangeError('Invalid redirect status');
      const result = new FetchResponse(null, {
        status,
        headers: { location: new globalThis.URL(String(url), fetchBaseURL()).href },
      });
      guards.set(result.headers, 'immutable');
      return result;
    }
    static json(data, init = {}) {
      const value = JSON.stringify(data);
      if (value === undefined) throw new TypeError('Value is not JSON serializable');
      const list = new FetchHeaders(init.headers);
      if (!list.has('content-type')) list.set('content-type', 'application/json');
      return new FetchResponse(value, { ...init, headers: list });
    }
  }
  for (const name of ['status', 'statusText', 'headers', 'url', 'type', 'redirected'])
    Object.defineProperty(FetchResponse.prototype, name, {
      get() {
        return slot(responses, this)[name];
      },
      configurable: true,
      enumerable: true,
    });
  bodyMethods(FetchResponse.prototype);
  Object.defineProperty(FetchResponse, 'name', { value: 'Response' });
  Object.defineProperty(FetchResponse.prototype, Symbol.toStringTag, {
    value: 'Response',
    configurable: true,
  });
  const normalizedMethods = new Set(['DELETE', 'GET', 'HEAD', 'OPTIONS', 'POST', 'PUT']);
  function dependentSignal(parent) {
    const controller = new globalThis.AbortController();
    if (parent != null) {
      if (!(parent instanceof globalThis.AbortSignal))
        throw new TypeError('signal must be AbortSignal');
      if (parent.aborted) controller.abort(parent.reason);
      else parent.addEventListener('abort', () => controller.abort(parent.reason), { once: true });
    }
    return controller.signal;
  }
  class FetchRequest {
    constructor(input, init = {}) {
      if (arguments.length === 0) throw new TypeError('Request requires input');
      const previous = requests.get(input),
        url = new globalThis.URL(previous ? previous.url : String(input), fetchBaseURL());
      if (url.username || url.password) throw new TypeError('Request URL contains credentials');
      let method = String(init.method === undefined ? previous?.method || 'GET' : init.method);
      if (
        !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(method) ||
        ['CONNECT', 'TRACE', 'TRACK'].includes(method.toUpperCase())
      )
        throw new TypeError('Invalid HTTP method');
      if (normalizedMethods.has(method.toUpperCase())) method = method.toUpperCase();
      const mode = String(init.mode ?? previous?.mode ?? 'cors'),
        credentials = String(init.credentials ?? previous?.credentials ?? 'same-origin'),
        cache = String(init.cache ?? previous?.cache ?? 'default'),
        redirect = String(init.redirect ?? previous?.redirect ?? 'follow');
      if (
        !['cors', 'same-origin', 'no-cors', 'navigate'].includes(mode) ||
        (mode === 'navigate' && init.mode !== undefined)
      )
        throw new TypeError('Invalid request mode');
      if (
        !['omit', 'same-origin', 'include'].includes(credentials) ||
        !['default', 'no-store', 'reload', 'no-cache', 'force-cache', 'only-if-cached'].includes(
          cache,
        ) ||
        !['follow', 'error', 'manual'].includes(redirect)
      )
        throw new TypeError('Invalid request option');
      if (cache === 'only-if-cached' && mode !== 'same-origin')
        throw new TypeError('only-if-cached requires same-origin');
      if (mode === 'no-cors' && !['GET', 'HEAD', 'POST'].includes(method))
        throw new TypeError('Invalid no-cors method');
      const supplied = init.body != null;
      const keepalive = Boolean(init.keepalive ?? previous?.keepalive ?? false);
      let body;
      if (supplied) body = extract(init.body);
      else if (previous) {
        const old = slot(bodies, input);
        if (unusable(old)) throw new TypeError('Request body already used');
        body = { ...old };
      } else body = extract(null);
      if (keepalive && body.stream !== null && body.bytes === undefined)
        throw new TypeError('A keepalive request cannot have a streaming body');
      if (['GET', 'HEAD'].includes(method) && body.stream !== null)
        throw new TypeError('GET/HEAD request cannot have body');
      if (supplied && init.body instanceof Streams && init.duplex !== 'half')
        throw new TypeError('Streaming body requires duplex half');
      if (init.duplex !== undefined && init.duplex !== 'half')
        throw new TypeError('Invalid duplex');
      const list = headers(
        init.headers === undefined ? previous?.headers : init.headers,
        'request',
      );
      if (body.type && !list.has('content-type')) list.set('content-type', body.type);
      const signal = dependentSignal(init.signal === undefined ? previous?.signal : init.signal),
        referrer = String(init.referrer ?? previous?.referrer ?? 'about:client');
      if (previous && !supplied && body.stream !== null)
        body.stream = body.stream.pipeThrough(new globalThis.TransformStream());
      requests.set(this, {
        url: url.href,
        method,
        headers: list,
        mode,
        credentials,
        cache,
        redirect,
        referrer,
        referrerPolicy: String(init.referrerPolicy ?? previous?.referrerPolicy ?? ''),
        integrity: String(init.integrity ?? previous?.integrity ?? ''),
        keepalive,
        signal,
        destination: '',
        duplex: 'half',
      });
      bodies.set(this, body);
    }
    clone() {
      const record = slot(requests, this),
        body = cloneBody(this),
        copy = Object.create(FetchRequest.prototype);
      requests.set(copy, {
        ...record,
        headers: headers(record.headers, 'request'),
        signal: dependentSignal(record.signal),
      });
      bodies.set(copy, body);
      return copy;
    }
  }
  for (const name of [
    'url',
    'method',
    'headers',
    'mode',
    'credentials',
    'cache',
    'redirect',
    'referrer',
    'referrerPolicy',
    'integrity',
    'keepalive',
    'signal',
    'destination',
    'duplex',
  ])
    Object.defineProperty(FetchRequest.prototype, name, {
      get() {
        return slot(requests, this)[name];
      },
      configurable: true,
      enumerable: true,
    });
  bodyMethods(FetchRequest.prototype);
  Object.defineProperty(FetchRequest, 'name', { value: 'Request' });
  Object.defineProperty(FetchRequest.prototype, Symbol.toStringTag, {
    value: 'Request',
    configurable: true,
  });
  // Existing Blob methods close over the old stream implementation, so replace
  // this boundary as well; all body streams then belong to the same realm family.
  Object.defineProperty(NativeBlob.prototype, 'stream', {
    value: function () {
      const blob = this;
      return new Streams({
        type: 'bytes',
        async start(controller) {
          try {
            const value = new Uint8Array(await blob.arrayBuffer());
            if (value.length) controller.enqueue(value);
            controller.close();
          } catch (error) {
            controller.error(error);
          }
        },
      });
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  let nextFetchID = 0;
  async function fetch(input, init = {}) {
    const request = new FetchRequest(input, init),
      signal = request.signal,
      id = 'fetch-' + ++nextFetchID;
    if (signal.aborted) throw signal.reason;
    let abortListener;
    const aborted = new platformPromise((resolve, reject) => {
      abortListener = () => {
        if (typeof host.abortFetch === 'function') host.abortFetch(id);
        reject(signal.reason);
      };
      signal.addEventListener('abort', abortListener, { once: true });
    });
    try {
      const operation = (async () => {
        const body = await consume(request);
        if (signal.aborted) throw signal.reason;
        let raw;
        try {
          raw = await host.fetch(
            request.url,
            request.method,
            Object.fromEntries(request.headers),
            Array.from(body),
            id,
            {
              credentials: request.credentials,
              mode: request.mode,
              cache: request.cache,
              redirect: request.redirect,
              referrer: request.referrer,
              keepalive: request.keepalive,
            },
          );
        } catch (error) {
          if (signal.aborted) throw signal.reason;
          throw new TypeError('Failed to fetch');
        }
        if (signal.aborted) throw signal.reason;
        const bodyBytes =
          raw.bodyBytes !== undefined
            ? new Uint8Array(raw.bodyBytes)
            : new Encoder().encode(raw.body ?? '');
        const list = headers(raw.headers, 'response');
        guards.set(list, 'immutable');
        return createResponse(
          {
            status: raw.status,
            statusText: raw.statusText || '',
            url: raw.type === 'opaque' ? '' : raw.url || request.url,
            headers: list,
            type: raw.type || 'basic',
            redirected: Boolean(raw.redirected),
          },
          nullBodyStatus(raw.status) ||
            request.method === 'HEAD' ||
            ['opaque', 'opaqueredirect', 'error'].includes(raw.type)
            ? { stream: null, type: null }
            : responseBody(bodyBytes, signal, list.get('content-type'), raw.bodyUnavailable),
        );
      })();
      return await platformPromiseRace([operation, aborted]);
    } finally {
      signal.removeEventListener('abort', abortListener);
    }
  }
  // Decoder state spans chunks; the upstream TransformStream supplies error
  // propagation, cancellation and backpressure rather than a second queue.
  const decoderStreams = new WeakMap();
  class TextDecoderStream {
    constructor(label = 'utf-8', options = {}) {
      const decoder = new Decoder(label, options);
      const stream = new globalThis.TransformStream({
        transform(chunk, controller) {
          if (!(chunk instanceof ArrayBuffer) && !ArrayBuffer.isView(chunk))
            throw new TypeError('Expected a BufferSource');
          const text = decoder.decode(chunk, { stream: true });
          if (text) controller.enqueue(text);
        },
        flush(controller) {
          const text = decoder.decode();
          if (text) controller.enqueue(text);
        },
      });
      decoderStreams.set(this, { decoder, stream });
    }
  }
  for (const name of ['encoding', 'fatal', 'ignoreBOM', 'readable', 'writable'])
    Object.defineProperty(TextDecoderStream.prototype, name, {
      get() {
        const state = slot(decoderStreams, this);
        return name === 'readable' || name === 'writable'
          ? state.stream[name]
          : state.decoder[name];
      },
      configurable: true,
      enumerable: true,
    });
  Object.defineProperty(TextDecoderStream.prototype, Symbol.toStringTag, {
    value: 'TextDecoderStream',
    configurable: true,
  });
  for (const [name, value] of Object.entries({
    Headers: FetchHeaders,
    Request: FetchRequest,
    Response: FetchResponse,
    TextDecoderStream,
    fetch,
  }))
    Object.defineProperty(globalThis, name, { value, writable: true, configurable: true });
  /* cache_storage */
}
