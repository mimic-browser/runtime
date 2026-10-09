// Canonical realm-independent primitives shared by Window and Worker.
const bufferViewCheck = ArrayBuffer.isView;
const bufferSourceSlice = ArrayBuffer.prototype.slice;
const bufferSourceLength = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, 'byteLength').get;
const bufferSourceArray = Uint8Array;
const bufferSourceCopy = Uint8Array.prototype.slice;
const bufferSourceTypedPrototype = Object.getPrototypeOf(Uint8Array.prototype);
const bufferSourceTypedGetters = ['buffer', 'byteOffset', 'byteLength'].map(
  (name) => Object.getOwnPropertyDescriptor(bufferSourceTypedPrototype, name).get,
);
const bufferSourceDataGetters = ['buffer', 'byteOffset', 'byteLength'].map(
  (name) => Object.getOwnPropertyDescriptor(DataView.prototype, name).get,
);
const bufferSourceBytes = (value) => {
  if (bufferViewCheck(value)) {
    let parts;
    try {
      parts = bufferSourceTypedGetters.map((get) => Reflect.apply(get, value, []));
    } catch {
      parts = bufferSourceDataGetters.map((get) => Reflect.apply(get, value, []));
    }
    Reflect.apply(bufferSourceLength, parts[0], []);
    return Reflect.apply(bufferSourceCopy, new bufferSourceArray(...parts), []);
  }
  try {
    return new bufferSourceArray(Reflect.apply(bufferSourceSlice, value, [0]));
  } catch {
    throw new TypeError('The provided value is not an ArrayBuffer or ArrayBufferView');
  }
};
const usvString = (value) => {
  if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
  const text = String(value);
  let result = '';
  for (const point of text) {
    const code = point.charCodeAt(0);
    result += point.length === 1 && code >= 0xd800 && code <= 0xdfff ? '\ufffd' : point;
  }
  return result;
};
const decodeUTF8 = (s, bytes, stream) => {
  let output = '';
  const emit = (point) => {
    if (!s.bom) {
      s.bom = true;
      if (point === 0xfeff && !s.ignoreBOM) return;
    }
    output += String.fromCodePoint(point);
  };
  const error = () => {
    if (s.fatal) throw new TypeError('Invalid encoded data');
    emit(0xfffd);
  };
  for (let i = 0; i < bytes.length; ) {
    const start = i,
      b = bytes[i++];
    if (b < 128) {
      emit(b);
      continue;
    }
    const count =
      b >= 0xc2 && b <= 0xdf ? 1 : b >= 0xe0 && b <= 0xef ? 2 : b >= 0xf0 && b <= 0xf4 ? 3 : 0;
    if (!count) {
      error();
      continue;
    }
    let point = b & ((1 << (6 - count)) - 1),
      valid = true;
    for (let j = 0; j < count; j++) {
      if (i === bytes.length) {
        if (stream) {
          s.bytes = bytes.slice(start);
          valid = false;
          i = bytes.length;
        } else {
          error();
          valid = false;
        }
        break;
      }
      const c = bytes[i],
        min = j === 0 && b === 0xe0 ? 0xa0 : j === 0 && b === 0xf0 ? 0x90 : 0x80,
        max = j === 0 && b === 0xed ? 0x9f : j === 0 && b === 0xf4 ? 0x8f : 0xbf;
      if (c < min || c > max) {
        error();
        valid = false;
        break;
      }
      i++;
      point = (point << 6) | (c & 63);
    }
    if (valid) emit(point);
  }
  if (!stream) s.bom = false;
  return output;
};
const textEncoderSlots = new WeakSet();
const encoderArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype),
  encoderArrayTag = Object.getOwnPropertyDescriptor(encoderArrayPrototype, Symbol.toStringTag).get,
  encoderArrayLength = Object.getOwnPropertyDescriptor(encoderArrayPrototype, 'length').get,
  encoderArraySet = Uint8Array.prototype.set;
const utf8Bytes = (input) => {
  const out = [];
  for (let i = 0; i < input.length; i++) {
    let point = input.charCodeAt(i);
    if (point >= 0xd800 && point <= 0xdbff) {
      const next = input.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        point = 0x10000 + ((point - 0xd800) << 10) + (next - 0xdc00);
        i++;
      } else point = 0xfffd;
    } else if (point >= 0xdc00 && point <= 0xdfff) point = 0xfffd;
    if (point <= 0x7f) out.push(point);
    else if (point <= 0x7ff) out.push(0xc0 | (point >> 6), 0x80 | (point & 63));
    else if (point <= 0xffff)
      out.push(0xe0 | (point >> 12), 0x80 | ((point >> 6) & 63), 0x80 | (point & 63));
    else
      out.push(
        0xf0 | (point >> 18),
        0x80 | ((point >> 12) & 63),
        0x80 | ((point >> 6) & 63),
        0x80 | (point & 63),
      );
  }
  return out;
};
class TextEncoder {
  constructor() {
    textEncoderSlots.add(this);
  }
  get encoding() {
    if (!textEncoderSlots.has(this)) throw new TypeError('Illegal invocation');
    return 'utf-8';
  }
  encode(input = '') {
    if (!textEncoderSlots.has(this)) throw new TypeError('Illegal invocation');
    return new Uint8Array(utf8Bytes(usvString(input)));
  }
  encodeInto(source, destination) {
    if (!textEncoderSlots.has(this)) throw new TypeError('Illegal invocation');
    if (arguments.length < 2) throw new TypeError('Not enough arguments');
    source = usvString(source);
    if (encoderArrayTag.call(destination) !== 'Uint8Array')
      throw new TypeError(
        "Failed to execute 'encodeInto' on 'TextEncoder': parameter 2 is not of type 'Uint8Array'.",
      );
    // Validate the native view (including detachment), then use its internal
    // length and captured write operation rather than public shadowable fields.
    encoderArraySet.call(destination, []);
    const capacity = encoderArrayLength.call(destination);
    let read = 0,
      written = 0;
    for (const scalar of source) {
      const bytes = utf8Bytes(scalar);
      if (written + bytes.length > capacity) break;
      encoderArraySet.call(destination, bytes, written);
      written += bytes.length;
      read += scalar.length;
    }
    return { read, written };
  }
}
Object.defineProperty(TextEncoder.prototype, Symbol.toStringTag, {
  value: 'TextEncoder',
  configurable: true,
});
for (const name of ['encode', 'encodeInto']) markNative(TextEncoder.prototype[name], name);
markNative(
  Object.getOwnPropertyDescriptor(TextEncoder.prototype, 'encoding').get,
  'encoding',
  'get ',
);
const domExceptionSlots = new WeakMap();
const domExceptionString = (value) => {
  if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
  return String(value);
};
const domExceptionCodes = {
  IndexSizeError: 1,
  HierarchyRequestError: 3,
  WrongDocumentError: 4,
  InvalidCharacterError: 5,
  NoModificationAllowedError: 7,
  NotFoundError: 8,
  NotSupportedError: 9,
  InUseAttributeError: 10,
  InvalidStateError: 11,
  SyntaxError: 12,
  InvalidModificationError: 13,
  NamespaceError: 14,
  InvalidAccessError: 15,
  TypeMismatchError: 17,
  SecurityError: 18,
  NetworkError: 19,
  AbortError: 20,
  URLMismatchError: 21,
  QuotaExceededError: 22,
  TimeoutError: 23,
  InvalidNodeTypeError: 24,
  DataCloneError: 25,
};
// DOMException instances inherit Error.prototype, but the interface object
// inherits Function.prototype. Avoid class extends Error, whose super lookup
// would also require the incorrect constructor-object inheritance.
const domExceptionInitialize = host.initializeExceptionState;
const domExceptionError = Error;
const domExceptionConstruct = Reflect.construct;
const domExceptionApply = Reflect.apply;
const domExceptionCaptureStackTrace = Error.captureStackTrace;
class DOMException {
  constructor(message = '', name = 'Error') {
    message = domExceptionString(message);
    name = domExceptionString(name);
    const error = domExceptionConstruct(domExceptionError, [], new.target);
    // DOMException has Error classification, but unlike ordinary Error its
    // formatter reads the current interface message rather than an original
    // Error-constructor message. Remove that internal state at creation only.
    if (typeof domExceptionInitialize === 'function') domExceptionInitialize(error);
    domExceptionSlots.set(error, { message, name });
    // Preserve the engine's Error.isError classification without publishing
    // Error's own stack/message properties on the DOMException instance.
    delete error.stack;
    return error;
  }
  get name() {
    return domExceptionSlots.get(this).name;
  }
  get message() {
    return domExceptionSlots.get(this).message;
  }
  get code() {
    const name = domExceptionSlots.get(this).name;
    return Object.prototype.hasOwnProperty.call(domExceptionCodes, name)
      ? domExceptionCodes[name]
      : 0;
  }
}
Object.setPrototypeOf(DOMException.prototype, Error.prototype);
// Chrome's author-created DOMException has no own stack. Platform-created
// exceptions capture one lazily at their API creation boundary. Capture through
// the engine; do not format, filter, or replace author-visible frame strings.
const platformDOMException = (message, name) => {
  const error = new DOMException(message, name);
  if (typeof domExceptionCaptureStackTrace === 'function')
    domExceptionApply(domExceptionCaptureStackTrace, domExceptionError, [error]);
  return error;
};
// WebIDL pair iterators consult current state on every advance. Reaching the
// current end does not permanently exhaust them: later appends are observable.
const pairIteratorPrototype = Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]()));
const pairIteratorFactory = (name, read) => {
  const slots = new WeakMap(),
    prototype = {
      next() {
        const state = slots.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        const pairs = read(state.receiver);
        if (state.index >= pairs.length) return { value: undefined, done: true };
        const pair = pairs[state.index++];
        return {
          value: state.kind === 0 ? pair[0] : state.kind === 1 ? pair[1] : [pair[0], pair[1]],
          done: false,
        };
      },
    };
  Object.setPrototypeOf(prototype, pairIteratorPrototype);
  Object.defineProperty(prototype, Symbol.toStringTag, {
    value: name + ' Iterator',
    configurable: true,
  });
  markNative(prototype.next, 'next');
  return (receiver, kind) => {
    read(receiver);
    const iterator = Object.create(prototype);
    slots.set(iterator, { receiver, kind, index: 0 });
    return iterator;
  };
};
const decodeForm = (s) => {
  const input = utf8Bytes(usvString(s).replace(/\+/g, ' ')),
    bytes = [];
  for (let i = 0; i < input.length; i++) {
    if (input[i] === 37 && i + 2 < input.length) {
      const hex = String.fromCharCode(input[i + 1], input[i + 2]);
      if (/^[0-9a-f]{2}$/i.test(hex)) {
        bytes.push(parseInt(hex, 16));
        i += 2;
        continue;
      }
    }
    bytes.push(input[i]);
  }
  return decodeUTF8({ fatal: false, ignoreBOM: true, bom: false, bytes: [] }, bytes, false);
};
const encodeForm = (s) =>
  encodeURIComponent(usvString(s))
    .replace(/%20/g, '+')
    .replace(/[!'()~]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase());
const urlSearchParamsSlots = new WeakMap(),
  urlSearchParamsState = (value) => urlSearchParamsSlots.get(value),
  readSearchParams = (value) => {
    const state = urlSearchParamsState(value);
    if (!state.url) return state.pairs;
    return parseSearchParams(state.url.search);
  },
  writeSearchParams = (value, pairs) => {
    const state = urlSearchParamsState(value);
    if (state.url)
      state.url.search = pairs.length
        ? '?' + pairs.map((x) => encodeForm(x[0]) + '=' + encodeForm(x[1])).join('&')
        : '';
    else state.pairs = pairs;
  },
  parseSearchParams = (input) => {
    const pairs = [],
      text = usvString(input || '').replace(/^\?/, '');
    if (text)
      for (const part of text.split('&')) {
        const i = part.indexOf('=');
        pairs.push([
          decodeForm(i < 0 ? part : part.slice(0, i)),
          decodeForm(i < 0 ? '' : part.slice(i + 1)),
        ]);
      }
    return pairs;
  };
const searchParamsIterator = pairIteratorFactory('URLSearchParams', readSearchParams);
class URLSearchParams {
  constructor(init = '') {
    let pairs = [];
    if (init instanceof URLSearchParams) pairs = readSearchParams(init).map((x) => x.slice());
    else if (typeof init === 'string') pairs = parseSearchParams(init);
    else if (init && typeof init[Symbol.iterator] === 'function') {
      for (const pair of init) pairs.push([usvString(pair[0]), usvString(pair[1])]);
    } else if (init) for (const key of Object.keys(init)) pairs.push([key, usvString(init[key])]);
    urlSearchParamsSlots.set(this, { pairs, url: null });
  }
  get size() {
    return readSearchParams(this).length;
  }
  append(name, value) {
    const p = readSearchParams(this);
    p.push([usvString(name), usvString(value)]);
    writeSearchParams(this, p);
  }
  delete(name, value) {
    name = usvString(name);
    const hasValue = arguments.length > 1,
      valueString = usvString(value);
    writeSearchParams(
      this,
      readSearchParams(this).filter((x) => x[0] !== name || (hasValue && x[1] !== valueString)),
    );
  }
  get(name) {
    name = usvString(name);
    return readSearchParams(this).find((x) => x[0] === name)?.[1] ?? null;
  }
  getAll(name) {
    name = usvString(name);
    return readSearchParams(this)
      .filter((x) => x[0] === name)
      .map((x) => x[1]);
  }
  has(name, value) {
    name = usvString(name);
    const hasValue = arguments.length > 1,
      valueString = usvString(value);
    return readSearchParams(this).some((x) => x[0] === name && (!hasValue || x[1] === valueString));
  }
  set(name, value) {
    name = usvString(name);
    value = usvString(value);
    const p = readSearchParams(this),
      out = [];
    let done = false;
    for (const x of p) {
      if (x[0] === name) {
        if (!done) {
          out.push([name, value]);
          done = true;
        }
      } else out.push(x);
    }
    if (!done) out.push([name, value]);
    writeSearchParams(this, out);
  }
  sort() {
    const p = readSearchParams(this).map((x, i) => [x, i]);
    p.sort((a, b) => (a[0][0] < b[0][0] ? -1 : a[0][0] > b[0][0] ? 1 : a[1] - b[1]));
    writeSearchParams(
      this,
      p.map((x) => x[0]),
    );
  }
  toString() {
    return readSearchParams(this)
      .map((x) => encodeForm(x[0]) + '=' + encodeForm(x[1]))
      .join('&');
  }
  entries() {
    return searchParamsIterator(this, 2);
  }
  keys() {
    return searchParamsIterator(this, 0);
  }
  values() {
    return searchParamsIterator(this, 1);
  }
  forEach(callback, thisArg) {
    readSearchParams(this);
    if (typeof callback !== 'function') throw new TypeError('Callback must be callable');
    for (let i = 0; ; i++) {
      const pairs = readSearchParams(this);
      if (i >= pairs.length) break;
      const [name, value] = pairs[i];
      Reflect.apply(callback, thisArg, [value, name, this]);
    }
  }
}
Object.defineProperty(URLSearchParams.prototype, Symbol.iterator, {
  value: URLSearchParams.prototype.entries,
  writable: true,
  configurable: true,
});
const stringBytes = (s) => {
  const binary = unescape(encodeURIComponent(String(s))),
    out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
};
const bytesString = (bytes) => {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  try {
    return decodeURIComponent(escape(binary));
  } catch {
    return '\ufffd';
  }
};
const blobSlots = new WeakMap(),
  blobState = (value) => blobSlots.get(value),
  blobBytes = (part) => {
    if (part instanceof Blob) return blobState(part).bytes;
    if (part instanceof ArrayBuffer) return new Uint8Array(part);
    if (ArrayBuffer.isView(part))
      return new Uint8Array(part.buffer, part.byteOffset, part.byteLength);
    return stringBytes(String(part));
  };
class Blob {
  constructor(parts = [], options = {}) {
    if (parts == null || typeof parts[Symbol.iterator] !== 'function')
      throw new TypeError(
        "Failed to construct 'Blob': The provided value cannot be converted to a sequence.",
      );
    const chunks = [...parts].map(blobBytes),
      size = chunks.reduce((n, x) => n + x.byteLength, 0),
      bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    const type = String(options.type || '');
    blobSlots.set(this, { bytes, type: /^[\x20-\x7e]*$/.test(type) ? type.toLowerCase() : '' });
  }
  get size() {
    return blobState(this).bytes.byteLength;
  }
  get type() {
    return blobState(this).type;
  }
  slice(start = 0, end = this.size, contentType = '') {
    const state = blobState(this),
      size = this.size;
    start = Number(start);
    end = Number(end);
    start = start < 0 ? Math.max(size + start, 0) : Math.min(start, size);
    end = end < 0 ? Math.max(size + end, 0) : Math.min(end, size);
    return new Blob([end < start ? new Uint8Array(0) : state.bytes.slice(start, end)], {
      type: contentType,
    });
  }
  arrayBuffer() {
    return platformPromiseResolve(blobState(this).bytes.slice().buffer);
  }
  bytes() {
    return platformPromiseResolve(blobState(this).bytes.slice());
  }
  text() {
    return platformPromiseResolve(bytesString(blobState(this).bytes));
  }
  stream() {
    const bytes = blobState(this).bytes.slice();
    return new ReadableStream({
      start(controller) {
        controller.enqueue(bytes);
        controller.close();
      },
    });
  }
}
const fileSlots = new WeakMap();
class File extends Blob {
  constructor(bits, fileName, options = {}) {
    super(bits, options);
    fileSlots.set(this, {
      name: String(fileName).replace(/\//g, ':'),
      lastModified: options.lastModified === undefined ? Date.now() : Number(options.lastModified),
    });
  }
  get name() {
    return fileSlots.get(this).name;
  }
  get lastModified() {
    return fileSlots.get(this).lastModified;
  }
  get webkitRelativePath() {
    return '';
  }
}
let formDataConstruction = null,
  formDataEventDispatch = null;
const installFormDataConstruction = (construct, dispatch) => {
    formDataConstruction = construct;
    formDataEventDispatch = dispatch;
  },
  formDataSlots = new WeakMap(),
  formDataState = (value) => {
    const state = formDataSlots.get(value);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  },
  formDataValue = (value, filename) => {
    if (value instanceof Blob) {
      if (value instanceof File && filename === undefined) return value;
      return new File([value], filename === undefined ? 'blob' : String(filename), {
        type: value.type,
        lastModified: value instanceof File ? value.lastModified : Date.now(),
      });
    }
    return String(value);
  };
class FormData {
  constructor(form = undefined, submitter = undefined) {
    formDataSlots.set(this, []);
    if (form !== undefined) {
      const construct = formDataConstruction;
      if (typeof construct !== 'function')
        throw new TypeError(
          "Failed to construct 'FormData': parameter 1 is not of type 'HTMLFormElement'.",
        );
      const entries = construct(form, submitter, this);
      for (const [name, value] of entries) formDataState(this).push([name, value]);
      formDataEventDispatch?.(form, this);
    }
  }
  append(name, value, filename = undefined) {
    formDataState(this).push([String(name), formDataValue(value, filename)]);
  }
  delete(name) {
    name = String(name);
    const entries = formDataState(this);
    for (let i = entries.length - 1; i >= 0; i--) if (entries[i][0] === name) entries.splice(i, 1);
  }
  get(name) {
    name = String(name);
    return formDataState(this).find((entry) => entry[0] === name)?.[1] ?? null;
  }
  getAll(name) {
    name = String(name);
    return formDataState(this)
      .filter((entry) => entry[0] === name)
      .map((entry) => entry[1]);
  }
  has(name) {
    name = String(name);
    return formDataState(this).some((entry) => entry[0] === name);
  }
  set(name, value, filename = undefined) {
    name = String(name);
    const entries = formDataState(this),
      next = formDataValue(value, filename),
      first = entries.findIndex((entry) => entry[0] === name);
    if (first < 0) {
      entries.push([name, next]);
      return;
    }
    entries[first] = [name, next];
    for (let i = entries.length - 1; i > first; i--)
      if (entries[i][0] === name) entries.splice(i, 1);
  }
  entries() {
    return formDataState(this).slice()[Symbol.iterator]();
  }
  keys() {
    return formDataState(this)
      .map((entry) => entry[0])
      [Symbol.iterator]();
  }
  values() {
    return formDataState(this)
      .map((entry) => entry[1])
      [Symbol.iterator]();
  }
  forEach(callback, thisArg) {
    for (const [name, value] of formDataState(this)) callback.call(thisArg, value, name, this);
  }
  [Symbol.iterator]() {
    return this.entries();
  }
}
const fileReaderSlots = new WeakMap(),
  fileReaderState = (value) => {
    const state = fileReaderSlots.get(value);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
let FileReader;
const installFileReader = () => {
  if (FileReader) return;
  const emit = (reader, type) => reader.dispatchEvent(new Event(type));
  const read = (reader, blob, kind, encoding = '') => {
    const state = fileReaderState(reader);
    if (!(blob instanceof Blob))
      throw new TypeError(
        "Failed to execute '" + kind + "' on 'FileReader': parameter 1 is not of type 'Blob'.",
      );
    if (state.readyState === 1)
      throw platformDOMException('The object is already busy reading Blobs.', 'InvalidStateError');
    state.readyState = 1;
    state.result = null;
    state.error = null;
    setTimeout(() => {
      const current = fileReaderState(reader);
      if (current.readyState !== 1) return;
      emit(reader, 'loadstart');
      try {
        const bytes = blobState(blob).bytes.slice();
        if (kind === 'readAsArrayBuffer') current.result = bytes.buffer;
        else if (kind === 'readAsBinaryString') {
          let value = '';
          for (const byte of bytes) value += String.fromCharCode(byte);
          current.result = value;
        } else if (kind === 'readAsDataURL') {
          let value = '';
          for (const byte of bytes) value += String.fromCharCode(byte);
          current.result = 'data:' + (blob.type || '') + ';base64,' + btoa(value);
        } else current.result = bytesString(bytes);
        current.readyState = 2;
        emit(reader, 'progress');
        emit(reader, 'load');
        emit(reader, 'loadend');
      } catch (error) {
        current.error =
          error instanceof DOMException
            ? error
            : platformDOMException(String((error && error.message) || error), 'NotReadableError');
        current.result = null;
        current.readyState = 2;
        emit(reader, 'error');
        emit(reader, 'loadend');
      }
    }, 0);
  };
  FileReader = class FileReader extends EventTarget {
    constructor() {
      super();
      fileReaderSlots.set(this, {
        readyState: 0,
        result: null,
        error: null,
        onloadstart: null,
        onprogress: null,
        onload: null,
        onabort: null,
        onerror: null,
        onloadend: null,
      });
    }
    get readyState() {
      return fileReaderState(this).readyState;
    }
    get result() {
      return fileReaderState(this).result;
    }
    get error() {
      return fileReaderState(this).error;
    }
    abort() {
      const state = fileReaderState(this);
      if (state.readyState !== 1) {
        state.result = null;
        return;
      }
      state.readyState = 2;
      state.result = null;
      state.error = platformDOMException('An ongoing operation was aborted.', 'AbortError');
      emit(this, 'abort');
      emit(this, 'loadend');
    }
    readAsArrayBuffer(blob) {
      read(this, blob, 'readAsArrayBuffer');
    }
    readAsBinaryString(blob) {
      read(this, blob, 'readAsBinaryString');
    }
    readAsText(blob, encoding = '') {
      read(this, blob, 'readAsText', encoding);
    }
    readAsDataURL(blob) {
      read(this, blob, 'readAsDataURL');
    }
  };
  for (const name of ['onloadstart', 'onprogress', 'onload', 'onabort', 'onerror', 'onloadend'])
    Object.defineProperty(FileReader.prototype, name, {
      get() {
        return fileReaderState(this)[name];
      },
      set(value) {
        fileReaderState(this)[name] =
          typeof value === 'function' || (value && typeof value === 'object') ? value : null;
      },
      enumerable: true,
      configurable: true,
    });
};
const blobURLs = new Map();
let createMediaSourceObjectURL;
const urlSlots = new WeakMap(),
  urlState = (value) => urlSlots.get(value),
  urlParts = (value) => host.urlParts(urlState(value).href),
  setURLPart = (value, name, part) => {
    const state = urlState(value);
    state.href = host.setURLPart(state.href, name, String(part));
  };
class URL {
  constructor(input, base = undefined) {
    input = String(input);
    base = base === undefined ? '' : String(base);
    let href;
    try {
      href = host.urlParts(input, base).href;
    } catch {
      throw new TypeError('Invalid URL');
    }
    urlSlots.set(this, { href, searchParams: null });
  }
  get href() {
    return urlState(this).href;
  }
  set href(v) {
    const state = urlState(this);
    try {
      state.href = host.urlParts(String(v), '').href;
    } catch {
      throw new TypeError('Invalid URL');
    }
  }
  get origin() {
    return urlParts(this).origin;
  }
  get protocol() {
    return urlParts(this).protocol;
  }
  set protocol(v) {
    setURLPart(this, 'protocol', v);
  }
  get username() {
    return urlParts(this).username;
  }
  set username(v) {
    setURLPart(this, 'username', v);
  }
  get password() {
    return urlParts(this).password;
  }
  set password(v) {
    setURLPart(this, 'password', v);
  }
  get host() {
    return urlParts(this).host;
  }
  set host(v) {
    setURLPart(this, 'host', v);
  }
  get hostname() {
    return urlParts(this).hostname;
  }
  set hostname(v) {
    setURLPart(this, 'hostname', v);
  }
  get port() {
    return urlParts(this).port;
  }
  set port(v) {
    setURLPart(this, 'port', v);
  }
  get pathname() {
    return urlParts(this).pathname;
  }
  set pathname(v) {
    setURLPart(this, 'pathname', v);
  }
  get search() {
    return urlParts(this).search;
  }
  set search(v) {
    setURLPart(this, 'search', v);
  }
  get searchParams() {
    const state = urlState(this);
    if (!state.searchParams) {
      state.searchParams = new URLSearchParams();
      urlSearchParamsState(state.searchParams).url = this;
    }
    return state.searchParams;
  }
  get hash() {
    return urlParts(this).hash;
  }
  set hash(v) {
    setURLPart(this, 'hash', v);
  }
  toString() {
    return this.href;
  }
  toJSON() {
    return this.href;
  }
  static canParse(input, base) {
    try {
      new URL(input, base);
      return true;
    } catch {
      return false;
    }
  }
  static parse(input, base) {
    try {
      return new URL(input, base);
    } catch {
      return null;
    }
  }
  static createObjectURL(object) {
    if (createMediaSourceObjectURL) {
      const result = createMediaSourceObjectURL(object);
      if (result !== undefined) return result;
    }
    if (!(object instanceof Blob))
      throw new TypeError(
        "Failed to execute 'createObjectURL' on 'URL': Overload resolution failed.",
      );
    const value = host.createObjectURL(Array.from(blobState(object).bytes), object.type);
    blobURLs.set(value, object);
    return value;
  }
  static revokeObjectURL(value) {
    value = String(value);
    blobURLs.delete(value);
    host.revokeObjectURL(value);
  }
}
const headersSlots = new WeakMap();
// WebIDL checks the receiver and required arguments before any user conversion.
const headersState = (receiver, count = 0, required = 0) => {
  const state = headersSlots.get(receiver);
  if (!state) throw new TypeError('Illegal invocation');
  if (count < required) throw new TypeError('Not enough arguments');
  return state;
};
const headerByteString = (value) => {
  if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
  const text = String(value);
  for (let i = 0; i < text.length; i++)
    if (text.charCodeAt(i) > 255) throw new TypeError('Value is not a ByteString');
  return text;
};
const headerName = (value) => {
    const name = headerByteString(value).toLowerCase();
    if (!name || /[^!#$%&'*+.^_`|~0-9a-z-]/.test(name)) throw new TypeError('Invalid header name');
    return name;
  },
  headerValue = (value) => {
    const text = headerByteString(value).replace(/^[\t\r\n ]+|[\t\r\n ]+$/g, '');
    if (/[\0\r\n]/.test(text)) throw new TypeError('Invalid header value');
    return text;
  };
const headerLists = new WeakMap(),
  readHeaderPairs = (receiver) => {
    const state = headersState(receiver);
    let pairs = headerLists.get(state);
    if (!pairs) {
      pairs = [...state].sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
      headerLists.set(state, pairs);
    }
    return pairs;
  },
  headersIterator = pairIteratorFactory('Headers', readHeaderPairs);
class Headers {
  constructor(init) {
    const values = new Map();
    headersSlots.set(this, values);
    if (init === undefined) return;
    if (init === null || (typeof init !== 'object' && typeof init !== 'function'))
      throw new TypeError('Headers initializer must be an object');
    // Complete WebIDL union/sequence conversion before mutating the header list.
    // Filling the list must not call an override of the public append method.
    const pairs = [],
      iterator = init[Symbol.iterator];
    if (iterator !== undefined && iterator !== null) {
      if (typeof iterator !== 'function') throw new TypeError('Initializer is not iterable');
      const iterable = {
        [Symbol.iterator]() {
          return iterator.call(init);
        },
      };
      for (const pair of iterable) {
        if (pair === null || (typeof pair !== 'object' && typeof pair !== 'function'))
          throw new TypeError('Header pair must be a sequence');
        const converted = [];
        for (const item of pair) converted.push(headerByteString(item));
        pairs.push(converted);
      }
    } else {
      for (const key of Reflect.ownKeys(init))
        if (Object.prototype.propertyIsEnumerable.call(init, key))
          pairs.push([headerByteString(key), headerByteString(init[key])]);
    }
    for (const pair of pairs) {
      if (pair.length !== 2) throw new TypeError('Header pair must contain exactly two items');
      const name = headerName(pair[0]),
        value = headerValue(pair[1]);
      values.set(name, values.has(name) ? values.get(name) + ', ' + value : value);
    }
  }
  append(name, value) {
    const values = headersState(this, arguments.length, 2);
    name = headerByteString(name);
    value = headerByteString(value);
    const key = headerName(name),
      next = headerValue(value);
    values.set(key, values.has(key) ? values.get(key) + ', ' + next : next);
    headerLists.delete(values);
  }
  delete(name) {
    const state = headersState(this, arguments.length, 1);
    state.delete(headerName(name));
    headerLists.delete(state);
  }
  get(name) {
    return headersState(this, arguments.length, 1).get(headerName(name)) ?? null;
  }
  has(name) {
    return headersState(this, arguments.length, 1).has(headerName(name));
  }
  set(name, value) {
    const state = headersState(this, arguments.length, 2);
    name = headerByteString(name);
    value = headerByteString(value);
    state.set(headerName(name), headerValue(value));
    headerLists.delete(state);
  }
  entries() {
    return headersIterator(this, 2);
  }
  keys() {
    return headersIterator(this, 0);
  }
  values() {
    return headersIterator(this, 1);
  }
  forEach(callback, thisArg) {
    headersState(this, arguments.length, 1);
    if (typeof callback !== 'function') throw new TypeError('Callback must be callable');
    for (let i = 0; ; i++) {
      const pairs = readHeaderPairs(this);
      if (i >= pairs.length) break;
      const [name, value] = pairs[i];
      Reflect.apply(callback, thisArg, [value, name, this]);
    }
  }
}
Object.defineProperty(Headers.prototype, Symbol.iterator, {
  value: Headers.prototype.entries,
  writable: true,
  configurable: true,
});
Object.defineProperty(Headers.prototype, Symbol.toStringTag, {
  value: 'Headers',
  configurable: true,
});
