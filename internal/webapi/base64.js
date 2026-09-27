// HTML forgiving-base64 decoding, shared by Window and Worker realms.
const base64Alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
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
    let s = String(input).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 0) s = s.replace(/={1,2}$/, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+/]/.test(s))
      throw platformDOMException(
        'The string to be decoded is not correctly encoded.',
        'InvalidCharacterError',
      );
    let out = '',
      bits = 0,
      count = 0;
    for (const ch of s) {
      bits = (bits << 6) | base64Alphabet.indexOf(ch);
      count += 6;
      if (count >= 8) {
        count -= 8;
        out += String.fromCharCode((bits >> count) & 255);
      }
    }
    return out;
  },
};
markNative(atob, 'atob');
markNative(btoa, 'btoa');
