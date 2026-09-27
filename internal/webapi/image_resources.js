(() => {
  const check = (value) => {
      const slot = elementSlot(value);
      if (!slot || slot.namespaceURI !== 'http://www.w3.org/1999/xhtml' || slot.tagName !== 'IMG')
        throw new TypeError('Illegal invocation');
      return slot.nodeId;
    },
    info = (value) => host.imageInfo(check(value)),
    define = (name, get, set) =>
      Object.defineProperty(HTMLImageElement.prototype, name, {
        get,
        set,
        enumerable: true,
        configurable: true,
      });
  for (const [key, field] of [
    ['complete', 'complete'],
    ['naturalWidth', 'width'],
    ['naturalHeight', 'height'],
    ['currentSrc', 'currentSrc'],
  ])
    define(key, function () {
      return info(this)[field];
    });
  define(
    'src',
    function () {
      check(this);
      const raw = this.getAttribute('src');
      return raw === null ? '' : host.urlParts(raw).href;
    },
    function (value) {
      check(this);
      this.setAttribute('src', String(value));
    },
  );
  for (const name of ['width', 'height'])
    define(
      name,
      function () {
        const state = info(this),
          raw = this.getAttribute(name);
        if (raw !== null) {
          const match = /^\s*\+?(\d+)/.exec(raw);
          return match ? Math.min(2147483647, Number(match[1])) : state[name];
        }
        const css = computedCSSDeclarations(this).find((e) => e.name === name)?.value;
        if (css && /^\d+(?:\.\d+)?px$/.test(css)) return Math.round(parseFloat(css));
        return state[name];
      },
      function (value) {
        check(this);
        this.setAttribute(name, String(+value >>> 0));
      },
    );
  const decode = function decode() {
    const image = this;
    try {
      check(image);
    } catch (error) {
      return platformPromiseReject(error);
    }
    return new platformPromise((resolve, reject) => {
      const source = image.getAttribute('src'),
        finish = () => {
          image.removeEventListener('load', finish);
          image.removeEventListener('error', finish);
          const current = info(image);
          if (image.getAttribute('src') === source && current.decoded) resolve();
          else reject(platformDOMException('The source image cannot be decoded', 'EncodingError'));
        };
      if (info(image).complete) queueMicrotask(finish);
      else {
        image.addEventListener('load', finish);
        image.addEventListener('error', finish);
      }
    });
  };
  markNative(decode, 'decode');
  Object.defineProperty(HTMLImageElement.prototype, 'decode', {
    value: decode,
    writable: true,
    enumerable: true,
    configurable: true,
  });
  define(
    'loading',
    function () {
      check(this);
      return String(this.getAttribute('loading') || '').toLowerCase() === 'lazy' ? 'lazy' : 'eager';
    },
    function (value) {
      check(this);
      this.setAttribute('loading', String(value));
    },
  );
  for (const key of [
    'complete',
    'naturalWidth',
    'naturalHeight',
    'currentSrc',
    'src',
    'width',
    'height',
    'loading',
  ]) {
    const d = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, key);
    if (d.get) markNative(d.get, key, 'get ');
    if (d.set) markNative(d.set, key, 'set ');
  }
})();
