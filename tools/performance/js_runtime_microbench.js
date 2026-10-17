// Synthetic browser API and pure-JS counterfactuals. None replaces page APIs.
// Each base64 case consumes 512 KiB * scale of deterministic binary output;
// indexed output reads include materialization of the returned binary string.
const runRuntimeMicrobench = (() => {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  const table = new Int16Array(128);
  table.fill(-1);
  for (let i = 0; i < alphabet.length; i++) table[alphabet.charCodeAt(i)] = i;
  function legacy(input) {
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw Error('invalid');
    let out = '',
      bits = 0,
      count = 0;
    for (const ch of s) {
      bits = (bits << 6) | alphabet.indexOf(ch);
      count += 6;
      if (count >= 8) {
        count -= 8;
        out += String.fromCharCode((bits >> count) & 255);
      }
    }
    return out;
  }
  function scalar(input) {
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw Error('invalid');
    let out = '',
      bits = 0,
      count = 0;
    for (let i = 0; i < s.length; i++) {
      bits = (bits << 6) | table[s.charCodeAt(i)];
      count += 6;
      if (count >= 8) {
        count -= 8;
        out += String.fromCharCode((bits >> count) & 255);
      }
    }
    return out;
  }
  function grouped(input) {
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw Error('invalid');
    let out = '',
      i = 0;
    for (; i + 3 < s.length; i += 4) {
      const n =
        (table[s.charCodeAt(i)] << 18) |
        (table[s.charCodeAt(i + 1)] << 12) |
        (table[s.charCodeAt(i + 2)] << 6) |
        table[s.charCodeAt(i + 3)];
      out += String.fromCharCode((n >>> 16) & 255, (n >>> 8) & 255, n & 255);
    }
    if (i + 1 < s.length) {
      const n =
        (table[s.charCodeAt(i)] << 12) |
        (table[s.charCodeAt(i + 1)] << 6) |
        (i + 2 < s.length ? table[s.charCodeAt(i + 2)] : 0);
      out += String.fromCharCode((n >>> 10) & 255);
      if (i + 2 < s.length) out += String.fromCharCode((n >>> 2) & 255);
    }
    return out;
  }
  function buffered(input) {
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw Error('invalid');
    const bytes = new Uint8Array((s.length * 6) >>> 3);
    let bits = 0,
      count = 0,
      at = 0;
    for (let i = 0; i < s.length; i++) {
      bits = (bits << 6) | table[s.charCodeAt(i)];
      count += 6;
      if (count >= 8) {
        count -= 8;
        bytes[at++] = (bits >> count) & 255;
      }
    }
    let out = '';
    for (let i = 0; i < bytes.length; i += 4096)
      out += String.fromCharCode.apply(null, bytes.subarray(i, i + 4096));
    return out;
  }
  const fromBase64 = Uint8Array.fromBase64;
  function nativeBuffered(input) {
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw Error('invalid');
    const bytes = fromBase64(s);
    let out = '';
    for (let i = 0; i < bytes.length; i += 4096)
      out += String.fromCharCode.apply(null, bytes.subarray(i, i + 4096));
    return out;
  }
  const operations = [
    (x) => (x + 17) | 0,
    (x) => x ^ 0x1234567,
    (x) => Math.imul(x, 33),
    (x) => x >>> 1,
  ];
  function arithmetic(iterations) {
    let x = 123;
    for (let i = 0; i < iterations; i++) {
      x = (x + 17) | 0;
      x ^= 0x1234567;
      x = Math.imul(x, 33);
      x >>>= 1;
    }
    return x;
  }
  function dispatch(iterations) {
    let x = 123;
    for (let i = 0; i < iterations; i++)
      for (let op = 0; op < operations.length; op++) x = operations[op](x);
    return x;
  }
  return function run(options = {}) {
    const rows = [],
      names = options.names || null;
    const measure = (name, iterations, run) => {
      if (names && !names.some((prefix) => name.startsWith(prefix))) return;
      const start = performance.now();
      let checksum = 0;
      for (let i = 0; i < iterations; i++) checksum = (checksum + run(i)) | 0;
      rows.push({ name, iterations, ms: performance.now() - start, checksum });
    };
    const decoders = { intrinsic: atob, legacy, scalar, grouped, buffered };
    if (typeof fromBase64 === 'function') decoders.nativeBuffered = nativeBuffered;
    for (const size of [32, 1024, 65536]) {
      let raw = '';
      for (let i = 0; i < size; i++) raw += String.fromCharCode((i * 73 + 19) & 255);
      const encoded = btoa(raw),
        iterations = Math.max(8, Math.floor(524288 / size)) * (options.scale || 1);
      for (const [name, decoder] of Object.entries(decoders)) {
        if (decoder(encoded) !== raw) throw Error('decode mismatch ' + name);
        measure('atob/' + size + '/' + name, iterations, () => {
          const value = decoder(encoded);
          return (
            value.length + value.charCodeAt(value.length >>> 1) + value.charCodeAt(value.length - 1)
          );
        });
      }
      measure('btoa/' + size, iterations, () => btoa(raw).length);
    }
    measure('arithmetic', 1, () => arithmetic(1000000));
    measure('dispatch', 1, () => dispatch(1000000));
    const element = document.createElement('div');
    element.style.cssText = 'width:123px;height:45px;padding:2px;border:1px solid';
    element.setAttribute('data-value', 'value');
    document.body.append(element);
    try {
      measure('dom/clientWidth', 1000, () => element.clientWidth);
      measure('dom/rect', 1000, () => element.getBoundingClientRect().width);
      measure('dom/attribute', 10000, () => element.getAttribute('data-value').length);
      measure('dom/mutation-width', 100, (i) => {
        element.style.width = 123 + (i & 1) + 'px';
        return element.clientWidth;
      });
    } finally {
      element.remove();
    }
    return rows;
  };
})();
