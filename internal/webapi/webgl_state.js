// WebGL resource state and bounded, query-time arithmetic observations.
// Native graphics, unsupported language and rendering features stay explicit.
(() => {
  const slots = new WeakMap(),
    resourceSlots = new WeakMap(),
    resources = {
      get: (value) => resourceSlots.get(value),
      set(value, record) {
        record.generation = record.owner.generation || 0;
        resourceSlots.set(value, record);
      },
    },
    precisionFormats = new WeakMap(),
    activeInfos = new WeakMap();
  if (typeof WebGLActiveInfo === 'function')
    for (const name of ['name', 'size', 'type']) {
      const get = function () {
        const data = activeInfos.get(this);
        if (!data) throw new TypeError('Illegal invocation');
        return data[name];
      };
      if (typeof markNative === 'function') markNative(get, name, 'get ');
      if (
        typeof lazyDomainInterfaces !== 'undefined' &&
        lazyDomainInterfaces.get('WebGLActiveInfo') === 'webgl'
      ) {
        bindLazyBehavior('WebGLActiveInfo', name, get, 'get');
        continue;
      }
      Object.defineProperty(WebGLActiveInfo.prototype, name, {
        get,
        enumerable: true,
        configurable: true,
      });
    }
  /* shared_glsl_observations */
  if (typeof WebGLShaderPrecisionFormat === 'function')
    for (const name of ['rangeMin', 'rangeMax', 'precision']) {
      const get = function () {
        const data = precisionFormats.get(this);
        if (!data) throw new TypeError('Illegal invocation');
        return data[name];
      };
      if (typeof markNative === 'function') markNative(get, name, 'get ');
      if (
        typeof lazyDomainInterfaces !== 'undefined' &&
        lazyDomainInterfaces.get('WebGLShaderPrecisionFormat') === 'webgl'
      ) {
        bindLazyBehavior('WebGLShaderPrecisionFormat', name, get, 'get');
        continue;
      }
      Object.defineProperty(WebGLShaderPrecisionFormat.prototype, name, {
        get,
        enumerable: true,
        configurable: true,
      });
    }
  const graphics = {},
    capabilities = {};
  const refreshGraphics = () => {
    const selected = typeof host.graphics === 'function' ? host.graphics() : {};
    Object.assign(graphics, selected);
    const next = JSON.parse(selected.capabilitiesJSON || '{}');
    for (const kind of ['webgl', 'webgl2']) {
      const current = (capabilities[kind] ||= {});
      for (const key of Object.keys(current)) delete current[key];
      Object.assign(current, next[kind] || {});
    }
  };
  refreshGraphics();
  if (typeof bootstrapRestoreHooks !== 'undefined') bootstrapRestoreHooks.push(refreshGraphics);
  const fail = (name) => {
    host.semanticMissingAt('webgl_state.js:9', 'WebGL.' + name);
    throw platformDOMException(
      'WebGL ' + name + ' requires an unsupported graphics operation.',
      'NotSupportedError',
    );
  };
  const check = (value) => {
    const s = slots.get(value);
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const error = (s, code) => {
    if (!s.error) s.error = code;
  };
  const resource = (s, value, type) => {
    const r = resources.get(value);
    if (!r || r.type !== type) throw new TypeError('Expected ' + type);
    if (r.owner !== s || r.deleted || r.generation !== (s.generation || 0)) {
      error(s, 1282);
      return null;
    }
    return r;
  };
  const bytes = (value) =>
    ArrayBuffer.isView(value)
      ? new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
      : value instanceof ArrayBuffer
        ? new Uint8Array(value)
        : null;
  const define = (proto, name, fn) => {
    const d = Object.getOwnPropertyDescriptor(proto, name);
    const f = {
      [name](...args) {
        const s = check(this);
        if (args.length < (d?.value?.length || 0)) throw new TypeError('Not enough arguments');
        if (s.lost && !['getError', 'isContextLost'].includes(name) && !name.startsWith('create'))
          return name.startsWith('is') ? false : name.startsWith('get') ? null : undefined;
        if (name.startsWith('is') && args[0]) {
          const r = resources.get(args[0]);
          if (r && r.generation !== (s.generation || 0)) return false;
        }
        return fn(s, ...args);
      },
    }[name];
    if (d?.value) Object.defineProperty(f, 'length', { value: d.value.length });
    if (typeof markNative === 'function') markNative(f, name);
    const type =
      proto === globalThis.WebGL2RenderingContext?.prototype
        ? 'WebGL2RenderingContext'
        : 'WebGLRenderingContext';
    if (typeof lazyDomainInterfaces !== 'undefined' && lazyDomainInterfaces.get(type) === 'webgl') {
      bindLazyBehavior(type, name, f);
      return;
    }
    Object.defineProperty(proto, name, {
      value: f,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  for (const [kind, type] of [
    ['webgl', 'WebGLRenderingContext'],
    ['webgl2', 'WebGL2RenderingContext'],
  ]) {
    const proto = globalThis[type]?.prototype;
    if (!proto) continue;
    const knownEnums = new Set();
    for (let p = proto; p && p !== Object.prototype; p = Object.getPrototypeOf(p))
      for (const key of Object.getOwnPropertyNames(p)) {
        const d = Object.getOwnPropertyDescriptor(p, key);
        if (typeof d.value === 'number') knownEnums.add(d.value);
      }
    // Replace generated successful placeholders with an observable boundary.
    for (const name of Object.getOwnPropertyNames(proto)) {
      const d = Object.getOwnPropertyDescriptor(proto, name);
      if (name !== 'constructor' && typeof d.value === 'function')
        define(proto, name, () => fail(name));
    }
    const method = (name, fn) => define(proto, name, fn);
    // Context metadata survives drawing-buffer resize; independent contexts
    // keep independent values. Pixel color conversion is a separate boundary.
    for (const name of ['drawingBufferColorSpace', 'unpackColorSpace']) {
      const get = function () {
          return check(this)[name] || 'srgb';
        },
        set = function (value) {
          const s = check(this);
          if (typeof value === 'symbol')
            throw new TypeError('Cannot convert a Symbol value to a string');
          value = String(value);
          if (value === 'srgb' || value === 'display-p3') s[name] = value;
        };
      Object.defineProperty(get, 'name', { value: 'get ' + name, configurable: true });
      Object.defineProperty(set, 'name', { value: 'set ' + name, configurable: true });
      if (typeof markNative === 'function') {
        markNative(get, name, 'get ');
        markNative(set, name, 'set ');
      }
      Object.defineProperty(proto, name, { get, set, enumerable: true, configurable: true });
    }
    for (const [name, get] of Object.entries({
      canvas: (s) => s.canvas,
      drawingBufferWidth: (s) => s.dim.width,
      drawingBufferHeight: (s) => s.dim.height,
    }))
      Object.defineProperty(proto, name, {
        get() {
          return get(check(this));
        },
        enumerable: true,
        configurable: true,
      });
    method('getError', (s) => {
      const code = s.error;
      s.error = 0;
      return code;
    });
    method('isContextLost', (s) => !!s.lost);
    method('getContextAttributes', (s) => ({ ...s.attributes }));
    const profile = capabilities[kind] || { parameters: {}, samples: {}, floatSamples: {} };
    /* shared_webgl_extensions */
    /* shared_webgl_vertex_arrays */
    const supportedExtensions = () => {
      const observed = Array.isArray(profile.extensions) ? new Set(profile.extensions) : null;
      return [...extensionDefinitions.keys()].filter((name) => !observed || observed.has(name));
    };
    method('getSupportedExtensions', () => supportedExtensions().sort());
    method('getExtension', (s, name) => {
      name = String(name).toLowerCase();
      const canonical = supportedExtensions().find((value) => value.toLowerCase() === name);
      if (!canonical) return null;
      if (s.extensions.has(canonical)) return s.extensions.get(canonical);
      const d = extensionDefinitions.get(canonical),
        cache = s.extensionObjects || (s.extensionObjects = new Map()),
        value = cache.get(canonical) || Object.create(d.prototype);
      cache.set(canonical, value);
      extensionSlots.set(value, { owner: s, name: canonical });
      s.extensions.set(canonical, value);
      d.enable(s);
      return value;
    });
    const parameterValue = (entry) => {
      if (entry.type === 'Int32Array') return new Int32Array(entry.value);
      if (entry.type === 'Float32Array') return new Float32Array(entry.value);
      if (entry.type === 'Uint32Array') return new Uint32Array(entry.value);
      return entry.value;
    };
    if (kind === 'webgl2')
      method('getInternalformatParameter', (s, target, format, pname) => {
        target = Number(target) >>> 0;
        format = Number(format) >>> 0;
        pname = Number(pname) >>> 0;
        if (target !== 36161 || pname !== 32937) {
          error(s, 1280);
          return null;
        }
        let values = profile.samples[format];
        if (
          values === undefined &&
          (s.extensions.has('EXT_color_buffer_float') ||
            (s.extensions.has('EXT_color_buffer_half_float') &&
              [33325, 33327, 34842].includes(format)))
        )
          values = profile.floatSamples[format];
        if (values === undefined) {
          error(s, 1280);
          return null;
        }
        return new Int32Array(values);
      });
    method('getShaderPrecisionFormat', (s, shader, precision) => {
      const data = profile.precision?.[(Number(shader) >>> 0) + ',' + (Number(precision) >>> 0)];
      if (!data) {
        error(s, 1280);
        return null;
      }
      const value = Object.create(WebGLShaderPrecisionFormat.prototype);
      precisionFormats.set(value, { ...data });
      return value;
    });
    method('getParameter', (s, p) => {
      p = Number(p) >>> 0;
      if (p === 34229 && (kind === 'webgl2' || s.extensions.has('OES_vertex_array_object')))
        return s.vertexArray || null;
      if (p === 34016) return 33984 + (s.activeTexture || 0);
      if ([32873, 34068, 32874, 35869].includes(p))
        return (
          textureUnit(s).get({ 32873: 3553, 34068: 34067, 32874: 32879, 35869: 35866 }[p]) || null
        );
      if (p === 3317) return s.unpackAlignment || 4;
      if (p === 36006) return s.drawFramebuffer || null;
      if (p === 36010 && kind === 'webgl2') return s.readFramebuffer || null;
      if (p === 36007) return s.renderbuffer || null;
      if (p === 35725) return s.program || null;
      if (p === 34964) return s.bindings.get(34962) || null;
      if (p === 34965) return bufferBinding(s, 34963) || null;
      // Chrome's masked API identification is independent of the selected GPU.
      // Unmasked machine identity continues to come from Environment.Graphics.
      if (p === 7936) return 'WebKit';
      if (p === 7937) return 'WebKit WebGL';
      if (p === 7938)
        return kind === 'webgl2'
          ? 'WebGL 2.0 (OpenGL ES 3.0 Chromium)'
          : 'WebGL 1.0 (OpenGL ES 2.0 Chromium)';
      if (p === 35724)
        return kind === 'webgl2'
          ? 'WebGL GLSL ES 3.00 (OpenGL ES GLSL ES 3.0 Chromium)'
          : 'WebGL GLSL ES 1.0 (OpenGL ES GLSL ES 1.0 Chromium)';
      if (p === 3106) return new Float32Array(s.color);
      if (p === 3107) return s.mask.slice();
      if (p === 2978) return new Int32Array(s.viewport);
      if (p === 3088) return new Int32Array(s.scissor);
      if (p === 3333) return s.pack;
      if (p === 2931) return s.depthClear;
      if (p === 2961) return s.stencilClear;
      if (p === 2930) return s.depthMask;
      if (p === 3379) return graphics.maxTextureSize;
      if (profile.parameters[p]) return parameterValue(profile.parameters[p]);
      if (p === 34467)
        return new Uint32Array(
          [...compressedFormats]
            .filter(([format, d]) => s.extensions.has(d.extension))
            .map(([format]) => format),
        );
      if (p === 35738) return 5121;
      if (p === 35739) return 6408;
      if ([3410, 3411, 3412].includes(p)) return 8;
      if (p === 3413) return s.attributes.alpha ? 8 : 0;
      if (p === 3414) return s.attributes.depth ? 24 : 0;
      if (p === 3415) return s.attributes.stencil ? 8 : 0;
      if (p === 32936) return s.attributes.antialias ? 1 : 0;
      if (p === 32937) return s.attributes.antialias ? 4 : 0;
      if (p === 2963 || p === 36004) return 4294967295;
      if (p === 2968) return s.stencilMaskFront;
      if (p === 36005) return s.stencilMaskBack;
      if (p === 34047) {
        if (s.extensions.has('EXT_texture_filter_anisotropic')) return profile.anisotropy;
        error(s, 1280);
        return null;
      }
      if (p === 35723) {
        if (kind === 'webgl2' || s.extensions.has('OES_standard_derivatives'))
          return s.derivativeHint;
        error(s, 1280);
        return null;
      }
      if (p === 36795) {
        if (s.extensions.has(timerName)) return false;
        error(s, 1280);
        return null;
      }
      if ((p === 37445 || p === 37446) && s.debug)
        return graphics[p === 37445 ? 'vendor' : 'renderer'];
      if (s.enabled.has(p)) return s.enabled.get(p);
      if (!knownEnums.has(p)) {
        error(s, 1280);
        return null;
      }
      return fail('getParameter(' + p + ')');
    });
    method('createBuffer', (s) => {
      const object = Object.create(WebGLBuffer.prototype);
      resources.set(object, {
        owner: s,
        type: 'WebGLBuffer',
        deleted: false,
        bound: false,
        target: 0,
        data: new Uint8Array(0),
        usage: 35044,
      });
      return object;
    });
    method('isBuffer', (s, value) => {
      const r = resources.get(value);
      return !!r && r.owner === s && r.type === 'WebGLBuffer' && !r.deleted && r.bound;
    });
    const target = (s, n) => {
      n = Number(n);
      if (
        n === 34962 ||
        n === 34963 ||
        (kind === 'webgl2' && [36662, 36663, 35051, 35052, 35982, 35345].includes(n))
      )
        return n;
      error(s, 1280);
      return 0;
    };
    method('bindBuffer', (s, t, value) => {
      t = target(s, t);
      if (!t) return;
      if (value === null) {
        setBufferBinding(s, t, null);
        return;
      }
      const r = resource(s, value, 'WebGLBuffer');
      if (!r) return;
      if (r.target && (r.target === 34963) !== (t === 34963)) {
        error(s, 1282);
        return;
      }
      r.target = t;
      r.bound = true;
      setBufferBinding(s, t, value);
    });
    method('deleteBuffer', (s, value) => {
      if (value === null) return;
      const r = resources.get(value);
      if (r?.owner === s && r.type === 'WebGLBuffer' && r.deleted) return;
      const own = resource(s, value, 'WebGLBuffer');
      if (!own) return;
      if (vertexState(s).element === value) vertexState(s).element = null;
      own.deleted = true;
      own.data = new Uint8Array(0);
      for (const [t, b] of s.bindings) if (b === value) setBufferBinding(s, t, null);
    });
    const bound = (s, t) => {
      t = target(s, t);
      if (!t) return null;
      const b = bufferBinding(s, t);
      if (!b) {
        error(s, 1282);
        return null;
      }
      return resources.get(b);
    };
    method('bufferData', (s, t, value, usage, ...extra) => {
      const b = bound(s, t);
      if (!b) return;
      if (extra.length) fail('bufferData source offset');
      usage = Number(usage);
      if (
        ![
          35040,
          35044,
          35048,
          ...(kind === 'webgl2' ? [35041, 35042, 35045, 35046, 35049, 35050] : []),
        ].includes(usage)
      ) {
        error(s, 1280);
        return;
      }
      let source;
      if (typeof value === 'number') {
        if (value < 0) {
          error(s, 1281);
          return;
        }
        if (value > 64 * 1024 * 1024) fail('buffer allocation limit');
        source = new Uint8Array(Math.trunc(value));
      } else {
        source = bytes(value);
        if (!source) {
          if (value === null) {
            error(s, 1281);
            return;
          }
          throw new TypeError('Expected buffer source');
        }
      }
      b.data = new Uint8Array(source);
      b.usage = usage;
    });
    method('bufferSubData', (s, t, offset, value, ...extra) => {
      const b = bound(s, t);
      if (!b) return;
      if (extra.length) fail('bufferSubData source offset');
      const source = bytes(value);
      if (!source) throw new TypeError('Expected buffer source');
      offset = Math.trunc(Number(offset));
      if (offset < 0 || offset + source.length > b.data.length) {
        error(s, 1281);
        return;
      }
      b.data.set(source, offset);
    });
    method('getBufferParameter', (s, t, p) => {
      const b = bound(s, t);
      if (!b) return null;
      if (Number(p) === 34660) return b.data.length;
      if (Number(p) === 34661) return b.usage;
      error(s, 1280);
      return null;
    });
    if (kind === 'webgl2')
      method('getBufferSubData', (s, t, offset, value, ...extra) => {
        const b = bound(s, t);
        if (!b) return;
        if (extra.length) fail('getBufferSubData destination offset');
        const dest = bytes(value);
        if (!dest) throw new TypeError('Expected buffer view');
        offset = Math.trunc(Number(offset));
        if (offset < 0 || offset + dest.length > b.data.length) {
          error(s, 1281);
          return;
        }
        dest.set(b.data.subarray(offset, offset + dest.length));
      });
    method('createShader', (s, shaderType) => {
      shaderType = Number(shaderType);
      if (shaderType !== 35632 && shaderType !== 35633) {
        error(s, 1280);
        return null;
      }
      const object = Object.create(WebGLShader.prototype);
      resources.set(object, {
        owner: s,
        type: 'WebGLShader',
        shaderType,
        source: '',
        deleted: false,
      });
      return object;
    });
    method('shaderSource', (s, value, source) => {
      const r = resource(s, value, 'WebGLShader');
      if (r) r.source = String(source);
    });
    method('getShaderSource', (s, value) => {
      const r = resource(s, value, 'WebGLShader');
      return r ? r.source : null;
    });
    method('isShader', (s, value) => {
      const r = resources.get(value);
      return !!r && r.owner === s && r.type === 'WebGLShader' && !r.deleted;
    });
    method('getShaderParameter', (s, value, p) => {
      const r = resource(s, value, 'WebGLShader');
      if (!r) return null;
      if (Number(p) === 35663) return r.shaderType;
      if (Number(p) === 35713) return false;
      if (Number(p) === 35712) return false;
      error(s, 1280);
      return null;
    });
    method('deleteShader', (s, value) => {
      if (value === null) return;
      const r = resource(s, value, 'WebGLShader');
      if (r) r.deleted = true;
    });
    method('compileShader', (s, value) => {
      if (resource(s, value, 'WebGLShader')) fail('compileShader');
    });
    /* shared_webgl_textures */
    /* shared_webgl_programs */
    /* shared_webgl_framebuffers */
    method('clearColor', (s, r, g, b, a) => {
      s.color = [r, g, b, a].map((v) => Math.fround(Math.min(1, Math.max(0, Number(v)))));
    });
    method('colorMask', (s, r, g, b, a) => {
      s.mask = [r, g, b, a].map(Boolean);
    });
    method('clearDepth', (s, value) => {
      s.depthClear = Math.min(1, Math.max(0, Number(value)));
    });
    method('stencilMask', (s, value) => {
      s.stencilMaskFront = s.stencilMaskBack = Number(value) >>> 0;
    });
    method('stencilMaskSeparate', (s, face, value) => {
      face = Number(face) >>> 0;
      value = Number(value) >>> 0;
      if (![1028, 1029, 1032].includes(face)) {
        error(s, 1280);
        return;
      }
      if (face !== 1029) s.stencilMaskFront = value;
      if (face !== 1028) s.stencilMaskBack = value;
    });
    method('hint', (s, target, mode) => {
      target = Number(target) >>> 0;
      mode = Number(mode) >>> 0;
      if (
        ![4352, 4353, 4354].includes(mode) ||
        (target !== 33170 &&
          !(
            target === 35723 &&
            (kind === 'webgl2' || s.extensions.has('OES_standard_derivatives'))
          ))
      ) {
        error(s, 1280);
        return;
      }
      if (target === 35723) s.derivativeHint = mode;
      else s.mipmapHint = mode;
    });
    method('clearStencil', (s, value) => {
      s.stencilClear = Number(value) | 0;
    });
    method('depthMask', (s, value) => {
      s.depthMask = Boolean(value);
    });
    const rectangle = (s, key, x, y, w, h) => {
      const rect = [x, y, w, h].map((v) => Number(v) | 0);
      if (rect[2] < 0 || rect[3] < 0) {
        error(s, 1281);
        return;
      }
      s[key] = rect;
    };
    method('viewport', (s, ...args) => rectangle(s, 'viewport', ...args));
    method('scissor', (s, ...args) => rectangle(s, 'scissor', ...args));
    for (const name of ['enable', 'disable'])
      method(name, (s, p) => {
        p = Number(p);
        if (!s.enabled.has(p)) {
          error(s, 1280);
          return;
        }
        s.enabled.set(p, name === 'enable');
      });
    method('isEnabled', (s, p) => {
      p = Number(p);
      if (!s.enabled.has(p)) {
        error(s, 1280);
        return false;
      }
      return s.enabled.get(p);
    });
    method('pixelStorei', (s, p, value) => {
      if (![3333, 3317].includes(Number(p))) return fail('pixelStorei');
      value = Number(value);
      if (![1, 2, 4, 8].includes(value)) {
        error(s, 1281);
        return;
      }
      if (Number(p) === 3317) s.unpackAlignment = value;
      else s.pack = value;
    });
    const storage = (s) => {
      const { width, height } = s.dim;
      if (width * height > 16 * 1024 * 1024) fail('drawing buffer allocation limit');
      if (!s.pixels || s.width !== width || s.height !== height) {
        s.width = width;
        s.height = height;
        s.pixels = new Uint8Array(width * height * 4);
        s.samplePixels = null;
        if (!s.attributes.alpha) for (let i = 3; i < s.pixels.length; i += 4) s.pixels[i] = 255;
      }
      materializePrograms(s);
      return s.pixels;
    };
    method('clear', (s, mask) => {
      if (s.drawFramebuffer) fail('attachment clear');
      mask = Number(mask) >>> 0;
      if (mask & ~(16384 | 256 | 1024)) {
        error(s, 1281);
        return;
      }
      if (mask & 256) s.depthValue = s.depthMask ? s.depthClear : s.depthValue;
      if (mask & 1024) s.stencilValue = s.stencilClear;
      if (!(mask & 16384)) return;
      const pixels = storage(s),
        rect = s.enabled.get(3089) ? s.scissor : [0, 0, s.width, s.height];
      for (let y = Math.max(0, rect[1]); y < Math.min(s.height, rect[1] + rect[3]); y++)
        for (let x = Math.max(0, rect[0]); x < Math.min(s.width, rect[0] + rect[2]); x++)
          for (let c = 0; c < 4; c++)
            if (s.mask[c] && (c !== 3 || s.attributes.alpha)) {
              const i = (y * s.width + x) * 4 + c,
                v = Math.floor(s.color[c] * 255);
              pixels[i] = v;
              if (s.samplePixels) for (const sample of s.samplePixels) sample[i] = v;
            }
    });
    method('readPixels', (s, x, y, w, h, format, pixelType, dest, ...extra) => {
      if (s.readFramebuffer) fail('attachment readPixels');
      if (extra.length) fail('readPixels destination offset');
      x = Number(x) | 0;
      y = Number(y) | 0;
      w = Number(w) | 0;
      h = Number(h) | 0;
      if (w < 0 || h < 0) {
        error(s, 1281);
        return;
      }
      if (Number(format) !== 6408 || Number(pixelType) !== 5121) fail('readPixels format');
      if (!(dest instanceof Uint8Array)) throw new TypeError('Expected Uint8Array');
      const stride = Math.ceil((w * 4) / s.pack) * s.pack;
      if (dest.length < (h ? stride * (h - 1) + w * 4 : 0)) {
        error(s, 1282);
        return;
      }
      const pixels = storage(s);
      for (let row = 0; row < h; row++)
        for (let col = 0; col < w; col++)
          for (let c = 0; c < 4; c++) {
            const inside = x + col >= 0 && x + col < s.width && y + row >= 0 && y + row < s.height;
            dest[row * stride + col * 4 + c] = inside
              ? pixels[((y + row) * s.width + x + col) * 4 + c]
              : 0;
          }
    });
    const create = (canvas, attributes, dim) => {
      attributes = attributes || {};
      const normalized = {};
      for (const [key, fallback] of Object.entries({
        alpha: true,
        antialias: true,
        depth: true,
        desynchronized: false,
        failIfMajorPerformanceCaveat: false,
        powerPreference: 'default',
        premultipliedAlpha: true,
        preserveDrawingBuffer: false,
        stencil: false,
        xrCompatible: false,
      })) {
        const value = attributes[key];
        normalized[key] =
          value === undefined ? fallback : key === 'powerPreference' ? String(value) : !!value;
      }
      normalized.xrCompatible = false;
      const powerPreference = normalized.powerPreference;
      if (!['default', 'low-power', 'high-performance'].includes(powerPreference))
        throw new TypeError(
          "Failed to execute 'getContext' on '" +
            (typeof HTMLCanvasElement !== 'undefined' && canvas instanceof HTMLCanvasElement
              ? 'HTMLCanvasElement'
              : 'OffscreenCanvas') +
            "': Failed to read the 'powerPreference' property from 'CanvasContextCreationAttributesModule': The provided value '" +
            powerPreference +
            "' is not a valid enum value of type CanvasPowerPreference.",
        );
      const context = Object.create(proto),
        s = {
          canvas,
          dim,
          error: 0,
          extensions: new Map(),
          stencilMaskFront: 4294967295,
          stencilMaskBack: 4294967295,
          derivativeHint: 4352,
          mipmapHint: 4352,
          bindings: new Map(),
          color: [0, 0, 0, 0],
          mask: [true, true, true, true],
          viewport: [0, 0, dim.width, dim.height],
          scissor: [0, 0, dim.width, dim.height],
          pack: 4,
          enabled: new Map([
            [3089, false],
            [3024, true],
            [3042, false],
            [2929, false],
            [2960, false],
            [2884, false],
            [32823, false],
            [32926, false],
            [32928, false],
          ]),
          attributes: normalized,
        };
      slots.set(context, s);
      return context;
    };
    const factory = (canvas, attributes, dim) => {
      const context = create(canvas, attributes, dim),
        s = slots.get(context);
      s.depthClear = 1;
      s.stencilClear = 0;
      s.depthMask = true;
      s.depthValue = 1;
      s.stencilValue = 0;
      dim.readPixels = () => {
        const src = storage(s),
          out = new Uint8ClampedArray(src.length);
        for (let y = 0; y < s.height; y++)
          for (let x = 0; x < s.width; x++) {
            const from = (y * s.width + x) * 4,
              to = ((s.height - 1 - y) * s.width + x) * 4,
              a = src[from + 3];
            for (let c = 0; c < 3; c++)
              out[to + c] = s.attributes.premultipliedAlpha
                ? src[from + c]
                : Math.round((src[from + c] * a) / 255);
            out[to + 3] = a;
          }
        return out;
      };
      dim.onTransfer = () => {
        s.pixels = null;
        s.drawCommands = [];
      };
      dim.onResize = () => {
        if (dim.width !== s.width || dim.height !== s.height) {
          s.pixels = null;
          s.drawCommands = [];
        }
      };
      return context;
    };
    canvasCompatibilityState.registerContext(kind, factory);
    if (kind === 'webgl')
      canvasCompatibilityState.registerContext('experimental-webgl', factory, 'webgl');
  }
})();
