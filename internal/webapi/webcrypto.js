// Shared Window/Worker WebCrypto bindings. Key bytes stay in private slots;
// native cryptographic primitives receive copied BufferSource observations.
const cryptoKeySlots = new WeakMap();
const cryptoBytes = (value) => bufferSourceBytes(value);
const cryptoError = (name, message) => {
  throw platformDOMException(message, name);
};
const cryptoAlgorithmName = (algorithm) => {
  if (typeof algorithm !== 'string' && (!algorithm || typeof algorithm !== 'object'))
    throw new TypeError('Invalid algorithm');
  const name = typeof algorithm === 'string' ? algorithm : algorithm.name;
  if (name === undefined) throw new TypeError('Algorithm name is required');
  const normalized = String(name).toUpperCase();
  if (
    ![
      'SHA-1',
      'SHA-256',
      'SHA-384',
      'SHA-512',
      'HMAC',
      'AES-GCM',
      'AES-CBC',
      'AES-CTR',
      'AES-KW',
      'PBKDF2',
      'HKDF',
      'RSA-OAEP',
    ].includes(normalized)
  )
    cryptoError('NotSupportedError', 'Unrecognized algorithm name');
  return normalized;
};
const cryptoHash = (algorithm) => {
  const name = cryptoAlgorithmName(algorithm);
  if (!name.startsWith('SHA-')) cryptoError('NotSupportedError', 'Unrecognized hash algorithm');
  return name;
};
const cryptoInteger = (value) => Number(value) >>> 0;
const cryptoKey = (value) => {
  const slot = cryptoKeySlots.get(value);
  if (!slot) throw new TypeError('The provided value is not a CryptoKey');
  return slot;
};
class CryptoKey {
  constructor() {
    throw new TypeError('Illegal constructor');
  }
  get type() {
    return cryptoKey(this).type;
  }
  get extractable() {
    return cryptoKey(this).extractable;
  }
  get algorithm() {
    return cryptoKey(this).exposedAlgorithm;
  }
  get usages() {
    return cryptoKey(this).usages.slice();
  }
}
const cryptoRun = (request) => {
  const result = JSON.parse(host.webCrypto(JSON.stringify(request)));
  if (result.error) cryptoError('OperationError', result.error);
  return 'verified' in result ? result.verified : new Uint8Array(result.bytes).buffer;
};
const cryptoResult = (callback) => {
  try {
    const result = callback();
    // A retained Window can still expose SubtleCrypto after its Document is
    // inactive, but successful operations started there cannot deliver a
    // completion. Operations started before removal keep their settled promise.
    // Workers have no Document lifecycle.
    if (host.documentActive && !host.documentActive()) return new platformPromise(() => {});
    return platformPromiseResolve(result);
  } catch (error) {
    return platformPromiseReject(error);
  }
};
const cryptoUsages = (name, usages, empty = false) => {
  const values = Array.from(usages, String);
  const allowed =
    name === 'HMAC'
      ? ['sign', 'verify']
      : ['PBKDF2', 'HKDF'].includes(name)
        ? ['deriveKey', 'deriveBits']
        : name === 'AES-KW'
          ? ['wrapKey', 'unwrapKey']
          : ['encrypt', 'decrypt', 'wrapKey', 'unwrapKey'];
  if (values.some((value) => !allowed.includes(value)) || (!empty && !values.length))
    cryptoError('SyntaxError', 'Invalid key usages');
  return values.filter((value, index) => values.indexOf(value) === index);
};
const cryptoMakeKey = (algorithm, bytes, extractable, usages, type = 'secret') => {
  bytes = Uint8Array.from(bytes);
  if (algorithm.name === 'HMAC' && algorithm.length % 8)
    bytes[bytes.length - 1] &= 0xff << (8 - (algorithm.length % 8));
  const key = Object.create(CryptoKey.prototype);
  const exposedAlgorithm = { ...algorithm };
  if (algorithm.hash) exposedAlgorithm.hash = { ...algorithm.hash };
  if (algorithm.publicExponent) exposedAlgorithm.publicExponent = algorithm.publicExponent.slice();
  cryptoKeySlots.set(key, {
    type,
    algorithm,
    exposedAlgorithm,
    bytes: Array.from(bytes),
    extractable: Boolean(extractable),
    usages,
  });
  return key;
};
const cryptoBase64URL = (bytes) =>
  btoa(String.fromCharCode(...bytes))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replace(/=+$/, '');
const cryptoDecodeBase64URL = (text) => {
  if (typeof text !== 'string' || !/^[A-Za-z0-9_-]*={0,2}$/.test(text))
    cryptoError('DataError', 'Invalid JWK key data');
  try {
    return Uint8Array.from(atob(text.replaceAll('-', '+').replaceAll('_', '/')), (c) =>
      c.charCodeAt(0),
    );
  } catch {
    cryptoError('DataError', 'Invalid JWK key data');
  }
};
const cryptoJWKAlgorithm = (algorithm) =>
  algorithm.name === 'HMAC'
    ? 'HS' + (algorithm.hash.name === 'SHA-1' ? '1' : algorithm.hash.name.slice(4))
    : 'A' + algorithm.length + algorithm.name.slice(4).replace('-', '');
const cryptoSecretAlgorithm = (name, algorithm, bytes, generating = false) => {
  if (name === 'HMAC') {
    if (!algorithm || algorithm.hash === undefined) throw new TypeError('Hash is required');
    const hash = cryptoHash(algorithm.hash);
    const bits = generating
      ? hash === 'SHA-384' || hash === 'SHA-512'
        ? 1024
        : 512
      : bytes.length * 8;
    const length = algorithm.length === undefined ? bits : cryptoInteger(algorithm.length);
    if (!length || (!generating && (length > bits || length <= bits - 8)))
      cryptoError(generating ? 'OperationError' : 'DataError', 'Invalid HMAC key length');
    return { name, hash: { name: hash }, length };
  }
  if (name.startsWith('AES-')) {
    const length = generating ? cryptoInteger(algorithm.length) : bytes.length * 8;
    if (![128, 192, 256].includes(length))
      cryptoError(generating ? 'OperationError' : 'DataError', 'Invalid AES key length');
    return { name, length };
  }
  if (['PBKDF2', 'HKDF'].includes(name)) return { name };
  cryptoError('NotSupportedError', 'Unsupported key algorithm');
};
class SubtleCrypto {
  constructor() {
    throw new TypeError('Illegal constructor');
  }
  digest(algorithm, data) {
    return cryptoResult(() =>
      cryptoRun({
        operation: 'digest',
        hash: cryptoHash(algorithm),
        data: Array.from(cryptoBytes(data)),
      }),
    );
  }
  generateKey(algorithm, extractable, keyUsages) {
    return cryptoResult(() => {
      const name = cryptoAlgorithmName(algorithm),
        usages = cryptoUsages(name, keyUsages);
      const normalized = cryptoSecretAlgorithm(name, algorithm, [], true);
      const bytes = new Uint8Array(
        cryptoRun({ operation: 'random', length: Math.ceil(normalized.length / 8) }),
      );
      return cryptoMakeKey(normalized, bytes, extractable, usages);
    });
  }
  importKey(format, keyData, algorithm, extractable, keyUsages) {
    return cryptoResult(() => {
      format = String(format);
      const name = cryptoAlgorithmName(algorithm),
        usages = cryptoUsages(name, keyUsages);
      if (format === 'spki' && name === 'RSA-OAEP' && host.subtleImportRSAOAEP) {
        const bytes = cryptoBytes(keyData),
          hash = cryptoHash(algorithm.hash);
        if (usages.some((usage) => !['encrypt', 'wrapKey'].includes(usage)))
          cryptoError('SyntaxError', 'Invalid public key usages');
        let metadata;
        try {
          metadata = String(host.subtleImportRSAOAEP(Array.from(bytes))).split('|');
        } catch {
          cryptoError('DataError', 'Invalid RSA public key');
        }
        return cryptoMakeKey(
          {
            name,
            hash: { name: hash },
            modulusLength: Number(metadata[0]),
            publicExponent: new Uint8Array(metadata[1].split(',').map(Number)),
          },
          bytes,
          extractable,
          usages,
          'public',
        );
      }
      let bytes;
      if (format === 'raw') bytes = cryptoBytes(keyData);
      else if (format === 'jwk') {
        if (!keyData || keyData.kty !== 'oct' || keyData.k === undefined)
          cryptoError('DataError', 'Invalid JWK');
        if (keyData.ext === false && extractable)
          cryptoError('DataError', 'JWK is not extractable');
        if (
          keyData.use !== undefined &&
          keyData.use !== (name === 'HMAC' ? 'sig' : 'enc') &&
          usages.length
        )
          cryptoError('DataError', 'Invalid JWK use');
        if (
          keyData.key_ops !== undefined &&
          (new Set(keyData.key_ops).size !== keyData.key_ops.length ||
            usages.some((usage) => !keyData.key_ops.includes(usage)))
        )
          cryptoError('DataError', 'Invalid JWK key operations');
        bytes = cryptoDecodeBase64URL(keyData.k);
      } else cryptoError('NotSupportedError', 'Unsupported key format');
      const normalized = cryptoSecretAlgorithm(name, algorithm, bytes);
      if (['HKDF', 'PBKDF2'].includes(name) && (extractable || format !== 'raw'))
        cryptoError('SyntaxError', 'Derivation keys must be nonextractable raw keys');
      if (
        format === 'jwk' &&
        keyData.alg !== undefined &&
        keyData.alg !== cryptoJWKAlgorithm(normalized)
      )
        cryptoError('DataError', 'JWK algorithm mismatch');
      return cryptoMakeKey(normalized, bytes, extractable, usages);
    });
  }
  exportKey(format, key) {
    return cryptoResult(() => {
      const slot = cryptoKey(key);
      format = String(format);
      if (!slot.extractable) cryptoError('InvalidAccessError', 'Key is not extractable');
      if (format === 'raw' && slot.type === 'secret') return new Uint8Array(slot.bytes).buffer;
      if (format === 'spki' && slot.type === 'public') return new Uint8Array(slot.bytes).buffer;
      if (format === 'jwk' && slot.type === 'secret')
        return {
          kty: 'oct',
          k: cryptoBase64URL(slot.bytes),
          alg: cryptoJWKAlgorithm(slot.algorithm),
          key_ops: slot.usages.slice(),
          ext: slot.extractable,
        };
      cryptoError('NotSupportedError', 'Unsupported key format');
    });
  }
  encrypt(algorithm, key, data) {
    return cryptoResult(() => cryptoCipher('encrypt', algorithm, key, data));
  }
  decrypt(algorithm, key, data) {
    return cryptoResult(() => cryptoCipher('decrypt', algorithm, key, data));
  }
  sign(algorithm, key, data) {
    return cryptoResult(() => cryptoSign('sign', algorithm, key, data));
  }
  verify(algorithm, key, signature, data) {
    return cryptoResult(() => cryptoSign('verify', algorithm, key, data, signature));
  }
  deriveBits(algorithm, key, length) {
    return cryptoResult(() => cryptoDerive(algorithm, key, length, 'deriveBits'));
  }
  deriveKey(algorithm, key, derivedKeyType, extractable, keyUsages) {
    return cryptoResult(() => {
      const name = cryptoAlgorithmName(derivedKeyType),
        normalized = cryptoSecretAlgorithm(name, derivedKeyType, [], true);
      const usages = cryptoUsages(name, keyUsages);
      const bytes = new Uint8Array(cryptoDerive(algorithm, key, normalized.length, 'deriveKey'));
      return cryptoMakeKey(normalized, bytes, extractable, usages);
    });
  }
  wrapKey(format, key, wrappingKey, algorithm) {
    return this.exportKey(format, key).then((exported) => {
      const data = format === 'jwk' ? new TextEncoder().encode(JSON.stringify(exported)) : exported;
      return cryptoCipher('encrypt', algorithm, wrappingKey, data, 'wrapKey');
    });
  }
  unwrapKey(
    format,
    wrappedKey,
    unwrappingKey,
    unwrapAlgorithm,
    unwrappedKeyAlgorithm,
    extractable,
    keyUsages,
  ) {
    return cryptoResult(() =>
      cryptoCipher('decrypt', unwrapAlgorithm, unwrappingKey, wrappedKey, 'unwrapKey'),
    ).then((bytes) => {
      let data = bytes;
      if (format === 'jwk') {
        try {
          data = JSON.parse(new TextDecoder().decode(bytes));
        } catch {
          cryptoError('DataError', 'Invalid wrapped JWK');
        }
      }
      return this.importKey(format, data, unwrappedKeyAlgorithm, extractable, keyUsages);
    });
  }
}
const cryptoAccess = (algorithm, key, usage) => {
  const name = cryptoAlgorithmName(algorithm),
    slot = cryptoKey(key);
  if (slot.algorithm.name !== name || !slot.usages.includes(usage))
    cryptoError('InvalidAccessError', 'Key does not permit this operation');
  return slot;
};
const cryptoCipher = (operation, algorithm, key, data, usage = operation) => {
  const slot = cryptoAccess(algorithm, key, usage),
    name = slot.algorithm.name;
  const request = { operation, name, key: slot.bytes, data: Array.from(cryptoBytes(data)) };
  if (name === 'RSA-OAEP' && operation === 'encrypt' && host.subtleRSAOAEPEncrypt) {
    try {
      return new Uint8Array(
        host.subtleRSAOAEPEncrypt(
          slot.algorithm.hash.name,
          slot.bytes,
          request.data,
          algorithm.label === undefined ? [] : Array.from(cryptoBytes(algorithm.label)),
        ),
      ).buffer;
    } catch {
      cryptoError('OperationError', 'RSA-OAEP operation failed');
    }
  }
  if (name === 'AES-GCM' || name === 'AES-CBC') {
    if (algorithm.iv === undefined) throw new TypeError('IV is required');
    request.iv = Array.from(cryptoBytes(algorithm.iv));
    if (name === 'AES-GCM') {
      request.tagLength =
        algorithm.tagLength === undefined ? 128 : cryptoInteger(algorithm.tagLength);
      request.additionalData =
        algorithm.additionalData === undefined
          ? []
          : Array.from(cryptoBytes(algorithm.additionalData));
    }
  } else if (name === 'AES-CTR') {
    if (algorithm.counter === undefined || algorithm.length === undefined)
      throw new TypeError('Counter and length are required');
    request.iv = Array.from(cryptoBytes(algorithm.counter));
    request.length = cryptoInteger(algorithm.length);
  } else if (name !== 'AES-KW' || !['wrapKey', 'unwrapKey'].includes(usage))
    cryptoError('NotSupportedError', 'Unsupported cipher');
  return cryptoRun(request);
};
const cryptoSign = (operation, algorithm, key, data, signature) => {
  const slot = cryptoAccess(algorithm, key, operation);
  if (slot.algorithm.name !== 'HMAC')
    cryptoError('NotSupportedError', 'Unsupported signature algorithm');
  return cryptoRun({
    operation,
    name: 'HMAC',
    hash: slot.algorithm.hash.name,
    key: slot.bytes,
    data: Array.from(cryptoBytes(data)),
    signature: signature === undefined ? [] : Array.from(cryptoBytes(signature)),
  });
};
const cryptoDerive = (algorithm, key, length, usage) => {
  const slot = cryptoAccess(algorithm, key, usage),
    name = slot.algorithm.name;
  if (!['PBKDF2', 'HKDF'].includes(name))
    cryptoError('NotSupportedError', 'Unsupported derivation algorithm');
  length = cryptoInteger(length);
  if (!length || length % 8)
    cryptoError('OperationError', 'Derived length must be a positive multiple of eight');
  if (algorithm.salt === undefined || algorithm.hash === undefined)
    throw new TypeError('Salt and hash are required');
  const request = {
    operation: 'deriveBits',
    name,
    key: slot.bytes,
    length: length / 8,
    hash: cryptoHash(algorithm.hash),
    salt: Array.from(cryptoBytes(algorithm.salt)),
  };
  if (name === 'HKDF') {
    if (algorithm.info === undefined) throw new TypeError('Info is required');
    request.info = Array.from(cryptoBytes(algorithm.info));
  } else {
    if (algorithm.iterations === undefined) throw new TypeError('Iterations are required');
    request.iterations = cryptoInteger(algorithm.iterations);
  }
  return cryptoRun(request);
};
