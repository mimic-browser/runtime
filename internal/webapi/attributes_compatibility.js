// Attached Attr values are projections of the canonical host attribute map.
// Only a removed Attr owns a detached value; wrappers and NamedNodeMap objects
// retain identity without copying the element's mutable attribute state.
const attributeCompatibility = (() => {
  const slots = new WeakMap(),
    maps = new WeakMap(),
    attributes = new WeakMap();
  const member = (prototype, name, value) => {
    Object.defineProperty(value, 'name', { value: name, configurable: true });
    markNative(value, name);
    Object.defineProperty(prototype, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const accessor = (prototype, name, get, set) => {
    if (get) Object.defineProperty(get, 'name', { value: 'get ' + name, configurable: true });
    if (set) Object.defineProperty(set, 'name', { value: 'set ' + name, configurable: true });
    markNative(get, name, 'get ');
    markNative(set, name, 'set ');
    Object.defineProperty(prototype, name, { get, set, enumerable: true, configurable: true });
  };
  const htmlElement = (value) => {
    if (!(value instanceof HTMLElement) || !elementSlot(value))
      throw new TypeError('Illegal invocation');
    return value;
  };
  const setSlotAttribute = Element.prototype.setAttribute;
  // Enumerated IDL attributes expose canonical keywords but preserve the original
  // attribute text. All writes go through the existing attribute/reaction path.
  const asciiLower = (value) => value.replace(/[A-Z]/g, (c) => c.toLowerCase());
  const reflectEnumeration = (C, property, canonicalize) => {
    if (!C) return;
    const attribute = property.toLowerCase();
    const check = (value) => {
      if (!(value instanceof C) || !elementSlot(value)) throw new TypeError('Illegal invocation');
    };
    accessor(
      C.prototype,
      property,
      function () {
        check(this);
        return canonicalize(Reflect.apply(intrinsicGetAttribute, this, [attribute]), this);
      },
      function (value) {
        check(this);
        if (typeof value === 'symbol')
          throw new TypeError(
            "Failed to set the '" +
              property +
              "' property on '" +
              C.name +
              "': Cannot convert a Symbol value to a string",
          );
        Reflect.apply(setSlotAttribute, this, [attribute, String(value)]);
      },
    );
  };
  for (const [name, keywords] of [
    ['dir', 'ltr rtl auto'],
    ['inputMode', 'none text tel url email numeric decimal search'],
    ['enterKeyHint', 'enter done go next previous search send'],
  ]) {
    const allowed = new Set(keywords.split(' '));
    reflectEnumeration(HTMLElement, name, (raw) => {
      const value = asciiLower(raw || '');
      return allowed.has(value) ? value : '';
    });
  }
  accessor(
    HTMLElement.prototype,
    'autofocus',
    function () {
      htmlElement(this);
      return this.hasAttribute('autofocus');
    },
    function (value) {
      htmlElement(this);
      this.toggleAttribute('autofocus', Boolean(value));
    },
  );
  const autofillFields = new Set(
    'name honorific-prefix given-name additional-name family-name honorific-suffix nickname username new-password current-password one-time-code organization-title organization street-address address-line1 address-line2 address-line3 address-level4 address-level3 address-level2 address-level1 country country-name postal-code cc-name cc-given-name cc-additional-name cc-family-name cc-number cc-exp cc-exp-month cc-exp-year cc-csc cc-type transaction-currency transaction-amount language bday bday-day bday-month bday-year sex url photo'.split(
      ' ',
    ),
  );
  const contactFields = new Set(
    'tel tel-country-code tel-national tel-area-code tel-local tel-local-prefix tel-local-suffix tel-extension email impp'.split(
      ' ',
    ),
  );
  const autocomplete = (raw, element) => {
    const tokens = asciiLower(raw || '').match(/[^\t\n\f\r ]+/g) || [];
    if (!tokens.length) return '';
    if (tokens.length === 1 && ['on', 'off'].includes(tokens[0]))
      return element.localName === 'input' && element.type === 'hidden' ? '' : tokens[0];
    const result = tokens.join(' ');
    if (tokens.at(-1) === 'webauthn') tokens.pop();
    const field = tokens.pop();
    if (!autofillFields.has(field) && !contactFields.has(field)) return '';
    if (
      contactFields.has(field) &&
      ['home', 'work', 'mobile', 'fax', 'pager'].includes(tokens.at(-1))
    )
      tokens.pop();
    if (['shipping', 'billing'].includes(tokens.at(-1))) tokens.pop();
    if (tokens.length === 1 && tokens[0].startsWith('section-')) tokens.pop();
    return tokens.length ? '' : result;
  };
  for (const C of [
    globalThis.HTMLInputElement,
    globalThis.HTMLTextAreaElement,
    globalThis.HTMLSelectElement,
  ])
    reflectEnumeration(C, 'autocomplete', autocomplete);
  reflectEnumeration(globalThis.HTMLFormElement, 'autocomplete', (raw) =>
    asciiLower(raw || '') === 'off' ? 'off' : 'on',
  );
  reflectEnumeration(globalThis.HTMLMediaElement, 'preload', (raw) => {
    if (raw === '') return 'auto';
    const value = asciiLower(raw || '');
    return ['none', 'metadata', 'auto'].includes(value) ? value : 'metadata';
  });
  // Slot assignment derives from canonical attributes, including property writes.
  for (const [C, name] of [
    [Element, 'slot'],
    [globalThis.HTMLSlotElement, 'name'],
  ])
    if (C)
      accessor(
        C.prototype,
        name,
        function () {
          const data = elementSlot(this);
          if (data?.type !== 'element' || (name === 'name' && data.tagName !== 'SLOT'))
            throw new TypeError('Illegal invocation');
          return host.getAttribute(data.nodeId, name) || '';
        },
        function (value) {
          const data = elementSlot(this);
          if (data?.type !== 'element' || (name === 'name' && data.tagName !== 'SLOT'))
            throw new TypeError('Illegal invocation');
          if (typeof value === 'symbol')
            throw new TypeError('Cannot convert a Symbol value to a string');
          Reflect.apply(setSlotAttribute, this, [name, String(value)]);
        },
      );
  const containingShadowRoot = (node) => {
    for (let current = node?.parentNode; current; current = current.parentNode)
      if (current instanceof ShadowRoot) return current;
    return null;
  };
  const assignedSlot = (node) => {
    const parent = node?.parentNode,
      root = parent && elementShadows.get(parent),
      data = elementSlot(node);
    if (
      !root ||
      shadowSlots.get(root)?.slotAssignment === 'manual' ||
      !['element', 'text'].includes(data?.type)
    )
      return null;
    const name = data.type === 'element' ? host.getAttribute(data.nodeId, 'slot') || '' : '';
    return (
      Array.from(root.querySelectorAll('slot')).find(
        (slot) => (host.getAttribute(elementSlot(slot).nodeId, 'name') || '') === name,
      ) || null
    );
  };
  for (const C of [Element, globalThis.Text])
    if (C)
      accessor(C.prototype, 'assignedSlot', function () {
        if (!elementSlot(this)) throw new TypeError('Illegal invocation');
        return assignedSlot(this);
      });
  if (globalThis.HTMLSlotElement) {
    const slot = (value) => {
      const data = elementSlot(value);
      if (!(value instanceof globalThis.HTMLSlotElement) || data?.tagName !== 'SLOT')
        throw new TypeError('Illegal invocation');
      return value;
    };
    const direct = (value) => {
      value = slot(value);
      const root = containingShadowRoot(value),
        owner = root && shadowSlots.get(root)?.host;
      if (!owner) return [];
      return Array.from(owner.childNodes).filter((node) => assignedSlot(node) === value);
    };
    member(globalThis.HTMLSlotElement.prototype, 'assignedNodes', function (options = {}) {
      let nodes = direct(this);
      if (!options?.flatten) return nodes;
      if (!nodes.length) nodes = Array.from(this.childNodes);
      const result = [];
      for (const node of nodes)
        if (node instanceof globalThis.HTMLSlotElement)
          result.push(...node.assignedNodes({ flatten: true }));
        else result.push(node);
      return result;
    });
    member(globalThis.HTMLSlotElement.prototype, 'assignedElements', function (options = {}) {
      return this.assignedNodes(options).filter((node) => node instanceof Element);
    });
  }
  for (const C of [globalThis.HTMLInputElement, globalThis.HTMLTextAreaElement]) {
    if (typeof C !== 'function') continue;
    const control = (value) => {
      if (!(value instanceof C) || !elementSlot(value)) throw new TypeError('Illegal invocation');
      return value;
    };
    accessor(
      C.prototype,
      'dirName',
      function () {
        return control(this).getAttribute('dirname') || '';
      },
      function (value) {
        control(this);
        if (typeof value === 'symbol')
          throw new TypeError('Cannot convert a Symbol value to a string');
        this.setAttribute('dirname', String(value));
      },
    );
    accessor(
      C.prototype,
      'maxLength',
      function () {
        const raw = control(this).getAttribute('maxlength') || '',
          match = /^[\t\n\f\r ]*([+-]?\d+)/.exec(raw),
          value = match ? Number(match[1]) : -1;
        return value >= 0 && value <= 2147483647 ? value : -1;
      },
      function (value) {
        control(this);
        value = +value | 0;
        if (value < 0)
          throw platformDOMException('The value provided is negative.', 'IndexSizeError');
        this.setAttribute('maxlength', String(value));
      },
    );
  }
  for (const name of ['translate', 'spellcheck', 'draggable'])
    accessor(
      HTMLElement.prototype,
      name,
      function () {
        let node = htmlElement(this);
        while (node) {
          const value = node.getAttribute(name)?.toLowerCase();
          if (name === 'draggable')
            return value === 'true'
              ? true
              : value === 'false'
                ? false
                : node.localName === 'img' || (node.localName === 'a' && node.hasAttribute('href'));
          if (value === (name === 'translate' ? 'no' : 'false')) return false;
          if (value === '' || value === (name === 'translate' ? 'yes' : 'true')) return true;
          const parent = node.parentNode;
          // Chrome spellcheck crosses a shadow host; translate uses DOM ancestry.
          node =
            parent instanceof HTMLElement
              ? parent
              : name === 'spellcheck' && parent instanceof ShadowRoot
                ? parent.host
                : null;
        }
        return !(name === 'spellcheck' && this.localName === 'input' && this.type === 'password');
      },
      function (value) {
        htmlElement(this).setAttribute(
          name,
          name === 'translate' ? (value ? 'yes' : 'no') : value ? 'true' : 'false',
        );
      },
    );
  const cache = (element) => {
    let value = attributes.get(element);
    if (!value) {
      value = new Map();
      attributes.set(element, value);
    }
    return value;
  };
  const state = (attr) => {
    const value = slots.get(attr);
    if (!value) throw new TypeError('Illegal invocation');
    return value;
  };
  const normalize = (element, name) =>
    element.namespaceURI === 'http://www.w3.org/1999/xhtml'
      ? String(name).toLowerCase()
      : String(name);
  const detach = (attr) => {
    const value = state(attr);
    if (value.owner) {
      value.value = value.owner.getAttribute(value.name) ?? value.value;
      value.document = value.owner.ownerDocument;
      value.owner = null;
    }
    return attr;
  };
  const create = (name, value, document, owner = null, namespace = null) => {
    const attr = Object.create(globalThis.Attr.prototype);
    slots.set(attr, { name, value, document, owner, namespace });
    nonHostNodeBrands.add(attr);
    return attr;
  };
  const get = (element, name) => {
    name = normalize(element, name);
    const value = element.getAttribute(name),
      values = cache(element),
      previous = values.get(name);
    if (value === null) {
      if (previous) {
        detach(previous);
        values.delete(name);
      }
      return null;
    }
    if (previous) {
      state(previous).value = value;
      return previous;
    }
    const attr = create(
      name,
      value,
      element.ownerDocument,
      element,
      host.nodeData(elementSlot(element).nodeId).attributeNamespaces?.[name] || null,
    );
    values.set(name, attr);
    return attr;
  };
  const names = (element) => element.getAttributeNames();
  const nsName = (element, namespace, local) => {
    if (!(element instanceof Element)) throw new TypeError('Illegal invocation');
    return host.attributeNameNS(
      elementSlot(element).nodeId,
      namespace == null ? '' : String(namespace),
      String(local),
    );
  };
  member(Element.prototype, 'getAttributeNS', function (namespace, local) {
    const name = nsName(this, namespace, local);
    return name ? this.getAttribute(name) : null;
  });
  member(Element.prototype, 'hasAttributeNS', function (namespace, local) {
    return !!nsName(this, namespace, local);
  });
  member(Element.prototype, 'removeAttributeNS', function (namespace, local) {
    const name = nsName(this, namespace, local);
    if (name) this.removeAttribute(name);
  });
  member(Element.prototype, 'setAttributeNS', function (namespace, name, value) {
    if (!(this instanceof Element)) throw new TypeError('Illegal invocation');
    if (arguments.length < 3) throw new TypeError('Not enough arguments');
    namespace = namespace == null ? '' : String(namespace);
    name = String(name);
    value = trustedAttributeValue(
      this,
      namespace ? name.split(':').at(-1) : name,
      value,
      namespace,
      "Failed to execute 'setAttributeNS' on 'Element': ",
    );
    name = validateDOMQualifiedName(namespace, name, true, 'setAttributeNS', 'Element');
    host.setAttributeNS(elementSlot(this).nodeId, namespace, name, value);
  });
  const indexed = (key) =>
    typeof key === 'string' && /^(0|[1-9][0-9]*)$/.test(key) && Number(key) < 4294967295;
  const mapOwners = new WeakMap();
  function namedMap(element) {
    let map = maps.get(element);
    if (map) return map;
    map = new Proxy(Object.create(globalThis.NamedNodeMap.prototype), {
      get(target, key, receiver) {
        if (indexed(key)) {
          const name = names(element)[Number(key)];
          return name === undefined ? undefined : get(element, name);
        }
        if (Reflect.has(target, key)) return Reflect.get(target, key, receiver);
        return typeof key === 'string' ? get(element, key) || undefined : undefined;
      },
      has(target, key) {
        return indexed(key)
          ? Number(key) < names(element).length
          : Reflect.has(target, key) || (typeof key === 'string' && get(element, key) !== null);
      },
      ownKeys() {
        const list = names(element);
        return [...list.map((_, i) => String(i)), ...list.filter((name) => !indexed(name))];
      },
      getOwnPropertyDescriptor(target, key) {
        if (indexed(key) && Number(key) < names(element).length)
          return {
            value: get(element, names(element)[Number(key)]),
            writable: false,
            enumerable: true,
            configurable: true,
          };
        if (typeof key === 'string' && get(element, key))
          return {
            value: get(element, key),
            writable: false,
            enumerable: false,
            configurable: true,
          };
        return undefined;
      },
    });
    maps.set(element, map);
    mapOwners.set(map, element);
    return map;
  }
  if (!globalThis.Attr || !globalThis.NamedNodeMap) return { namedMap };
  const owner = (map) => {
    const element = mapOwners.get(map);
    if (!element) throw new TypeError('Illegal invocation');
    return element;
  };
  accessor(globalThis.NamedNodeMap.prototype, 'length', function () {
    return names(owner(this)).length;
  });
  member(globalThis.NamedNodeMap.prototype, 'item', function (index) {
    const element = owner(this),
      name = names(element)[Number(index) >>> 0];
    return name === undefined ? null : get(element, name);
  });
  member(globalThis.NamedNodeMap.prototype, 'getNamedItem', function (name) {
    return get(owner(this), name);
  });
  member(globalThis.NamedNodeMap.prototype, 'removeNamedItem', function (name) {
    const element = owner(this),
      attr = get(element, name);
    if (!attr) throw platformDOMException('Attribute not found', 'NotFoundError');
    return element.removeAttributeNode(attr);
  });
  member(globalThis.NamedNodeMap.prototype, 'setNamedItem', function (attr) {
    return owner(this).setAttributeNode(attr);
  });
  Object.defineProperty(globalThis.NamedNodeMap.prototype, Symbol.iterator, {
    value: function* () {
      const element = owner(this);
      for (let index = 0; index < names(element).length; index++)
        yield get(element, names(element)[index]);
    },
    writable: true,
    configurable: true,
  });
  for (const name of ['name', 'nodeName'])
    accessor(globalThis.Attr.prototype, name, function () {
      return state(this).name;
    });
  accessor(globalThis.Attr.prototype, 'namespaceURI', function () {
    return state(this).namespace;
  });
  accessor(globalThis.Attr.prototype, 'localName', function () {
    const s = state(this);
    return s.namespace ? s.name.split(':').at(-1) : s.name;
  });
  accessor(globalThis.Attr.prototype, 'prefix', function () {
    const s = state(this);
    return s.namespace && s.name.includes(':') ? s.name.split(':')[0] : null;
  });
  for (const name of ['parentNode', 'parentElement'])
    accessor(globalThis.Attr.prototype, name, function () {
      state(this);
      return null;
    });
  accessor(globalThis.Attr.prototype, 'nodeType', function () {
    state(this);
    return 2;
  });
  accessor(globalThis.Attr.prototype, 'specified', function () {
    state(this);
    return true;
  });
  accessor(globalThis.Attr.prototype, 'ownerElement', function () {
    const value = state(this);
    if (value.owner && !value.owner.hasAttribute(value.name)) {
      cache(value.owner).delete(value.name);
      detach(this);
    }
    return value.owner;
  });
  accessor(globalThis.Attr.prototype, 'ownerDocument', function () {
    const value = state(this);
    return value.owner ? value.owner.ownerDocument : value.document;
  });
  const read = function () {
    const value = state(this);
    return value.owner ? (value.owner.getAttribute(value.name) ?? value.value) : value.value;
  };
  const write = function (input) {
    const value = state(this),
      text = bindingString(input);
    if (value.owner) {
      const approved = trustedAttributeValue(
          value.owner,
          value.name,
          text,
          value.namespace,
          "Failed to set the 'value' property on 'Attr': ",
        ),
        kind = trustedAttributeInfo(
          value.owner.localName,
          value.name,
          value.owner.namespaceURI,
          value.namespace,
        )?.[0];
      value.owner.setAttribute(
        value.name,
        kind ? trustedValue(trustedConstructors[kind], approved) : approved,
      );
    } else value.value = text;
  };
  accessor(globalThis.Attr.prototype, 'value', read, write);
  for (const name of ['nodeValue', 'textContent'])
    accessor(globalThis.Attr.prototype, name, read, function (value) {
      write.call(this, value == null ? '' : value);
    });
  member(globalThis.Attr.prototype, 'cloneNode', function () {
    const value = state(this);
    return create(value.name, this.value, this.ownerDocument, null, value.namespace);
  });
  member(Document.prototype, 'createAttribute', function (name) {
    if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
    name = String(name);
    validateDOMName(name, true, 'createAttribute', 'Document');
    return create(name.toLowerCase(), '', this);
  });
  member(Element.prototype, 'getAttributeNodeNS', function (namespace, local) {
    const name = nsName(this, namespace, local);
    return name ? get(this, name) : null;
  });
  member(Element.prototype, 'getAttributeNode', function (name) {
    if (!(this instanceof Element)) throw new TypeError('Illegal invocation');
    return get(this, name);
  });
  const remove = Element.prototype.removeAttribute;
  member(Element.prototype, 'removeAttribute', function (name) {
    name = normalize(this, name);
    const attr = cache(this).get(name);
    if (attr) {
      detach(attr);
      cache(this).delete(name);
    }
    return remove.call(this, name);
  });
  member(Element.prototype, 'removeAttributeNode', function (attr) {
    if (!(this instanceof Element)) throw new TypeError('Illegal invocation');
    if (!slots.has(attr)) throw new TypeError('Parameter is not an Attr');
    if (attr.ownerElement !== this || get(this, attr.name) !== attr)
      throw platformDOMException('Attribute is not owned by this element', 'NotFoundError');
    this.removeAttribute(attr.name);
    return attr;
  });
  member(Element.prototype, 'setAttributeNode', function (attr) {
    if (!(this instanceof Element)) throw new TypeError('Illegal invocation');
    if (!slots.has(attr)) throw new TypeError('Parameter is not an Attr');
    const value = state(attr);
    if (value.owner && value.owner !== this)
      throw platformDOMException('Attribute is already in use', 'InUseAttributeError');
    const previous = get(this, value.name);
    if (previous === attr) return attr;
    const approved = trustedAttributeValue(
        this,
        value.namespace ? value.name.split(':').at(-1) : value.name,
        value.value,
        value.namespace,
        "Failed to execute 'setAttributeNode' on 'Element': ",
      ),
      kind = trustedAttributeInfo(
        this.localName,
        value.namespace ? value.name.split(':').at(-1) : value.name,
        this.namespaceURI,
        value.namespace,
      )?.[0];
    value.value = approved;
    if (previous) detach(previous);
    value.document = this.ownerDocument;
    value.owner = this;
    cache(this).set(value.name, attr);
    if (value.namespace)
      this.setAttributeNS(
        value.namespace,
        value.name,
        kind ? trustedValue(trustedConstructors[kind], approved) : approved,
      );
    else
      this.setAttribute(
        value.name,
        kind ? trustedValue(trustedConstructors[kind], approved) : approved,
      );
    return previous;
  });
  member(Element.prototype, 'setAttributeNodeNS', function (attr) {
    return this.setAttributeNode(attr);
  });
  // Attr inherits these bindings from Node. Select its private state here so
  // borrowing Node accessors and changing public prototypes preserve semantics.
  for (const name of [
    'nodeName',
    'nodeType',
    'nodeValue',
    'ownerDocument',
    'parentNode',
    'parentElement',
    'textContent',
  ]) {
    const attr = Object.getOwnPropertyDescriptor(globalThis.Attr.prototype, name);
    const node = Object.getOwnPropertyDescriptor(Node.prototype, name);
    accessor(
      Node.prototype,
      name,
      function () {
        if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
        return functionSourceApply(slots.has(this) ? attr.get : node.get, this, []);
      },
      node.set
        ? function (value) {
            if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
            return functionSourceApply(slots.has(this) ? attr.set : node.set, this, [value]);
          }
        : undefined,
    );
    delete globalThis.Attr.prototype[name];
  }
  const clone = Node.prototype.cloneNode;
  member(Node.prototype, 'cloneNode', function (deep = false) {
    if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
    if (!slots.has(this)) return functionSourceApply(clone, this, [deep]);
    const value = state(this);
    return create(
      value.name,
      functionSourceApply(read, this, []),
      value.owner ? value.owner.ownerDocument : value.document,
      null,
      value.namespace,
    );
  });
  delete globalThis.Attr.prototype.cloneNode;
  // Equality observes canonical DOM state, never author-overridable getters or
  // serialization. Template contents and shadow trees are separate node trees.
  const equalityState = (node) => {
    const attribute = slots.get(node);
    if (attribute) {
      const value = attribute.owner
        ? host.nodeData(elementSlot(attribute.owner).nodeId).attributes[attribute.name]
        : attribute.value;
      return {
        type: 'attribute',
        namespace: attribute.namespace || '',
        name: attribute.namespace ? attribute.name.split(':').at(-1) : attribute.name,
        value,
        children: [],
      };
    }
    const fragment = fragmentSlots.get(node);
    if (fragment) return { type: 'fragment', children: fragment.children };
    const id = node === document ? realmDocumentRootID : elementSlot(node).nodeId;
    const data = host.nodeData(id);
    return {
      type: data.type,
      namespace: data.namespaceURI || '',
      name: data.qualifiedName || data.tagName,
      value:
        data.type === 'text' || data.type === 'comment'
          ? decodeDOMStringJSON(host.textContentJSON(id))
          : undefined,
      attributes: data.attributes,
      namespaces: data.attributeNamespaces || {},
      children: data.children.map(wrap),
    };
  };
  const equalAttributes = (a, b) => {
    const left = Object.keys(a.attributes),
      right = Object.keys(b.attributes);
    if (left.length !== right.length) return false;
    return left.every((name) => {
      const namespace = a.namespaces[name] || '';
      const local = namespace ? name.split(':').at(-1) : name;
      return right.some(
        (other) =>
          (b.namespaces[other] || '') === namespace &&
          (namespace ? other.split(':').at(-1) : other) === local &&
          a.attributes[name] === b.attributes[other],
      );
    });
  };
  member(Node.prototype, 'isEqualNode', function (other) {
    if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
    if (!arguments.length) throw new TypeError('Argument required');
    if (other == null) return false;
    if (!isDOMNode(other)) throw new TypeError('Expected a Node');
    const pending = [[this, other]];
    while (pending.length) {
      const [left, right] = pending.pop();
      if (left === right) continue;
      const a = equalityState(left),
        b = equalityState(right);
      if (a.type !== b.type) return false;
      if (
        a.type === 'element' &&
        (a.name !== b.name || a.namespace !== b.namespace || !equalAttributes(a, b))
      )
        return false;
      if (
        a.type === 'attribute' &&
        (a.name !== b.name || a.namespace !== b.namespace || a.value !== b.value)
      )
        return false;
      if ((a.type === 'text' || a.type === 'comment') && a.value !== b.value) return false;
      if (
        a.type === 'doctype' &&
        (a.name !== b.name ||
          (a.attributes.public || '') !== (b.attributes.public || '') ||
          (a.attributes.system || '') !== (b.attributes.system || ''))
      )
        return false;
      if (a.children.length !== b.children.length) return false;
      for (let i = 0; i < a.children.length; i++) pending.push([a.children[i], b.children[i]]);
    }
    return true;
  });
  return { namedMap };
})();
