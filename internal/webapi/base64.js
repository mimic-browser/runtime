// HTML forgiving-base64 decoding, shared by Window and Worker realms.
const base64Alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
// Validation below admits only ASCII alphabet characters. Indexed lookup avoids
// searching the alphabet for every input character; a quartet emits three bytes.
const base64DecodeTable = new Uint8Array(128);
for (let i = 0; i < base64Alphabet.length; i++) base64DecodeTable[base64Alphabet.charCodeAt(i)] = i;
// Capture before author code runs. The intrinsic performs forgiving validation
// and decoding together; its result is fresh storage with no author callbacks.
const nativeBase64Decode = host.nativeBase64Decode;
const base64SyntaxErrorPrototype = SyntaxError.prototype;
const base64GetPrototypeOf = Object.getPrototypeOf;
function invalidBase64() {
  return platformDOMException(
    'The string to be decoded is not correctly encoded.',
    'InvalidCharacterError',
  );
}
const { atob, btoa } = {
  btoa(input) {
    if (!arguments.length) throw new TypeError('1 argument required');
    if (typeof input === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    const s = String(input);
    let out = '';
    for (let i = 0; i < s.length; i += 3) {
      const a = s.charCodeAt(i),
        b = i + 1 < s.length ? s.charCodeAt(i + 1) : 0,
        c = i + 2 < s.length ? s.charCodeAt(i + 2) : 0;
      if (a > 255 || b > 255 || c > 255)
        throw platformDOMException(
          'The string to be encoded contains characters outside of the Latin1 range.',
          'InvalidCharacterError',
        );
      const n = (a << 16) | (b << 8) | c;
      out +=
        base64Alphabet[(n >> 18) & 63] +
        base64Alphabet[(n >> 12) & 63] +
        (i + 1 < s.length ? base64Alphabet[(n >> 6) & 63] : '=') +
        (i + 2 < s.length ? base64Alphabet[n & 63] : '=');
    }
    return out;
  },
  atob(input) {
    if (!arguments.length) throw new TypeError('1 argument required');
    if (typeof input === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    let s = String(input);
    if (s.length >= 1024 && typeof nativeBase64Decode === 'function') {
      let bytes;
      try {
        bytes = nativeBase64Decode(s);
      } catch (error) {
        if (base64GetPrototypeOf(error) !== base64SyntaxErrorPrototype) throw error;
        throw invalidBase64();
      }
      return host.binaryStringFromBytes(bytes);
    }
    s = s.replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s)) throw invalidBase64();
    // Small inputs stay in JS: a host crossing costs more than a few quartets.
    // Large inputs return a native one-byte string without per-byte JS output.
    if (s.length >= 1024) return host.decodeBase64String(s);
    let out = '',
      i = 0;
    for (; i + 3 < s.length; i += 4) {
      const bits =
        (base64DecodeTable[s.charCodeAt(i)] << 18) |
        (base64DecodeTable[s.charCodeAt(i + 1)] << 12) |
        (base64DecodeTable[s.charCodeAt(i + 2)] << 6) |
        base64DecodeTable[s.charCodeAt(i + 3)];
      out += String.fromCharCode((bits >>> 16) & 255, (bits >>> 8) & 255, bits & 255);
    }
    if (i + 1 < s.length) {
      const bits =
        (base64DecodeTable[s.charCodeAt(i)] << 12) |
        (base64DecodeTable[s.charCodeAt(i + 1)] << 6) |
        (i + 2 < s.length ? base64DecodeTable[s.charCodeAt(i + 2)] : 0);
      out += String.fromCharCode((bits >>> 10) & 255);
      if (i + 2 < s.length) out += String.fromCharCode((bits >>> 2) & 255);
    }
    return out;
  },
};
markNative(atob, 'atob');
markNative(btoa, 'btoa');
