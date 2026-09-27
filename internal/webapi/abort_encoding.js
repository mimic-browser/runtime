// Shared cancellation and text decoder semantics for Window and Worker.
const abortSlots = new WeakMap(),
  controllerSlots = new WeakMap();
const abort = (signal, reason) => {
  const s = abortSlots.get(signal);
  if (s.aborted) return;
  s.aborted = true;
  s.reason =
    reason === undefined
      ? platformDOMException('signal is aborted without reason', 'AbortError')
      : reason;
  dispatchTrusted(signal, new Event('abort'));
};
class AbortSignal extends EventTarget {
  constructor(token) {
    super();
    if (token !== hostToken) throw new TypeError('Illegal constructor');
    abortSlots.set(this, { aborted: false, reason: undefined, onabort: null });
  }
  get aborted() {
    return abortSlots.get(this).aborted;
  }
  get reason() {
    return abortSlots.get(this).reason;
  }
  get onabort() {
    return abortSlots.get(this).onabort;
  }
  set onabort(value) {
    abortSlots.get(this).onabort = value;
  }
  throwIfAborted() {
    if (this.aborted) throw this.reason;
  }
  static abort(reason) {
    const signal = new AbortSignal(hostToken);
    abort(signal, reason);
    return signal;
  }
  static timeout(ms) {
    ms = Number(ms);
    if (ms < 0 || !Number.isFinite(ms)) throw new RangeError('Invalid timeout');
    const signal = new AbortSignal(hostToken);
    setTimeout(() => abort(signal, platformDOMException('signal timed out', 'TimeoutError')), ms);
    return signal;
  }
  static any(signals) {
    const result = new AbortSignal(hostToken);
    for (const signal of signals) {
      if (!(signal instanceof AbortSignal)) throw new TypeError('Expected AbortSignal');
      if (signal.aborted) {
        abort(result, signal.reason);
        break;
      }
      signal.addEventListener('abort', () => abort(result, signal.reason));
    }
    return result;
  }
}
class AbortController {
  constructor() {
    controllerSlots.set(this, new AbortSignal(hostToken));
  }
  get signal() {
    return controllerSlots.get(this);
  }
  abort(reason) {
    abort(this.signal, reason);
  }
}
expose('AbortSignal', AbortSignal);
expose('AbortController', AbortController);
const decoderSlots = new WeakMap();
const decoderLabels = new Map([
  ['utf-8', 'utf-8'],
  ['utf8', 'utf-8'],
  ['unicode-1-1-utf-8', 'utf-8'],
]);
for (const label of [
  'ansi_x3.4-1968',
  'ascii',
  'cp1252',
  'cp819',
  'csisolatin1',
  'ibm819',
  'iso-8859-1',
  'iso-ir-100',
  'iso8859-1',
  'iso88591',
  'iso_8859-1',
  'iso_8859-1:1987',
  'l1',
  'latin1',
  'us-ascii',
  'windows-1252',
  'x-cp1252',
])
  decoderLabels.set(label, 'windows-1252');
// The web's Latin-1 and ASCII labels use Windows-1252, including its C1
// mappings. Undefined legacy positions retain their control code points.
const windows1252C1 = [
  0x20ac, 0x81, 0x201a, 0x192, 0x201e, 0x2026, 0x2020, 0x2021, 0x2c6, 0x2030, 0x160, 0x2039, 0x152,
  0x8d, 0x17d, 0x8f, 0x90, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0x2dc, 0x2122,
  0x161, 0x203a, 0x153, 0x9d, 0x17e, 0x178,
];
class TextDecoder {
  constructor(label = 'utf-8', options = {}) {
    label = String(label)
      .replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '')
      .toLowerCase();
    const encoding = decoderLabels.get(label);
    if (!encoding) throw new RangeError('Unsupported encoding: ' + label);
    decoderSlots.set(this, {
      encoding,
      fatal: !!options.fatal,
      ignoreBOM: !!options.ignoreBOM,
      bytes: [],
      bom: false,
    });
  }
  get encoding() {
    return decoderSlots.get(this).encoding;
  }
  get fatal() {
    return decoderSlots.get(this).fatal;
  }
  get ignoreBOM() {
    return decoderSlots.get(this).ignoreBOM;
  }
  decode(input, options = {}) {
    const s = decoderSlots.get(this),
      stream = !!options.stream,
      bytes = s.bytes.concat(
        input == null
          ? []
          : Array.from(
              ArrayBuffer.isView(input)
                ? new Uint8Array(input.buffer, input.byteOffset, input.byteLength)
                : new Uint8Array(input),
            ),
      );
    s.bytes = [];
    let output = '';
    if (s.encoding === 'windows-1252') {
      for (const byte of bytes)
        output += String.fromCodePoint(
          byte >= 0x80 && byte <= 0x9f ? windows1252C1[byte - 0x80] : byte,
        );
      return output;
    }
    return decodeUTF8(s, bytes, stream);
  }
}
expose('TextDecoder', TextDecoder);
