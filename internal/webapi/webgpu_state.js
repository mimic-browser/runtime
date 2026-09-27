// Resource and queue semantics without a native WebGPU backend.
(() => {
  if (typeof globalThis.GPUDevice !== 'function' || typeof globalThis.GPUAdapter !== 'function')
    return;
  const slots = new WeakMap(),
    nativeTransfer = ArrayBuffer.prototype.transfer;
  /* shared_glsl_observations */
  /* shared_wgsl_observations */
  const unsupported = (name) => {
    host.semanticMissingAt('webgpu_state.js:5', 'WebGPU.' + name);
    throw platformDOMException('Unsupported WebGPU operation: ' + name, 'NotSupportedError');
  };
  const check = (value, type) => {
    const s = slots.get(value);
    if (
      !s ||
      (s.type !== type &&
        !(
          type === 'GPUError' &&
          ['GPUValidationError', 'GPUOutOfMemoryError', 'GPUInternalError'].includes(s.type)
        ))
    )
      throw new TypeError('Illegal invocation');
    return s;
  };
  const make = (type, data = {}) => {
    const value = Object.create(globalThis[type].prototype);
    slots.set(value, { type, label: '', ...data });
    return value;
  };
  const method = (type, name, fn) => {
    const proto = globalThis[type]?.prototype;
    if (!proto) return;
    const prior = Object.getOwnPropertyDescriptor(proto, name),
      value = {
        [name](...args) {
          const s = check(this, type);
          if (args.length < (prior?.value?.length || 0))
            throw new TypeError('Not enough arguments');
          return fn(s, ...args);
        },
      }[name];
    if (prior?.value) Object.defineProperty(value, 'length', { value: prior.value.length });
    if (typeof markNative === 'function') markNative(value, name);
    if (
      typeof lazyDomainInterfaces !== 'undefined' &&
      lazyDomainInterfaces.get(type) === 'webgpu'
    ) {
      if (bindLazyBehavior(type, name, value)) return;
    }
    Object.defineProperty(proto, name, {
      value,
      enumerable: true,
      configurable: true,
      writable: true,
    });
  };
  const getter = (type, name, fn, set) => {
    const proto = globalThis[type]?.prototype;
    if (!proto) return;
    const get = function () {
      return fn(check(this, type));
    };
    if (typeof markNative === 'function') markNative(get, name, 'get ');
    const d = { get, enumerable: true, configurable: true };
    if (set)
      d.set = function (v) {
        set(check(this, type), v);
      };
    if (
      typeof lazyDomainInterfaces !== 'undefined' &&
      lazyDomainInterfaces.get(type) === 'webgpu'
    ) {
      const boundGet = bindLazyBehavior(type, name, get, 'get');
      const boundSet = !d.set || bindLazyBehavior(type, name, d.set, 'set');
      if (boundGet && boundSet) return;
    }
    Object.defineProperty(proto, name, d);
  };
  const detach = (buffer) => {
    if (nativeTransfer) Reflect.apply(nativeTransfer, buffer, [0]);
    else if (host.detachArrayBuffer) host.detachArrayBuffer(buffer);
    else unsupported('ArrayBuffer detachment');
  };
  const number = (value) => {
    const n = Number(value);
    if (!Number.isSafeInteger(n) || n < 0) throw new TypeError('Expected unsigned integer');
    return n;
  };
  const operation = (message) => platformDOMException(message, 'OperationError');
  const validation = (device, message) => {
    if (device.destroyed) return;
    const value = make('GPUValidationError', { message });
    for (let i = device.scopes.length - 1; i >= 0; i--)
      if (device.scopes[i].filter === 'validation') {
        device.scopes[i].error ||= value;
        return;
      }
    setTimeout(() => {
      if (device.destroyed) return;
      const event = new Event('uncapturederror');
      Object.defineProperty(event, 'error', { value, enumerable: true });
      device.object.dispatchEvent(event);
    }, 0);
  };
  for (const type of [
    'GPUDevice',
    'GPUQueue',
    'GPUBuffer',
    'GPUCommandEncoder',
    'GPUCommandBuffer',
  ]) {
    const proto = globalThis[type]?.prototype;
    if (!proto) continue;
    for (const name of Object.getOwnPropertyNames(proto))
      if (
        name !== 'constructor' &&
        typeof Object.getOwnPropertyDescriptor(proto, name).value === 'function'
      )
        method(type, name, () => unsupported(type + '.' + name));
    getter(
      type,
      'label',
      (s) => s.label,
      (s, v) => {
        s.label = String(v);
      },
    );
  }
  getter('GPUError', 'message', (s) => s.message);
  getter('GPUDeviceLostInfo', 'reason', (s) => s.reason);
  getter('GPUDeviceLostInfo', 'message', (s) => s.message);
  const features = (values) => make('GPUSupportedFeatures', { values: new Set(values) });
  getter('GPUSupportedFeatures', 'size', (s) => s.values.size);
  for (const name of ['has', 'keys', 'values', 'entries'])
    method('GPUSupportedFeatures', name, (s, ...args) => s.values[name](...args));
  method('GPUSupportedFeatures', 'forEach', (s, callback, thisArg) => {
    if (typeof callback !== 'function') throw new TypeError('Expected callback');
    s.values.forEach((v, k) => callback.call(thisArg, v, k, s.object));
  });
  const featureIterator = function values() {
    return check(this, 'GPUSupportedFeatures').values.values();
  };
  if (
    typeof lazyDomainInterfaces !== 'undefined' &&
    lazyDomainInterfaces.get('GPUSupportedFeatures') === 'webgpu'
  )
    bindLazyBehavior('GPUSupportedFeatures', '@@iterator', featureIterator);
  else
    Object.defineProperty(GPUSupportedFeatures.prototype, Symbol.iterator, {
      value: featureIterator,
      writable: true,
      configurable: true,
    });
  const gpuCapabilities = host.gpuCapabilities(),
    gpuLimitDefaults = gpuCapabilities.defaults;
  for (const name of Object.keys(gpuLimitDefaults))
    getter('GPUSupportedLimits', name, (s) => s.values[name]);
  for (const name of ['vendor', 'architecture', 'device', 'description'])
    getter('GPUAdapterInfo', name, (s) => s.values[name] || '');
  for (const name of ['subgroupMinSize', 'subgroupMaxSize'])
    getter('GPUAdapterInfo', name, (s) => s.values[name]);
  getter('GPUAdapterInfo', 'isFallbackAdapter', (s) => s.values.isFallbackAdapter);
  const info = (values) => make('GPUAdapterInfo', { values: { ...values } }),
    limits = (values) => make('GPUSupportedLimits', { values: { ...values } });
  const makeFeatures = (values) => {
    const object = features(values);
    slots.get(object).object = object;
    return object;
  };
  for (const name of ['info', 'features', 'limits']) getter('GPUAdapter', name, (s) => s[name]);
  getter(
    'GPUAdapter',
    'isFallbackAdapter',
    (s) => check(s.info, 'GPUAdapterInfo').values.isFallbackAdapter,
  );
  // The navigator owns one canonical GPU object; adapters/devices own their views.
  const gpu = make('GPU');
  if (typeof publishLazySingleton === 'function') publishLazySingleton('webgpu', 'gpu', gpu);
  method('GPU', 'getPreferredCanvasFormat', () => 'bgra8unorm');
  if (globalThis.WGSLLanguageFeatures?.prototype) {
    const languageFeatures = make('WGSLLanguageFeatures', {
      values: new Set(gpuCapabilities.wgsl || []),
    });
    getter('GPU', 'wgslLanguageFeatures', () => languageFeatures);
    getter('WGSLLanguageFeatures', 'size', (s) => s.values.size);
    for (const name of ['has', 'keys', 'values', 'entries'])
      method('WGSLLanguageFeatures', name, (s, ...args) => s.values[name](...args));
    method('WGSLLanguageFeatures', 'forEach', (s, callback, thisArg) => {
      if (typeof callback !== 'function') throw new TypeError('Expected callback');
      s.values.forEach((v, k) => Reflect.apply(callback, thisArg, [v, k, languageFeatures]));
    });
    const iterator = function values() {
      return check(this, 'WGSLLanguageFeatures').values.values();
    };
    if (
      typeof lazyDomainInterfaces !== 'undefined' &&
      lazyDomainInterfaces.get('WGSLLanguageFeatures') === 'webgpu'
    )
      bindLazyBehavior('WGSLLanguageFeatures', '@@iterator', iterator);
    else
      Object.defineProperty(WGSLLanguageFeatures.prototype, Symbol.iterator, {
        value: iterator,
        writable: true,
        configurable: true,
      });
  }
  method('GPU', 'requestAdapter', (_, options = {}) => {
    options = options ?? {};
    const preference = options.powerPreference === undefined ? '' : String(options.powerPreference);
    if (preference && !['low-power', 'high-performance'].includes(preference))
      return platformPromiseReject(new TypeError('Invalid powerPreference'));
    return host
      .gpuRequestAdapter(preference, options.forceFallbackAdapter ? 'fallback' : '')
      .then((g) =>
        g === null
          ? null
          : make('GPUAdapter', {
              info: info(g),
              features: makeFeatures(g.features || []),
              limits: limits(g.limits || gpuCapabilities.limits),
              requested: false,
            }),
      );
  });
  const navProto =
    typeof WorkerNavigator === 'function' ? WorkerNavigator.prototype : Navigator.prototype;
  const secure =
    typeof host.isSecureContext === 'function'
      ? host.isSecureContext()
      : host.documentSecurity().secureContext;
  if (secure && typeof publishLazySingleton === 'undefined') {
    // Capture the owning navigator: inheriting its prototype does not confer its brand.
    const owner = globalThis.navigator;
    const get = Object.getOwnPropertyDescriptor(
      {
        get gpu() {
          if (this !== owner) throw new TypeError('Illegal invocation');
          return gpu;
        },
      },
      'gpu',
    ).get;
    if (typeof markNative === 'function') markNative(get, 'gpu', 'get ');
    Object.defineProperty(navProto, 'gpu', { get, enumerable: true, configurable: true });
  }
  method('GPUAdapter', 'requestDevice', (adapter, descriptor = {}) => {
    if (adapter.requested) return platformPromiseReject(operation('Adapter already used'));
    const requested = Array.from(descriptor.requiredFeatures || [], String),
      available = slots.get(adapter.features).values;
    if (requested.some((f) => !available.has(f)))
      return platformPromiseReject(new TypeError('Unsupported feature'));
    const values = { ...gpuLimitDefaults },
      supported = slots.get(adapter.limits).values;
    for (const [name, value] of Object.entries(descriptor.requiredLimits || {})) {
      if (!(name in values)) return platformPromiseReject(operation('Unknown limit'));
      const n = number(value),
        minimum = name.startsWith('min');
      if (minimum ? n < supported[name] : n > supported[name])
        return platformPromiseReject(operation('Unsupported limit'));
      values[name] = minimum ? Math.min(values[name], n) : Math.max(values[name], n);
    }
    adapter.requested = true;
    let resolveLost;
    const object = make('GPUDevice', {
      label: String(descriptor.label || ''),
      features: makeFeatures(requested),
      limits: limits(values),
      adapterInfo: info(slots.get(adapter.info).values),
      scopes: [],
      destroyed: false,
      mapped: new Set(),
      lost: new platformPromise((resolve) => {
        resolveLost = resolve;
      }),
      resolveLost: null,
      onuncapturederror: null,
    });
    const device = slots.get(object);
    device.object = object;
    device.resolveLost = resolveLost;
    device.queue = make('GPUQueue', { device });
    return platformPromiseResolve(object);
  });
  for (const name of ['features', 'limits', 'adapterInfo', 'queue', 'lost'])
    getter('GPUDevice', name, (s) => s[name]);
  getter(
    'GPUDevice',
    'onuncapturederror',
    (s) => s.onuncapturederror,
    (s, v) => {
      s.onuncapturederror = typeof v === 'function' ? v : null;
    },
  );
  method('GPUDevice', 'pushErrorScope', (s, filter) => {
    filter = String(filter);
    if (!['validation', 'out-of-memory', 'internal'].includes(filter))
      throw new TypeError('Invalid error filter');
    s.scopes.push({ filter, error: null });
  });
  method('GPUDevice', 'popErrorScope', (s) =>
    s.scopes.length
      ? platformPromiseResolve(s.scopes.pop().error)
      : platformPromiseReject(operation('No error scope')),
  );
  const unmap = (b) => {
    if (b.pending) {
      b.pending.reject(platformDOMException('Mapping aborted', 'AbortError'));
      b.pending = null;
    }
    for (const range of b.ranges) {
      if (b.mode === 2 && !b.destroyed && !b.device.destroyed)
        b.bytes.set(new Uint8Array(range.buffer), range.offset);
      detach(range.buffer);
    }
    b.ranges = [];
    b.mapState = 'unmapped';
    b.device.mapped.delete(b);
  };
  method('GPUDevice', 'destroy', (s) => {
    if (s.destroyed) return;
    s.destroyed = true;
    for (const b of s.mapped) unmap(b);
    s.resolveLost(make('GPUDeviceLostInfo', { reason: 'destroyed', message: 'Device destroyed' }));
  });
  method('GPUDevice', 'createBuffer', (s, descriptor) => {
    if (!descriptor || descriptor.size === undefined || descriptor.usage === undefined)
      throw new TypeError('Missing buffer descriptor');
    const size = number(descriptor.size),
      usage = Number(descriptor.usage) >>> 0,
      mapped = !!descriptor.mappedAtCreation;
    if (mapped && size % 4) throw new RangeError('Mapped size must be a multiple of four');
    if (size > 64 * 1024 * 1024) unsupported('buffer storage limit');
    if (mapped && !nativeTransfer && !host.detachArrayBuffer)
      unsupported('mapped buffer detachment');
    const valid =
      !s.destroyed &&
      usage > 0 &&
      !(usage & ~1023) &&
      (!(usage & 1) || !(usage & ~9)) &&
      (!(usage & 2) || !(usage & ~6));
    if (!valid) validation(s, 'Invalid buffer usage');
    const object = make('GPUBuffer', {
      device: s,
      size,
      usage,
      label: String(descriptor.label || ''),
      bytes: new Uint8Array(size),
      valid,
      destroyed: false,
      mapState: mapped ? 'mapped' : 'unmapped',
      mode: mapped ? 2 : 0,
      mapOffset: 0,
      mapSize: size,
      ranges: [],
      pending: null,
    });
    if (mapped) s.mapped.add(slots.get(object));
    return object;
  });
  for (const name of ['size', 'usage', 'mapState']) getter('GPUBuffer', name, (s) => s[name]);
  method('GPUBuffer', 'getMappedRange', (b, offset = 0, size) => {
    if (!nativeTransfer && !host.detachArrayBuffer) unsupported('mapped buffer detachment');
    offset = number(offset);
    size = size === undefined ? b.size - offset : number(size);
    if (
      b.mapState !== 'mapped' ||
      offset % 8 ||
      size % 4 ||
      offset < b.mapOffset ||
      offset + size > b.mapOffset + b.mapSize ||
      b.ranges.some((r) => offset < r.offset + r.size && offset + size > r.offset)
    )
      throw operation('Invalid mapped range');
    const buffer = b.bytes.slice(offset, offset + size).buffer;
    b.ranges.push({ offset, size, buffer });
    return buffer;
  });
  method('GPUBuffer', 'unmap', unmap);
  method('GPUBuffer', 'destroy', (b) => {
    b.destroyed = true;
    unmap(b);
    b.bytes = new Uint8Array(0);
  });
  method('GPUBuffer', 'mapAsync', (b, mode, offset = 0, size) => {
    mode = Number(mode) >>> 0;
    offset = number(offset);
    size = size === undefined ? b.size - offset : number(size);
    if (b.mapState !== 'unmapped')
      return platformPromiseReject(operation('Buffer already mapped or pending'));
    if (
      !b.valid ||
      b.destroyed ||
      b.device.destroyed ||
      ![1, 2].includes(mode) ||
      !(b.usage & mode) ||
      offset % 8 ||
      size % 4 ||
      offset + size > b.size
    ) {
      validation(b.device, 'Invalid buffer mapping');
      return platformPromiseReject(operation('Invalid mapping'));
    }
    b.mapState = 'pending';
    b.mode = mode;
    b.mapOffset = offset;
    b.mapSize = size;
    b.device.mapped.add(b);
    return new platformPromise((resolve, reject) => {
      const pending = { resolve, reject };
      b.pending = pending;
      setTimeout(() => {
        if (b.pending !== pending) return;
        b.pending = null;
        b.mapState = 'mapped';
        resolve();
      }, 0);
    });
  });
  method('GPUDevice', 'createCommandEncoder', (s, descriptor = {}) =>
    make('GPUCommandEncoder', {
      device: s,
      label: String(descriptor.label || ''),
      commands: [],
      finished: false,
      valid: !s.destroyed,
    }),
  );
  const available = (b, device, usage) =>
    b.device === device &&
    b.valid &&
    !b.destroyed &&
    b.mapState === 'unmapped' &&
    !!(b.usage & usage);
  method(
    'GPUCommandEncoder',
    'copyBufferToBuffer',
    (e, source, sourceOffset, destination, destinationOffset, size) => {
      const a = check(source, 'GPUBuffer'),
        b = check(destination, 'GPUBuffer');
      sourceOffset = number(sourceOffset);
      destinationOffset = number(destinationOffset);
      size = number(size);
      if (
        e.finished ||
        a === b ||
        a.device !== e.device ||
        b.device !== e.device ||
        !(a.usage & 4) ||
        !(b.usage & 8) ||
        sourceOffset % 4 ||
        destinationOffset % 4 ||
        size % 4 ||
        sourceOffset + size > a.size ||
        destinationOffset + size > b.size
      ) {
        validation(e.device, 'Invalid buffer copy');
        e.valid = false;
        return;
      }
      e.commands.push({ kind: 'copy', a, b, sourceOffset, destinationOffset, size });
    },
  );
  method('GPUCommandEncoder', 'clearBuffer', (e, buffer, offset = 0, size) => {
    const b = check(buffer, 'GPUBuffer');
    offset = number(offset);
    size = size === undefined ? b.size - offset : number(size);
    if (
      e.finished ||
      b.device !== e.device ||
      !(b.usage & 8) ||
      offset % 4 ||
      size % 4 ||
      offset + size > b.size
    ) {
      validation(e.device, 'Invalid buffer clear');
      e.valid = false;
      return;
    }
    e.commands.push({ kind: 'clear', b, destinationOffset: offset, size });
  });
  method('GPUCommandEncoder', 'finish', (e, descriptor = {}) => {
    if (e.finished || e.passActive) {
      validation(
        e.device,
        e.passActive ? 'Render pass is still active' : 'Encoder already finished',
      );
      e.valid = false;
    }
    e.finished = true;
    return make('GPUCommandBuffer', {
      device: e.device,
      label: String(descriptor.label || ''),
      commands: e.commands.slice(),
      valid: e.valid,
      submitted: false,
    });
  });
  method('GPUQueue', 'submit', (q, values) => {
    const commands = Array.from(values, (v) => check(v, 'GPUCommandBuffer')),
      seen = new Set();
    for (const c of commands) {
      if (
        c.device !== q.device ||
        !c.valid ||
        c.submitted ||
        seen.has(c) ||
        c.commands.some((op) =>
          op.textureCommand
            ? !validateTextureCommand(op, q.device)
            : !available(op.b, q.device, 8) || (op.a && !available(op.a, q.device, 4)),
        )
      ) {
        validation(q.device, 'Invalid submission');
        return;
      }
      seen.add(c);
    }
    if (q.device.destroyed) return;
    for (const c of commands) {
      c.submitted = true;
      for (const op of c.commands)
        if (op.textureCommand) executeTextureCommand(op);
        else if (op.kind === 'copy')
          op.b.bytes.set(
            op.a.bytes.subarray(op.sourceOffset, op.sourceOffset + op.size),
            op.destinationOffset,
          );
        else op.b.bytes.fill(0, op.destinationOffset, op.destinationOffset + op.size);
    }
  });
  method('GPUQueue', 'onSubmittedWorkDone', () => platformPromiseResolve());
  method('GPUQueue', 'writeBuffer', (q, buffer, offset, data, dataOffset = 0, size) => {
    const b = check(buffer, 'GPUBuffer');
    offset = number(offset);
    dataOffset = number(dataOffset);
    const view = ArrayBuffer.isView(data);
    if (!view && !(data instanceof ArrayBuffer)) throw new TypeError('Expected buffer source');
    const scale = view && !(data instanceof DataView) ? data.BYTES_PER_ELEMENT : 1,
      bytes = view
        ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
        : new Uint8Array(data),
      start = dataOffset * scale,
      length = size === undefined ? bytes.length - start : number(size) * scale;
    if (start + length > bytes.length || length < 0 || length % 4)
      throw operation('Invalid source range');
    if (!available(b, q.device, 8) || offset % 4 || offset + length > b.size) {
      validation(q.device, 'Invalid buffer write');
      return;
    }
    b.bytes.set(bytes.subarray(start, start + length), offset);
  });
  /* shared_webgpu_render */
})();
