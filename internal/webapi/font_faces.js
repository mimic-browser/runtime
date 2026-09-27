// Font collection ownership and descriptor state; no native font backend.
// Loaded resources feed the same CPU metrics as SVG text. No native graphics backend.
(() => {
  if (typeof globalThis.FontFace !== 'function' || typeof globalThis.FontFaceSet !== 'function')
    return;
  const faces = new WeakMap(),
    sets = new WeakMap(),
    owners = new WeakMap(),
    cssFaces = new WeakMap(),
    encodeBase64 = globalThis.btoa;
  const syntax = () => platformDOMException('Invalid font descriptor', 'SyntaxError');
  const requireFace = (value) => {
    const s = faces.get(value);
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const requireSet = (value) => {
    const s = sets.get(value);
    if (!s) throw new TypeError('Illegal invocation');
    syncCSSFaces(s);
    return s;
  };
  const defaults = {
    style: 'normal',
    weight: 'normal',
    stretch: 'normal',
    unicodeRange: 'U+0-10FFFF',
    variant: 'normal',
    featureSettings: 'normal',
    variationSettings: 'normal',
    display: 'auto',
    ascentOverride: 'normal',
    descentOverride: 'normal',
    lineGapOverride: 'normal',
    sizeAdjust: '100%',
  };
  const percent = (v) => /^\d+(?:\.\d+)?%$/.test(v);
  function descriptor(key, value) {
    value = String(value).trim().replace(/\s+/g, ' ');
    let valid = false;
    if (key === 'style')
      valid = /^(normal|italic|oblique(?: -?\d+(?:\.\d+)?deg){0,2})$/.test(value);
    else if (key === 'weight')
      valid =
        /^(normal|bold)$/.test(value) ||
        (value.split(' ').length <= 2 &&
          value
            .split(' ')
            .every((v) => /^\d+(?:\.\d+)?$/.test(v) && Number(v) >= 1 && Number(v) <= 1000));
    else if (key === 'stretch')
      valid =
        /^(normal|(?:ultra-|extra-|semi-)?(?:condensed|expanded))$/.test(value) ||
        (value.split(' ').length <= 2 && value.split(' ').every(percent));
    else if (key === 'display') valid = /^(auto|block|swap|fallback|optional)$/.test(value);
    else if (key === 'sizeAdjust') valid = percent(value);
    else if (['ascentOverride', 'descentOverride', 'lineGapOverride'].includes(key))
      valid = value === 'normal' || percent(value);
    else if (key === 'variant') valid = /^(normal|small-caps)$/.test(value);
    else if (key === 'featureSettings' || key === 'variationSettings')
      valid =
        value === 'normal' ||
        value
          .split(',')
          .every((v) =>
            key === 'featureSettings'
              ? /^\s*["'][\x20-\x7e]{4}["'](?: (?:\d+|on|off))?\s*$/.test(v)
              : /^\s*["'][\x20-\x7e]{4}["'] -?\d+(?:\.\d+)?\s*$/.test(v),
          );
    else if (key === 'unicodeRange') {
      return value
        .split(',')
        .map((v) => {
          const m = /^U\+([0-9A-F?]{1,6})(?:-([0-9A-F]{1,6}))?$/i.exec(v.trim());
          if (!m || (m[2] && m[1].includes('?')) || /\?[0-9a-f]/i.test(m[1])) throw syntax();
          const start = parseInt(m[1].replace(/\?/g, '0'), 16),
            end = parseInt(m[2] || m[1].replace(/\?/g, 'F'), 16);
          if (start > end || end > 0x10ffff) throw syntax();
          return (
            'U+' +
            start.toString(16).toUpperCase() +
            (end === start ? '' : '-' + end.toString(16).toUpperCase())
          );
        })
        .join(', ');
    }
    if (!valid) throw syntax();
    return value;
  }
  const familyName = (value) =>
    /^[-_a-zA-Z][-_a-zA-Z0-9]*$/.test(value) &&
    !/^(serif|sans-serif|monospace|cursive|fantasy|system-ui|inherit|initial|unset|revert|default)$/i.test(
      value,
    )
      ? value
      : JSON.stringify(value);
  const OriginalFontFace = globalThis.FontFace;
  function createFontFace(family, source, descriptors = {}, base) {
    const object = Object.create(FontFace.prototype);
    let reject, resolve;
    const s = {
      family: familyName(String(family)),
      ...defaults,
      status: 'unloaded',
      sets: new Set(),
      loaded: new platformPromise((a, b) => {
        resolve = a;
        reject = b;
      }),
    };
    faces.set(object, s);
    s.reject = reject;
    s.resolve = resolve;
    s.object = object;
    // Internal rejection handling does not change the public promise identity.
    s.loaded.catch(() => {});
    try {
      if (
        descriptors !== null &&
        typeof descriptors !== 'object' &&
        typeof descriptors !== 'function'
      )
        throw new TypeError('Expected descriptor dictionary');
      for (const key of Object.keys(defaults))
        if (descriptors?.[key] !== undefined) s[key] = descriptor(key, descriptors[key]);
      if (ArrayBuffer.isView(source) || source instanceof ArrayBuffer) {
        const bytes = ArrayBuffer.isView(source)
          ? new Uint8Array(source.buffer, source.byteOffset, source.byteLength)
          : new Uint8Array(source);
        // Every supported sfnt/WOFF container needs more than this prefix. A valid
        // prefix is not sufficient evidence of a decoded, usable font.
        if (bytes.length < 12) throw syntax();
        const resource = loadBinary(bytes);
        if (!resource.id) throw syntax();
        s.id = resource.id;
        s.status = 'loaded';
        resolve(object);
        return object;
      }
      s.source = String(source).trim();
      s.sources = parseSources(s.source, base);
      if (!s.sources.length) throw syntax();
    } catch (e) {
      if (e instanceof TypeError) throw e;
      s.status = 'error';
      reject(e);
    }
    return object;
  }
  function FontFace(family, source, descriptors = {}) {
    if (!new.target || arguments.length < 2) throw new TypeError('Expected family and source');
    const object = createFontFace(family, source, descriptors);
    Object.setPrototypeOf(object, new.target.prototype);
    return object;
  }
  FontFace.prototype = OriginalFontFace.prototype;
  Object.defineProperty(FontFace.prototype, 'constructor', {
    value: FontFace,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(globalThis, 'FontFace', {
    value: FontFace,
    writable: true,
    configurable: true,
  });
  for (const key of ['family', ...Object.keys(defaults), 'status', 'loaded']) {
    const d = {
      get() {
        return requireFace(this)[key];
      },
      configurable: true,
      enumerable: true,
    };
    if (key === 'family' || key in defaults)
      d.set = function (value) {
        const s = requireFace(this);
        s[key] = key === 'family' ? String(value) : descriptor(key, value);
        syncCollection();
      };
    Object.defineProperty(FontFace.prototype, key, d);
  }
  function loadBinary(bytes) {
    let raw = '';
    for (let i = 0; i < bytes.length; i += 8192)
      raw += String.fromCharCode(...bytes.subarray(i, i + 8192));
    return host.fontBinary(encodeBase64(raw));
  }
  function parseSources(
    source,
    base = typeof document === 'object' ? document.baseURI : location.href,
  ) {
    const items = [],
      re =
        /(local|url)\(\s*(?:"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)'|([^()]*?))\s*\)(?:\s*format\([^()]*\))?/gi;
    let match,
      at = 0;
    while ((match = re.exec(source))) {
      if (source.slice(at, match.index).trim().replace(/^,/, '').trim()) throw syntax();
      at = re.lastIndex;
      const kind = match[1].toLowerCase(),
        value = (match[2] ?? match[3] ?? match[4]).trim();
      items.push({
        kind,
        value: kind === 'url' ? new URL(value, base).href : value,
      });
    }
    if (source.slice(at).trim()) throw syntax();
    return items;
  }
  function syncCollection() {
    if (!host.fontCollection || typeof document !== 'object') return;
    const set = owners.get(document),
      choices = [];
    if (set)
      for (const face of requireSet(set).values) {
        const s = requireFace(face);
        if (s.status === 'loaded')
          choices.push({
            id: s.id,
            family: s.family,
            style: s.style,
            unicodeRange: s.unicodeRange,
            unsupported: [
              'stretch',
              'variant',
              'featureSettings',
              'variationSettings',
              'ascentOverride',
              'descentOverride',
              'lineGapOverride',
              'sizeAdjust',
            ]
              .filter((key) => s[key] !== defaults[key])
              .join(', '),
            weight: s.weight === 'bold' ? 700 : s.weight === 'normal' ? 400 : parseFloat(s.weight),
          });
      }
    host.fontCollection(JSON.stringify(choices));
  }
  const loadEvents = new WeakMap(),
    eventPrototype = globalThis.FontFaceSetLoadEvent?.prototype || Object.create(Event.prototype);
  function FontFaceSetLoadEvent(type, init = {}) {
    if (!new.target || arguments.length < 1) throw new TypeError('Expected event type');
    const event = new Event(type, init),
      values = Array.from(init.fontfaces || []);
    for (const face of values) requireFace(face);
    Object.setPrototypeOf(event, new.target.prototype);
    loadEvents.set(event, Object.freeze(values));
    return event;
  }
  FontFaceSetLoadEvent.prototype = eventPrototype;
  Object.defineProperty(eventPrototype, 'constructor', {
    value: FontFaceSetLoadEvent,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(eventPrototype, 'fontfaces', {
    get() {
      if (!loadEvents.has(this)) throw new TypeError('Illegal invocation');
      return loadEvents.get(this);
    },
    enumerable: true,
    configurable: true,
  });
  if (typeof document === 'object')
    Object.defineProperty(globalThis, 'FontFaceSetLoadEvent', {
      value: FontFaceSetLoadEvent,
      writable: true,
      configurable: true,
    });
  function loadingEvent(s, name, faces = []) {
    s.object.dispatchEvent(new FontFaceSetLoadEvent(name, { fontfaces: faces }));
  }
  function track(set, s) {
    set.pending.add(s);
    if (set.status !== 'loading') {
      set.status = 'loading';
      set.ready = new platformPromise((resolve) => (set.resolveReady = resolve));
      setTimeout(() => loadingEvent(set, 'loading'), 0);
    }
  }
  function settle(set) {
    if (set.status !== 'loading') return;
    setTimeout(() => {
      if (set.status !== 'loading' || Array.from(set.pending).some((s) => s.status === 'loading'))
        return;
      const loaded = set.loadedFaces.splice(0),
        failed = set.failedFaces.splice(0);
      set.pending.clear();
      set.status = 'loaded';
      loadingEvent(set, 'loadingdone', loaded);
      if (failed.length) loadingEvent(set, 'loadingerror', failed);
      set.resolveReady?.(set.object);
    }, 0);
  }
  function begin(s) {
    s.status = 'loading';
    for (const set of s.sets) track(set, s);
  }
  function complete(s, error) {
    s.status = error ? 'error' : 'loaded';
    if (error) s.reject(error);
    else s.resolve(s.object);
    syncCollection();
    for (const set of s.sets) {
      (error ? set.failedFaces : set.loadedFaces).push(s.object);
      settle(set);
      if (!set.values.has(s.object)) s.sets.delete(set);
    }
  }
  function loadFace(s) {
    if (s.status !== 'unloaded') return s.loaded;
    begin(s);
    let index = 0;
    const next = () => {
      while (index < s.sources.length) {
        const source = s.sources[index++];
        if (source.kind === 'local') {
          const result = host.fontLocal(source.value);
          if (result.id) {
            s.id = result.id;
            complete(s);
            return;
          }
          continue;
        }
        host
          .fetch(source.value, 'GET', {}, [], '', {
            mode: 'cors',
            credentials: 'same-origin',
            resourceKind: 'font',
          })
          .then((response) => {
            if (response.status < 200 || response.status >= 300)
              throw new Error('Font response failed');
            const result = loadBinary(new Uint8Array(response.bodyBytes));
            if (!result.id) throw new Error('Invalid font data');
            s.id = result.id;
            complete(s);
          })
          .catch(next);
        return;
      }
      complete(s, platformDOMException('A font resource could not be loaded', 'NetworkError'));
    };
    next();
    return s.loaded;
  }
  Object.defineProperty(FontFace.prototype, 'load', {
    value: function load() {
      try {
        return loadFace(requireFace(this));
      } catch (e) {
        return platformPromiseReject(e);
      }
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  function makeSet(owner) {
    const value = new EventTarget();
    Object.setPrototypeOf(value, FontFaceSet.prototype);
    const state = {
      object: value,
      owner,
      values: new Set(),
      cssValues: [],
      pending: new Set(),
      loadedFaces: [],
      failedFaces: [],
      handlers: Object.create(null),
      status: 'loaded',
      ready:
        typeof document === 'object' && owner === document
          ? platformPromiseResolve(value)
          : new platformPromise(() => {}),
    };
    sets.set(value, state);
    return value;
  }
  const descriptorNames = {
    'font-style': 'style',
    'font-weight': 'weight',
    'font-stretch': 'stretch',
    'unicode-range': 'unicodeRange',
    'font-variant': 'variant',
    'font-feature-settings': 'featureSettings',
    'font-variation-settings': 'variationSettings',
    'font-display': 'display',
    'ascent-override': 'ascentOverride',
    'descent-override': 'descentOverride',
    'line-gap-override': 'lineGapOverride',
    'size-adjust': 'sizeAdjust',
  };
  const cssFamily = (value) => {
    value = String(value || '').trim();
    return /^(['"]).*\1$/.test(value) ? value.slice(1, -1) : value;
  };
  function syncCSSFaces(state) {
    if (
      typeof document !== 'object' ||
      state.owner !== document ||
      typeof constructedStyleSheets.fontFaceRules !== 'function'
    )
      return;
    const previous = state.cssValues;
    state.cssValues = constructedStyleSheets
      .fontFaceRules(document)
      .map((record) => {
        const family = cssFamily(record.declarations['font-family']),
          source = record.declarations.src;
        if (!family || !source) return null;
        const descriptors = {};
        for (const [cssName, key] of Object.entries(descriptorNames))
          if (record.declarations[cssName] !== undefined)
            descriptors[key] = record.declarations[cssName];
        const signature = JSON.stringify([family, source, descriptors, record.base]);
        let cached = cssFaces.get(record.rule);
        if (!cached || cached.signature !== signature) {
          try {
            cached = {
              signature,
              face: createFontFace(family, source, descriptors, record.base),
            };
          } catch {
            return null;
          }
          cssFaces.set(record.rule, cached);
        }
        return cached.face;
      })
      .filter(Boolean);
    for (const face of previous) {
      if (!state.cssValues.includes(face) && !state.pending.has(requireFace(face)))
        requireFace(face).sets.delete(state);
    }
    for (const face of state.cssValues) requireFace(face).sets.add(state);
  }
  const allValues = (state) => {
    syncCSSFaces(state);
    return state.cssValues.concat(
      Array.from(state.values).filter((face) => !state.cssValues.includes(face)),
    );
  };
  for (const [key, get] of Object.entries({
    size: (s) => allValues(s).length,
    status: (s) => s.status,
    ready: (s) => s.ready,
  }))
    Object.defineProperty(FontFaceSet.prototype, key, {
      get() {
        return get(requireSet(this));
      },
      configurable: true,
      enumerable: true,
    });
  const methods = {
    add(face) {
      const s = requireSet(this);
      const f = requireFace(face);
      s.values.add(face);
      f.sets.add(s);
      if (f.status === 'loading') track(s, f);
      syncCollection();
      return this;
    },
    delete(face) {
      const s = requireSet(this);
      const f = requireFace(face);
      if (f.status !== 'loading') f.sets.delete(s);
      s.pending.delete(f);
      settle(s);
      const removed = s.values.delete(face);
      syncCollection();
      return removed;
    },
    has(face) {
      const s = requireSet(this);
      requireFace(face);
      return s.values.has(face) || s.cssValues.includes(face);
    },
    clear() {
      const s = requireSet(this);
      for (const face of s.values) {
        const f = requireFace(face);
        if (!s.pending.has(f)) f.sets.delete(s);
      }
      s.values.clear();
      syncCollection();
    },
    keys() {
      return allValues(requireSet(this)).values();
    },
    values() {
      return allValues(requireSet(this)).values();
    },
    entries() {
      return allValues(requireSet(this))
        .map((value) => [value, value])
        .values();
    },
    forEach(callback, thisArg) {
      const s = requireSet(this);
      if (typeof callback !== 'function') throw new TypeError('Expected callback');
      allValues(s).forEach((v) => callback.call(thisArg, v, v, this));
    },
    check(font, text = ' ') {
      const s = requireSet(this);
      if (arguments.length < 1) throw new TypeError('Expected font');
      const parsed = parseFont(font);
      return matching(s, parsed, String(text)).every(
        (face) => requireFace(face).status === 'loaded',
      );
    },
    load(font, text = ' ') {
      try {
        const s = requireSet(this);
        if (arguments.length < 1) throw new TypeError('Expected font');
        const parsed = parseFont(font);
        return platformPromiseAll(matching(s, parsed, String(text)).map((face) => face.load()));
      } catch (e) {
        return platformPromiseReject(e);
      }
    },
  };
  // A bounded shorthand grammar. More complex CSS must not be silently accepted.
  function parseFont(value) {
    value = String(value).trim();
    if (
      !/^(?:(?:normal|italic|oblique|small-caps|bold|bolder|lighter|[1-9]\d{0,2})\s+)*(?:\d+(?:\.\d+)?(?:px|pt|em|rem|%)|(?:xx?-small|small|medium|large|xx?-large))(?:\s*\/\s*(?:normal|\d+(?:\.\d+)?(?:px|pt|em|rem|%)?))?\s+(?:["'][^"']+["']|[-_a-zA-Z][-_a-zA-Z0-9 ]*)(?:\s*,\s*(?:["'][^"']+["']|[-_a-zA-Z][-_a-zA-Z0-9 ]*))*$/.test(
        value,
      )
    )
      throw syntax();
    const match = /\s((?:[\"'][^\"']+[\"']|[-_a-zA-Z][-_a-zA-Z0-9 ]*)(?:\s*,.*)?)$/.exec(value);
    return {
      families: (match?.[1] || '').split(',').map((v) =>
        v
          .trim()
          .replace(/^['\"]|['\"]$/g, '')
          .toLowerCase(),
      ),
    };
  }
  function matching(set, parsed, text) {
    return allValues(set).filter((face) => {
      const s = requireFace(face),
        family = s.family.replace(/^['\"]|['\"]$/g, '').toLowerCase();
      if (!parsed.families.includes(family)) return false;
      const ranges = s.unicodeRange.split(',').map((raw) =>
        raw
          .trim()
          .slice(2)
          .split('-')
          .map((v) => parseInt(v, 16)),
      );
      return Array.from(text).some((ch) =>
        ranges.some(
          ([start, end = start]) => ch.codePointAt(0) >= start && ch.codePointAt(0) <= end,
        ),
      );
    });
  }
  for (const [name, value] of Object.entries(methods))
    Object.defineProperty(FontFaceSet.prototype, name, {
      value,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  for (const type of ['loading', 'loadingdone', 'loadingerror'])
    Object.defineProperty(FontFaceSet.prototype, 'on' + type, {
      get() {
        return requireSet(this).handlers[type] || null;
      },
      set(value) {
        requireSet(this).handlers[type] = typeof value === 'function' ? value : null;
      },
      configurable: true,
      enumerable: true,
    });
  Object.defineProperty(FontFaceSet.prototype, Symbol.iterator, {
    value: methods.values,
    writable: true,
    configurable: true,
  });
  function owned(owner) {
    let value = owners.get(owner);
    if (!value) {
      value = makeSet(owner);
      owners.set(owner, value);
    }
    syncCSSFaces(value);
    return value;
  }
  let automaticLoadQueued = false;
  function loadRenderedCSSFonts() {
    automaticLoadQueued = false;
    if (typeof document !== 'object' || !document.documentElement) return;
    const set = requireSet(owned(document));
    if (!set.cssValues.length) return;
    const candidates = new Map();
    for (const element of document.querySelectorAll('*')) {
      if (/^(SCRIPT|STYLE|TEMPLATE|NOSCRIPT)$/.test(element.tagName)) continue;
      const text = element.textContent;
      if (!text || !text.trim()) continue;
      const style = getComputedStyle(element);
      if (style.display === 'none' || style.visibility === 'hidden') continue;
      const families = String(style.fontFamily || '')
        .split(',')
        .map((value) =>
          value
            .trim()
            .replace(/^['"]|['"]$/g, '')
            .toLowerCase(),
        );
      for (const face of set.cssValues) {
        const state = requireFace(face);
        const family = state.family.replace(/^['"]|['"]$/g, '').toLowerCase();
        if (state.status === 'unloaded' && families.includes(family)) candidates.set(face, text);
      }
    }
    for (const [face, text] of candidates) {
      const state = requireFace(face);
      if (
        matching(
          set,
          { families: [state.family.replace(/^['"]|['"]$/g, '').toLowerCase()] },
          text,
        ).includes(face)
      )
        loadFace(state);
    }
  }
  function queueAutomaticCSSFontLoad() {
    if (automaticLoadQueued) return;
    automaticLoadQueued = true;
    setTimeout(loadRenderedCSSFonts, 0);
  }
  if (typeof globalThis.Document === 'function')
    Object.defineProperty(Document.prototype, 'fonts', {
      get() {
        if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
        return owned(this);
      },
      configurable: true,
      enumerable: true,
    });
  if (typeof document === 'object') {
    document.addEventListener('DOMContentLoaded', queueAutomaticCSSFontLoad);
  } else
    Object.defineProperty(globalThis, 'fonts', {
      get() {
        return owned(globalThis);
      },
      configurable: true,
      enumerable: true,
    });
  if (typeof markNative === 'function') {
    markNative(FontFace, 'FontFace');
    markNative(FontFaceSetLoadEvent, 'FontFaceSetLoadEvent');
    for (const prototype of [FontFace.prototype, FontFaceSet.prototype, eventPrototype])
      for (const key of Object.getOwnPropertyNames(prototype)) {
        if (key === 'constructor') continue;
        const d = Object.getOwnPropertyDescriptor(prototype, key);
        if (typeof d.value === 'function') markNative(d.value, key);
        if (d.get) markNative(d.get, key, 'get ');
        if (d.set) markNative(d.set, key, 'set ');
      }
  }
})();
