(() => {
  const xml = document.implementation.createDocument(null, 'root');
  const element = document.createElement('div');
  const operations = {
    html: [false, (name) => document.createElement(name)],
    xml: [false, (name) => xml.createElement(name)],
    attribute: [true, (name) => document.createAttribute(name)],
    setAttribute: [true, (name) => element.setAttribute(name, 'x')],
  };
  const forbidden = [0, 9, 10, 12, 13, 32, 47, 62];
  // These admission sets are retained headful Chrome 152 observations, rather
  // than the older XML Name grammar or Mimic's validation expressions.
  const elementInitial = new Set([
    58, 95,
    ...Array.from({ length: 26 }, (_, index) => 65 + index),
    ...Array.from({ length: 26 }, (_, index) => 97 + index),
  ]);
  for (const [operation, [attribute, invoke]] of Object.entries(operations)) {
    for (let code = 0; code < 128; code++) {
      for (const initial of [true, false]) {
        const name = initial ? String.fromCharCode(code) + 'a' : 'a' + String.fromCharCode(code) + 'b';
        const accepted =
          attribute || !initial
            ? !forbidden.includes(code) && (!attribute || code !== 61)
            : elementInitial.has(code);
        let failure;
        try {
          invoke(name);
        } catch (error) {
          failure = error;
        }
        if (accepted ? failure !== undefined : failure?.name !== 'InvalidCharacterError') {
          throw new Error(operation + ': admission differs for ' + code + ', initial=' + initial);
        }
      }
    }
    for (const name of ['', '<bad>']) {
      try {
        invoke(name);
        throw new Error(operation + ': invalid name was accepted');
      } catch (error) {
        if (error.name !== 'InvalidCharacterError') throw error;
      }
    }
    for (const code of [128, 133, 160, 173, 0x1680, 0x2000, 0x200b, 0x2028, 0x2029, 0x3000, 0xd800, 0xffff]) {
      invoke(String.fromCharCode(code) + 'a');
    }
  }
  let invalidTag;
  try {
    document.createElement('<bad>');
  } catch (error) {
    invalidTag = error;
  }
  if (
    invalidTag.message !==
    "Failed to execute 'createElement' on 'Document': The tag name provided ('<bad>') is not a valid name."
  ) {
    throw new Error('invalid tag diagnostic differs');
  }
  for (const owner of [document, xml]) {
    if (owner.createElementNS('urn:x', 'a:b:c').nodeName !== 'a:b') {
      throw new Error('qualified name did not preserve the measured prefix/local pair');
    }
    if (owner.createElementNS('urn:x', '1a:b').nodeName !== '1a:b') {
      throw new Error('namespace prefix used the element local-name grammar');
    }
    for (const name of [':a', 'a:', 'a:1b']) {
      let failure;
      try {
        owner.createElementNS('urn:x', name);
      } catch (error) {
        failure = error;
      }
      if (failure?.name !== 'InvalidCharacterError') throw new Error('invalid qualified name accepted');
    }
  }
  element.setAttributeNS('urn:x', 'a:1b', 'value');
  if (element.getAttributeNS('urn:x', '1b') !== 'value') {
    throw new Error('attribute local name used the element grammar');
  }
  return 'ok';
})();
