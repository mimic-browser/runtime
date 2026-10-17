(() => {
  for (const size of [
    0, 1, 2, 3, 4, 5, 6, 7, 31, 32, 33, 255, 256, 257, 767, 768, 769, 4097, 65537,
  ]) {
    let raw = '';
    for (let i = 0; i < size; i++) raw += String.fromCharCode((i * 73 + 19) & 255);
    const encoded = btoa(raw);
    for (const input of [
      encoded,
      encoded.replace(/=+$/, ''),
      encoded.replace(/.{1,17}/g, '$&\t\r\n\f '),
    ]) {
      if (atob(input) !== raw) throw Error('byte mismatch at length ' + size);
    }
  }
  for (const input of ['AB', 'AB==', 'AAB', 'AAB=']) {
    const raw = atob(input);
    if (
      raw.length !== (input.startsWith('AAB') ? 2 : 1) ||
      raw.split('').some((c) => c.charCodeAt(0) !== 0)
    )
      throw Error('unused tail bits were not ignored');
  }
  for (const input of [
    'A',
    'YQ=',
    'YQ===',
    '=YQ=',
    'AA\v',
    'AA\u00a0',
    'AA\u0100',
    'AA\ud800',
    'AA-_',
  ]) {
    for (const prefix of ['', 'A'.repeat(1024)]) {
      let error;
      try {
        atob(prefix + input);
      } catch (caught) {
        error = caught;
      }
      if (error?.name !== 'InvalidCharacterError')
        throw Error('invalid input accepted: ' + JSON.stringify(prefix + input));
    }
  }
  if (atob(' '.repeat(1024)) !== '') throw Error('native empty input changed');
  if (atob(' '.repeat(1024) + 'AB==') !== '\0')
    throw Error('native decoder changed whitespace or discarded tail bits');
  const largeRaw = 'a\0\u0080\u00ff'.repeat(1024);
  const largeEncoded = btoa(largeRaw);
  const descriptors = [
    [Uint8Array, 'fromBase64'],
    [Uint8Array.prototype, 'byteLength'],
    [Uint8Array.prototype, 'buffer'],
    [Uint8Array.prototype, Symbol.iterator],
    [Object, 'getPrototypeOf'],
  ].map(([target, key]) => [target, key, Object.getOwnPropertyDescriptor(target, key)]);
  try {
    for (const [target, key] of descriptors)
      Object.defineProperty(target, key, {
        configurable: true,
        get() {
          throw Error('author hook invoked');
        },
      });
    if (atob(largeEncoded) !== largeRaw) throw Error('native binary text mismatch');
    let error;
    try {
      atob(largeEncoded + '?');
    } catch (caught) {
      error = caught;
    }
    if (error?.name !== 'InvalidCharacterError')
      throw Error('native invalid input lost DOMException');
  } finally {
    for (const [target, key, descriptor] of descriptors) {
      if (descriptor) Object.defineProperty(target, key, descriptor);
      else delete target[key];
    }
  }
  let calls = 0;
  const input = {
    toString() {
      calls++;
      return largeEncoded;
    },
  };
  if (atob(input) !== largeRaw || calls !== 1) throw Error('input coercion changed');
  const marker = {};
  for (const operation of [atob, btoa]) {
    try {
      operation({
        toString() {
          throw marker;
        },
      });
    } catch (error) {
      if (error === marker) continue;
      throw error;
    }
    throw Error('coercion exception swallowed');
  }
  return true;
})();
