const compatibilityElementState = {};
// Realm-local semantic state. Canonical nodes remain owned by the DOM host.
{
  const expose = (name, value) =>
    Object.defineProperty(globalThis, name, { value, writable: true, configurable: true });
  const definitions = new Map(),
    constructors = new Map(),
    upgraded = new WeakMap(),
    construction = [];
  const waiting = new Map();
  let defining = false,
    upgradeRevision = 0;
  const report = (error) => console.error((error && error.stack) || String(error));
  let reactionDepth = 0;
  const reactions = [],
    elementReactions = new WeakMap();
  // An invocation owns its element queue. A nested CEReactions operation
  // drains the affected element's reactions, never its pending siblings.
  const flushReactions = () => {
    if (reactionDepth) return;
    const pending = reactions.splice(0);
    for (const node of pending) {
      const queue = elementReactions.get(node);
      while (queue?.length) {
        const [fn, args] = queue.shift();
        try {
          fn.apply(node, args);
        } catch (error) {
          report(error);
        }
      }
    }
  };
  const reaction = (node, name, args = []) => {
    const fn = upgraded.get(node)?.callbacks[name];
    if (typeof fn === 'function') {
      let queue = elementReactions.get(node);
      if (!queue) elementReactions.set(node, (queue = []));
      queue.push([fn, args]);
      reactions.push(node);
      flushReactions();
    }
  };
  const validName = (name) =>
    /^[a-z][.0-9_a-z\-]*-[.0-9_a-z\-]*$/.test(name) &&
    ![
      'annotation-xml',
      'color-profile',
      'font-face',
      'font-face-src',
      'font-face-uri',
      'font-face-format',
      'font-face-name',
      'missing-glyph',
    ].includes(name);
  const rawCreate = Document.prototype.createElement;
  constructCustomElement = (ctor) => {
    const definition = constructors.get(ctor);
    if (ctor === HTMLElement || !definition) throw new TypeError('Illegal constructor');
    const frame = construction[construction.length - 1];
    if (frame && frame.definition === definition) {
      if (frame.used) throw new TypeError('Custom element constructed twice');
      frame.used = true;
      return frame.node;
    }
    const node = rawCreate.call(document, definition.extendsTag || definition.name);
    if (definition.extendsTag) node.setAttribute('is', definition.name);
    Object.setPrototypeOf(node, ctor.prototype);
    upgraded.set(node, definition);
    upgradeRevision++;
    host.invalidateStyleObservations();
    return node;
  };
  const upgrade = (node) => {
    if (!(node instanceof Element) || upgraded.has(node)) return;
    const customizedName = node.getAttribute?.('is'),
      definition =
        (customizedName && definitions.get(customizedName)?.extendsTag === node.localName
          ? definitions.get(customizedName)
          : null) || definitions.get(node.localName);
    if (!definition) return;
    upgraded.set(node, null);
    Object.setPrototypeOf(node, definition.prototype);
    const frame = { node, definition, used: false },
      attributes = node
        .getAttributeNames()
        .filter((name) => definition.attributes.includes(name))
        .map((name) => [name, null, node.getAttribute(name), null]),
      connected = node.isConnected;
    construction.push(frame);
    reactionDepth++;
    try {
      if (Reflect.construct(definition.ctor, []) !== node)
        throw new TypeError('Custom element returned a different object');
      upgraded.set(node, definition);
      upgradeRevision++;
      host.invalidateStyleObservations();
      for (const args of attributes) reaction(node, 'attributeChangedCallback', args);
      if (connected) reaction(node, 'connectedCallback');
    } catch (error) {
      report(error);
    } finally {
      construction.pop();
      reactionDepth--;
      flushReactions();
    }
  };
  const walk = (node, callback) => {
    callback(node);
    const shadow = elementShadows.get(node);
    if (shadow) walk(shadow, callback);
    for (const child of canonicalDOMChildren(node)) walk(child, callback);
  };
  const definitionCandidates = (root, definition) => {
    const selector = definition.extendsTag
      ? definition.extendsTag + '[is="' + definition.name + '"]'
      : definition.name;
    if (!shadowHosts.size) return compatibilitySelectors.query(root, selector);
    const candidates = [];
    const collect = (tree) => {
      for (const node of compatibilitySelectors.query(tree, '*')) {
        if (
          definition.extendsTag
            ? node.localName === definition.extendsTag &&
              node.getAttribute('is') === definition.name
            : node.localName === definition.name
        )
          candidates.push(node);
        const shadow = elementShadows.get(node);
        if (shadow) collect(shadow);
      }
    };
    collect(root);
    return candidates;
  };
  class CustomElementRegistry {
    constructor() {
      if (arguments[0] !== hostToken) throw new TypeError('Illegal constructor');
    }
    define(name, ctor, options = {}) {
      if (arguments.length < 2) throw new TypeError('Two arguments required');
      name = String(name);
      if (typeof ctor !== 'function') throw new TypeError('Invalid constructor');
      try {
        Reflect.construct(
          new Proxy(ctor, {
            construct() {
              return {};
            },
          }),
          [],
        );
      } catch (error) {
        throw new TypeError('Invalid constructor');
      }
      if (!validName(name))
        throw platformDOMException('Invalid custom element name', 'SyntaxError');
      if (definitions.has(name) || constructors.has(ctor))
        throw platformDOMException('Already defined', 'NotSupportedError');
      const extendsTag =
        options.extends === undefined ? null : String(options.extends).toLowerCase();
      if (extendsTag && (validName(extendsTag) || !/^[a-z][0-9a-z-]*$/.test(extendsTag)))
        throw platformDOMException('Invalid built-in element name', 'NotSupportedError');
      if (defining) throw platformDOMException('Definition is running', 'NotSupportedError');
      let definition;
      defining = true;
      const sequence = (value) => {
        if (value === undefined) return [];
        if (value == null || typeof value[Symbol.iterator] !== 'function')
          throw new TypeError('Expected iterable');
        return Array.from(value, String);
      };
      try {
        const prototype = ctor.prototype;
        if (
          prototype === null ||
          (typeof prototype !== 'object' && typeof prototype !== 'function')
        )
          throw new TypeError('Invalid prototype');
        const callbacks = {};
        const readCallback = (key) => {
          const value = prototype[key];
          if (value !== undefined && typeof value !== 'function')
            throw new TypeError('Callback is not callable');
          callbacks[key] = value;
        };
        for (const key of [
          'connectedCallback',
          'disconnectedCallback',
          'connectedMoveCallback',
          'adoptedCallback',
          'attributeChangedCallback',
        ])
          readCallback(key);
        const attributes = callbacks.attributeChangedCallback
          ? sequence(ctor.observedAttributes)
          : [];
        const disabledFeatures = sequence(ctor.disabledFeatures),
          formAssociated = !!ctor.formAssociated;
        if (formAssociated)
          for (const key of [
            'formAssociatedCallback',
            'formResetCallback',
            'formDisabledCallback',
            'formStateRestoreCallback',
            'toolFillCallback',
          ])
            readCallback(key);
        definition = {
          name,
          ctor,
          prototype,
          callbacks,
          attributes,
          disabledFeatures,
          formAssociated,
          extendsTag,
        };
      } finally {
        defining = false;
      }
      definitions.set(name, definition);
      constructors.set(ctor, definition);
      // Collect the shadow-including upgrade candidates before constructors
      // run. Native queries avoid one FFI call for every child of every
      // existing node each time another custom element is defined.
      for (const node of definitionCandidates(document, definition)) upgrade(node);
      if (waiting.has(name)) {
        waiting.get(name).resolve(ctor);
        waiting.delete(name);
      }
    }
    get(name) {
      return definitions.get(String(name))?.ctor;
    }
    getName(ctor) {
      if (typeof ctor !== 'function') throw new TypeError('Expected a constructor');
      return constructors.get(ctor)?.name || null;
    }
    whenDefined(name) {
      name = String(name);
      if (!validName(name))
        return platformPromiseReject(
          platformDOMException('Invalid custom element name', 'SyntaxError'),
        );
      if (definitions.has(name)) return platformPromiseResolve(definitions.get(name).ctor);
      if (!waiting.has(name)) {
        let resolve;
        const promise = new platformPromise((r) => (resolve = r));
        waiting.set(name, { promise, resolve });
      }
      return waiting.get(name).promise;
    }
    upgrade(root) {
      walk(root, upgrade);
    }
  }
  Object.defineProperty(CustomElementRegistry.prototype, Symbol.toStringTag, {
    value: 'CustomElementRegistry',
    configurable: true,
  });
  expose('CustomElementRegistry', CustomElementRegistry);
  expose('customElements', new CustomElementRegistry(hostToken));
  Object.defineProperty(Document.prototype, 'createElement', {
    value: function (name, options) {
      const node = rawCreate.call(this, name, options);
      if (options?.is !== undefined) node.setAttribute('is', String(options.is));
      if (definitions.size && !customElementCloneInert) upgrade(node);
      return node;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });

  const observers = new Set(),
    observerSlots = new WeakMap(),
    pendingObservers = new Set();
  let deliveryQueued = false,
    mutationDepth = 0,
    observerSequence = 0,
    otherWorldObservers = false;
  const scheduleDelivery = () => {
    if (!deliveryQueued) {
      deliveryQueued = true;
      queueMicrotask(deliver);
    }
  };
  const deliver = () => {
    deliveryQueued = false;
    const active = Array.from(pendingObservers).sort(
      (a, b) => observerSlots.get(a).sequence - observerSlots.get(b).sequence,
    );
    pendingObservers.clear();
    for (const observer of active) {
      const s = observerSlots.get(observer),
        records = s.records.splice(0);
      s.transients = [];
      if (records.length)
        try {
          s.callback.call(observer, records, observer);
        } catch (error) {
          report(error);
        }
    }
  };
  const recordSlots = new WeakMap();
  class MutationRecord {
    constructor() {
      throw new TypeError('Illegal constructor');
    }
  }
  for (const name of [
    'type',
    'target',
    'addedNodes',
    'removedNodes',
    'previousSibling',
    'nextSibling',
    'attributeName',
    'attributeNamespace',
    'oldValue',
  ])
    Object.defineProperty(MutationRecord.prototype, name, {
      get() {
        const data = recordSlots.get(this);
        if (!data) throw new TypeError('Illegal invocation');
        return data[name];
      },
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(MutationRecord.prototype, Symbol.toStringTag, {
    value: 'MutationRecord',
    configurable: true,
  });
  expose('MutationRecord', MutationRecord);
  const observerState = (observer) => {
    const state = observerSlots.get(observer);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const observedRoots = (s) => [...s.targets, ...s.transients];
  const retainRemoved = (node, remote = false) => {
    if (otherWorldObservers && !remote)
      host.worldMutation({ kind: 'retain', target: elementSlot(node).nodeId });
    for (const observer of observers) {
      const s = observerSlots.get(observer),
        entries = [];
      for (const [root, options] of observedRoots(s))
        if (options.subtree && root !== node && root.contains(node)) entries.push([node, options]);
      if (entries.length) {
        s.transients.push(...entries);
        pendingObservers.add(observer);
        scheduleDelivery();
      }
    }
  };
  const queueRecord = (type, target, details = {}, remote = false) => {
    invalidateDOMCollections(target);
    if (mutationDepth) return;
    if (otherWorldObservers && !remote) {
      // Synthetic roots have no canonical DOM node ID. Their mutations remain
      // observable in the owning world, but cannot yet be projected into a
      // different realm as an ordinary node record.
      const id = (node) => elementSlot(node)?.nodeId || 0;
      host.worldMutation({
        kind: 'mutation',
        type,
        target: id(target),
        addedNodes: (details.addedNodes || []).map(id),
        removedNodes: (details.removedNodes || []).map(id),
        previousSibling: id(details.previousSibling),
        nextSibling: id(details.nextSibling),
        attributeName: details.attributeName ?? null,
        attributeNamespace: details.attributeNamespace ?? null,
        oldValue: details.oldValue ?? null,
      });
    }
    if (!observers.size) return;
    for (const observer of observers) {
      const s = observerSlots.get(observer);
      let match = false,
        old = false;
      for (const [root, options] of observedRoots(s)) {
        if (root !== target && (!options.subtree || !root.contains(target))) continue;
        if (!options[type]) continue;
        if (
          type === 'attributes' &&
          options.attributeFilter &&
          (details.attributeNamespace != null ||
            !options.attributeFilter.includes(details.attributeName))
        )
          continue;
        match = true;
        old =
          old || !!options[type === 'attributes' ? 'attributeOldValue' : 'characterDataOldValue'];
      }
      if (!match) continue;
      const record = Object.create(MutationRecord.prototype);
      const fields = {
        type,
        target,
        addedNodes: [],
        removedNodes: [],
        previousSibling: null,
        nextSibling: null,
        attributeName: null,
        attributeNamespace: null,
        oldValue: null,
        ...details,
      };
      fields.addedNodes = nodeList(fields.addedNodes.map((n) => elementSlot(n)));
      fields.removedNodes = nodeList(fields.removedNodes.map((n) => elementSlot(n)));
      if (!old) fields.oldValue = null;
      recordSlots.set(record, fields);
      s.records.push(record);
      pendingObservers.add(observer);
      scheduleDelivery();
    }
  };
  registerBootstrapCallback('registerWorldMutationReceiver', (kind, record) => {
    if (kind === 'enabled') {
      otherWorldObservers = record;
      return;
    }
    const resolve = (id) => (id ? wrap(host.nodeData(id)) : null),
      target = resolve(record.target);
    if (!target) return;
    if (record.kind === 'retain') {
      retainRemoved(target, true);
      return;
    }
    queueRecord(
      record.type,
      target,
      {
        addedNodes: record.addedNodes.map(resolve),
        removedNodes: record.removedNodes.map(resolve),
        previousSibling: resolve(record.previousSibling),
        nextSibling: resolve(record.nextSibling),
        attributeName: record.attributeName,
        attributeNamespace: record.attributeNamespace,
        oldValue: record.oldValue,
      },
      true,
    );
  });
  class MutationObserver {
    constructor(callback) {
      if (typeof callback !== 'function') throw new TypeError('Callback must be callable');
      observerSlots.set(this, {
        callback,
        sequence: observerSequence++,
        records: [],
        targets: new Map(),
        transients: [],
      });
    }
    observe(target, options) {
      const s = observerState(this);
      if (!isDOMNode(target)) throw new TypeError('Target must be a Node');
      const init = Object(options);
      options = {
        attributeFilter: init.attributeFilter,
        attributeOldValue: init.attributeOldValue,
        attributes: init.attributes,
        characterData: init.characterData,
        characterDataOldValue: init.characterDataOldValue,
        childList: init.childList,
        subtree: init.subtree,
      };
      if (
        options.attributes === undefined &&
        (options.attributeOldValue !== undefined || options.attributeFilter !== undefined)
      )
        options.attributes = true;
      if (options.characterData === undefined && options.characterDataOldValue !== undefined)
        options.characterData = true;
      if (
        (!options.childList && !options.attributes && !options.characterData) ||
        (!options.attributes && (options.attributeOldValue || options.attributeFilter)) ||
        (!options.characterData && options.characterDataOldValue)
      )
        throw new TypeError('Invalid observer options');
      if (options.attributeFilter !== undefined) {
        if (
          options.attributeFilter == null ||
          typeof options.attributeFilter[Symbol.iterator] !== 'function'
        )
          throw new TypeError('attributeFilter must be iterable');
        options.attributeFilter = Array.from(options.attributeFilter, String);
      }
      if (s.targets.has(target))
        s.transients = s.transients.filter(
          ([, registered]) => registered !== s.targets.get(target),
        );
      s.targets.set(target, options);
      const first = !observers.size;
      observers.add(this);
      if (first) host.worldObserverPresence(true);
    }
    disconnect() {
      const s = observerState(this);
      s.targets.clear();
      s.records.length = 0;
      s.transients = [];
      const removed = observers.delete(this);
      pendingObservers.delete(this);
      if (removed && !observers.size) host.worldObserverPresence(false);
    }
    takeRecords() {
      return observerState(this).records.splice(0);
    }
  }
  // Web IDL operations are enumerable; constructor wrappers discover them
  // by enumerating an instance before forwarding to its branded observer.
  for (const name of ['observe', 'disconnect', 'takeRecords'])
    Object.defineProperty(MutationObserver.prototype, name, { enumerable: true });
  Object.defineProperty(MutationObserver.prototype, Symbol.toStringTag, {
    value: 'MutationObserver',
    configurable: true,
  });
  expose('MutationObserver', MutationObserver);
  // The generated alias predates this semantic constructor replacement.
  // Both Window names must retain the same constructor and observer state.
  expose('WebKitMutationObserver', MutationObserver);
  // CharacterData and traversal share the canonical host node; cached wrapper
  // records only describe identity, and must never serve mutable text.
  const member = (proto, name, value) =>
    Object.defineProperty(proto, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  const accessor = (proto, name, get, set) =>
    Object.defineProperty(proto, name, { get, set, enumerable: true, configurable: true });
  for (const [name, value] of Object.entries({
    ELEMENT_NODE: 1,
    ATTRIBUTE_NODE: 2,
    TEXT_NODE: 3,
    CDATA_SECTION_NODE: 4,
    ENTITY_REFERENCE_NODE: 5,
    ENTITY_NODE: 6,
    PROCESSING_INSTRUCTION_NODE: 7,
    COMMENT_NODE: 8,
    DOCUMENT_NODE: 9,
    DOCUMENT_TYPE_NODE: 10,
    DOCUMENT_FRAGMENT_NODE: 11,
    NOTATION_NODE: 12,
    DOCUMENT_POSITION_DISCONNECTED: 1,
    DOCUMENT_POSITION_PRECEDING: 2,
    DOCUMENT_POSITION_FOLLOWING: 4,
    DOCUMENT_POSITION_CONTAINS: 8,
    DOCUMENT_POSITION_CONTAINED_BY: 16,
    DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC: 32,
  }))
    for (const target of [Node, Node.prototype])
      Object.defineProperty(target, name, { value, enumerable: true });
  let documentRootID = host.documentRootID();
  const canonicalChildLists = new WeakMap();
  const childLists = new WeakMap();
  bootstrapRestoreHooks.push(() => {
    documentRootID = host.documentRootID();
  });
  const childCount = (node) => {
    const slot = elementSlot(node);
    if (node === document || slot) return canonicalChildren(node).length;
    return fragmentSlots.get(node)?.children.length || 0;
  };
  const childAt = (node, index) => {
    const slot = elementSlot(node);
    if (node === document || slot) return canonicalChildren(node)[index] || null;
    return fragmentSlots.get(node)?.children[index] || null;
  };
  const canonicalChildren = (node) => {
    // Mutations from another Window do not advance this realm's JS revision.
    // The shared DOM arena is the authority for live child collections.
    const revision = host.domRevision(),
      previous = canonicalChildLists.get(node);
    if (previous?.revision === revision) return previous.values;
    const slot = elementSlot(node),
      values = host.nodeChildren(node === document ? documentRootID : slot.nodeId).map(wrap);
    canonicalChildLists.set(node, { revision, values });
    return values;
  };
  accessor(Node.prototype, 'childNodes', function () {
    let list = childLists.get(this);
    if (!list) {
      const node = this;
      list = nodeListView(
        () => childCount(node),
        (index) => childAt(node, index),
      );
      childLists.set(this, list);
    }
    return list;
  });
  accessor(Document.prototype, 'firstChild', function () {
    return wrap(host.firstChild(documentRootID));
  });
  const characterText = (node) =>
    decodeDOMStringJSON(host.textContentJSON(elementSlot(node).nodeId));
  const setCharacterText = (node, value) => {
    const old = observers.size ? characterText(node) : null;
    value = String(value);
    host.setCharacterDataJSON(elementSlot(node).nodeId, domStringJSONEncode(value));
    queueRecord('characterData', node, { oldValue: old });
  };
  accessor(
    CharacterData.prototype,
    'data',
    function () {
      return characterText(this);
    },
    function (value) {
      setCharacterText(this, value === null ? '' : value);
    },
  );
  accessor(
    CharacterData.prototype,
    'textContent',
    function () {
      return characterText(this);
    },
    function (value) {
      setCharacterText(this, value == null ? '' : value);
    },
  );
  accessor(CharacterData.prototype, 'length', function () {
    return this.data.length;
  });
  const unsigned = (value) => Number(value) >>> 0;
  member(CharacterData.prototype, 'substringData', function (offset, count) {
    if (arguments.length < 2) throw new TypeError('Two arguments required');
    const text = this.data;
    offset = unsigned(offset);
    count = unsigned(count);
    if (offset > text.length)
      throw platformDOMException('Offset exceeds data length', 'IndexSizeError');
    return text.slice(offset, offset + count);
  });
  member(CharacterData.prototype, 'replaceData', function (offset, count, data) {
    if (arguments.length < 3) throw new TypeError('Three arguments required');
    const text = this.data;
    offset = unsigned(offset);
    count = unsigned(count);
    data = String(data);
    if (offset > text.length)
      throw platformDOMException('Offset exceeds data length', 'IndexSizeError');
    setCharacterText(this, text.slice(0, offset) + data + text.slice(offset + count));
  });
  member(CharacterData.prototype, 'appendData', function (data) {
    if (!arguments.length) throw new TypeError('Argument required');
    this.replaceData(this.length, 0, String(data));
  });
  member(CharacterData.prototype, 'insertData', function (offset, data) {
    if (arguments.length < 2) throw new TypeError('Two arguments required');
    this.replaceData(offset, 0, String(data));
  });
  member(CharacterData.prototype, 'deleteData', function (offset, count) {
    if (arguments.length < 2) throw new TypeError('Two arguments required');
    this.replaceData(offset, count, '');
  });
  const sibling = (node, offset) => {
    const parent = syntheticParents.get(node);
    if (parent) {
      const children = fragmentSlots.get(parent).children;
      return children[children.indexOf(node) + offset] || null;
    }
    const slot = elementSlot(node);
    return slot ? wrap(host.sibling(slot.nodeId, offset)) : null;
  };
  for (const proto of [Node.prototype, Element.prototype]) {
    accessor(proto, 'nextSibling', function () {
      return sibling(this, 1);
    });
    accessor(proto, 'previousSibling', function () {
      return sibling(this, -1);
    });
  }
  accessor(Node.prototype, 'lastChild', function () {
    const nodes = this.childNodes;
    return nodes[nodes.length - 1] || null;
  });
  for (const proto of [Element.prototype, CharacterData.prototype])
    for (const [name, offset] of [
      ['nextElementSibling', 1],
      ['previousElementSibling', -1],
    ])
      accessor(proto, name, function () {
        let node = this;
        do {
          node = sibling(node, offset);
        } while (node && node.nodeType !== 1);
        return node;
      });
  accessor(Element.prototype, 'lastElementChild', function () {
    const nodes = this.children;
    return nodes[nodes.length - 1] || null;
  });
  member(Text.prototype, 'splitText', function (offset) {
    if (!arguments.length) throw new TypeError('Argument required');
    offset = unsigned(offset);
    const text = this.data;
    if (offset > text.length)
      throw platformDOMException('Offset exceeds data length', 'IndexSizeError');
    const node = document.createTextNode(text.slice(offset)),
      parent = this.parentNode;
    if (parent) parent.insertBefore(node, this.nextSibling);
    this.replaceData(offset, text.length - offset, '');
    return node;
  });
  accessor(Text.prototype, 'wholeText', function () {
    let node = this;
    while (node.previousSibling?.nodeType === 3) node = node.previousSibling;
    let text = '';
    for (; node?.nodeType === 3; node = node.nextSibling) text += node.data;
    return text;
  });
  member(Node.prototype, 'normalize', function () {
    for (let node = this.firstChild; node; ) {
      if (node.nodeType === 3) {
        if (node.length === 0) {
          const next = node.nextSibling;
          this.removeChild(node);
          node = next;
          continue;
        }
        while (node.nextSibling?.nodeType === 3) {
          const next = node.nextSibling;
          node.appendData(next.data);
          this.removeChild(next);
        }
      } else node.normalize();
      node = node.nextSibling;
    }
  });
  const sameDOMNode = (left, right) => {
    if (left === right) return true;
    const a = elementSlot(left),
      b = elementSlot(right);
    return !!a && !!b && a.nodeId === b.nodeId;
  };
  member(Node.prototype, 'replaceChild', function (node, child) {
    if (!isDOMNode(node) || !isDOMNode(child)) throw new TypeError('Expected Nodes');
    if (!sameDOMNode(canonicalDOMParent(child), this))
      throw platformDOMException('Not a child', 'NotFoundError');
    if (node === child) return child;
    this.insertBefore(node, child);
    this.removeChild(child);
    return child;
  });
  const removeChildBase = Node.prototype.removeChild;
  member(Node.prototype, 'removeChild', function (node) {
    if (!isDOMNode(node)) throw new TypeError('Expected a Node');
    if (!sameDOMNode(canonicalDOMParent(node), this))
      throw platformDOMException('Not a child', 'NotFoundError');
    return removeChildBase.call(this, node);
  });
  for (const proto of [Element.prototype, CharacterData.prototype]) {
    member(proto, 'remove', function () {
      if (this.parentNode) this.parentNode.removeChild(this);
    });
    member(proto, 'before', function (...nodes) {
      const parent = this.parentNode;
      if (!parent) return;
      for (const node of nodes)
        parent.insertBefore(isDOMNode(node) ? node : document.createTextNode(String(node)), this);
    });
    member(proto, 'after', function (...nodes) {
      const parent = this.parentNode;
      if (!parent) return;
      const next = this.nextSibling;
      for (const node of nodes)
        parent.insertBefore(isDOMNode(node) ? node : document.createTextNode(String(node)), next);
    });
    member(proto, 'replaceWith', function (...nodes) {
      const parent = this.parentNode;
      if (!parent) return;
      this.before(...nodes);
      if (this.parentNode === parent) parent.removeChild(this);
    });
  }
  for (const proto of [Element.prototype, DocumentFragment.prototype]) {
    member(proto, 'append', function (...nodes) {
      for (const node of nodes)
        this.appendChild(isDOMNode(node) ? node : document.createTextNode(String(node)));
    });
    member(proto, 'prepend', function (...nodes) {
      const before = this.firstChild;
      for (const node of nodes)
        this.insertBefore(isDOMNode(node) ? node : document.createTextNode(String(node)), before);
    });
    member(proto, 'replaceChildren', function (...nodes) {
      const fragment = document.createDocumentFragment();
      fragment.append(...nodes);
      while (this.firstChild) this.removeChild(this.firstChild);
      this.appendChild(fragment);
    });
  }
  const attributeChanged = (node, name, oldValue) => {
    compatibilityElementState.dialogAttributeChanged?.(node, name, oldValue);
    queueRecord('attributes', node, { attributeName: name, oldValue });
    const definition = upgraded.get(node);
    if (definition && definition.attributes.includes(name))
      reaction(node, 'attributeChangedCallback', [
        name,
        oldValue,
        intrinsicGetAttribute.call(node, name),
        null,
      ]);
  };
  compatibilityElementState.inlineStyleChanged = (node, oldValue) => {
    if (observers.size || definitions.size) attributeChanged(node, 'style', oldValue);
  };
  for (const name of ['setAttribute', 'removeAttribute']) {
    const original = Element.prototype[name];
    Object.defineProperty(Element.prototype, name, {
      value: function (key, value) {
        if (
          !observers.size &&
          !otherWorldObservers &&
          !definitions.size &&
          !compatibilityElementState.hasModal?.()
        )
          return original.apply(this, arguments);
        key =
          this.namespaceURI === 'http://www.w3.org/1999/xhtml'
            ? String(key).toLowerCase()
            : String(key);
        const old = intrinsicGetAttribute.call(this, key);
        const result = original.apply(this, arguments);
        if (name === 'setAttribute' || old !== null) attributeChanged(this, key, old);
        return result;
      },
      writable: true,
      configurable: true,
      enumerable: true,
    });
  }
  // Internal token/reflection writes retain observation and CE reactions while
  // bypassing public attribute methods that frameworks may override.
  intrinsicSetAttribute = Element.prototype.setAttribute;
  // Mutation algorithms own one record transaction. Recursive fragment
  // insertion and replaceChild's remove/insert steps must not emit duplicates.
  const mutationOriginals = Object.fromEntries(
    ['appendChild', 'insertBefore', 'removeChild', 'replaceChild'].map((name) => [
      name,
      Node.prototype[name],
    ]),
  );
  const nodeSnapshot = (node) => ({
    node,
    parent: canonicalDOMParent(node),
    previousSibling: sibling(node, -1),
    nextSibling: sibling(node, 1),
    connected: isNodeConnected(node),
  });
  const detachedReaction = (entry) => {
    if (entry.connected)
      walk(entry.node, (n) => {
        compatibilityElementState.detached?.(n);
        if (definitions.size && upgraded.get(n)) reaction(n, 'disconnectedCallback');
      });
  };
  const insertedReaction = (node) => {
    if (definitions.size && !customElementCloneInert && !templateTreeIsInert(node))
      walk(node, (n) => {
        if (upgraded.get(n)) {
          if (isNodeConnected(n)) reaction(n, 'connectedCallback');
        } else upgrade(n);
      });
  };
  const emitRemoval = (entry) => {
    if (entry.parent)
      queueRecord('childList', entry.parent, {
        removedNodes: [entry.node],
        previousSibling: entry.previousSibling,
        nextSibling: entry.nextSibling,
      });
  };
  for (const method of ['appendChild', 'insertBefore', 'removeChild', 'replaceChild']) {
    const original = mutationOriginals[method];
    member(Node.prototype, method, function (node, reference) {
      if (
        mutationDepth ||
        (!observers.size &&
          !otherWorldObservers &&
          !definitions.size &&
          !compatibilityElementState.hasModal?.())
      )
        return original.apply(this, arguments);
      if (!isDOMNode(node)) return original.apply(this, arguments);
      // Validate before creating transient observer registrations or running
      // any of replaceChild's removal steps. The ordinary insertion path
      // can combine these checks with its canonical mutation in Go.
      if (method === 'removeChild') {
        if (!sameDOMNode(canonicalDOMParent(node), this))
          throw platformDOMException('Not a child', 'NotFoundError');
      } else prepareInsertion(this, node, method === 'appendChild' ? null : reference, true);
      const fragment = node instanceof DocumentFragment,
        children = fragment ? Array.from(node.childNodes) : [node],
        entries = children.map(nodeSnapshot);
      const removed =
        method === 'removeChild' ? node : method === 'replaceChild' ? reference : null;
      const removedEntry = isDOMNode(removed) ? nodeSnapshot(removed) : null;
      // Snapshot transient registrations while the old ancestor chain exists.
      for (const entry of entries) if (entry.parent) retainRemoved(entry.node);
      if (removedEntry) retainRemoved(removed);
      let result;
      mutationDepth++;
      reactionDepth++;
      try {
        if (method === 'replaceChild' && node === reference && canonicalDOMParent(node) === this) {
          const next = node.nextSibling;
          mutationOriginals.removeChild.call(this, node);
          mutationOriginals.insertBefore.call(this, node, next);
          result = node;
        } else result = original.apply(this, arguments);
      } catch (error) {
        reactionDepth--;
        throw error;
      } finally {
        mutationDepth--;
      }
      try {
        if (method === 'removeChild') {
          emitRemoval(removedEntry);
          detachedReaction(removedEntry);
        } else {
          if (fragment) {
            if (children.length) queueRecord('childList', node, { removedNodes: children });
          } else for (const entry of entries) emitRemoval(entry);
          if (method === 'replaceChild') {
            const previous = children.length
              ? children[0].previousSibling
              : removedEntry.previousSibling;
            const next = children.length
              ? children[children.length - 1].nextSibling
              : removedEntry.nextSibling;
            const removedNodes = node === reference ? [] : [reference];
            if (children.length || removedNodes.length)
              queueRecord('childList', this, {
                addedNodes: children,
                removedNodes,
                previousSibling: previous,
                nextSibling: next,
              });
          } else if (children.length)
            queueRecord('childList', this, {
              addedNodes: children,
              previousSibling: children[0].previousSibling,
              nextSibling: children[children.length - 1].nextSibling,
            });
          for (const entry of entries) if (entry.parent) detachedReaction(entry);
          if (removedEntry && removed !== node) detachedReaction(removedEntry);
          for (const child of children) insertedReaction(child);
        }
      } finally {
        reactionDepth--;
        flushReactions();
      }
      return result;
    });
  }
  for (const [prototype, key] of [
    [Element.prototype, 'innerHTML'],
    [Element.prototype, 'textContent'],
    [Node.prototype, 'textContent'],
  ]) {
    const original = Object.getOwnPropertyDescriptor(prototype, key);
    if (!original?.set) continue;
    Object.defineProperty(prototype, key, {
      ...original,
      set(value) {
        if (
          mutationDepth ||
          (!observers.size &&
            !otherWorldObservers &&
            !definitions.size &&
            !compatibilityElementState.hasModal?.())
        )
          return original.set.call(this, value);
        const character = this.nodeType === 3 || this.nodeType === 8,
          old = character ? this.textContent : null;
        const removed = character ? [] : Array.from(this.childNodes),
          entries = removed.map(nodeSnapshot);
        for (const node of removed) retainRemoved(node);
        mutationDepth++;
        reactionDepth++;
        try {
          original.set.call(this, value);
        } catch (error) {
          reactionDepth--;
          throw error;
        } finally {
          mutationDepth--;
        }
        try {
          if (character) queueRecord('characterData', this, { oldValue: old });
          else {
            const added = Array.from(this.childNodes);
            if (added.length || removed.length)
              queueRecord('childList', this, { addedNodes: added, removedNodes: removed });
            for (const entry of entries) detachedReaction(entry);
            for (const node of added) insertedReaction(node);
          }
        } finally {
          reactionDepth--;
          flushReactions();
        }
      },
    });
  }
  // Markup insertion shares the child-list transaction and CE reaction queue
  // with ordinary node mutations; the host parser remains the tree authority.
  const markupMutation = (parent, removed, invoke) => {
    if (
      !parent ||
      mutationDepth ||
      (!observers.size &&
        !otherWorldObservers &&
        !definitions.size &&
        !compatibilityElementState.hasModal?.())
    )
      return invoke();
    const before = Array.from(parent.childNodes),
      prior = new Set(before),
      entry = removed ? nodeSnapshot(removed) : null;
    if (removed) retainRemoved(removed);
    mutationDepth++;
    reactionDepth++;
    try {
      invoke();
    } catch (error) {
      reactionDepth--;
      throw error;
    } finally {
      mutationDepth--;
    }
    try {
      const added = Array.from(parent.childNodes).filter((node) => !prior.has(node));
      const gone = removed && removed.parentNode !== parent ? [removed] : [];
      if (added.length || gone.length)
        queueRecord('childList', parent, {
          addedNodes: added,
          removedNodes: gone,
          previousSibling: added.length ? added[0].previousSibling : entry?.previousSibling || null,
          nextSibling: added.length
            ? added[added.length - 1].nextSibling
            : entry?.nextSibling || null,
        });
      for (const node of added) insertedReaction(node);
      if (gone.length) detachedReaction(entry);
    } finally {
      reactionDepth--;
      flushReactions();
    }
  };
  const adjacentHTML = Element.prototype.insertAdjacentHTML;
  member(Element.prototype, 'insertAdjacentHTML', function (position, text) {
    if (arguments.length < 2) return adjacentHTML.apply(this, arguments);
    position = bindingString(position).toLowerCase();
    const parent = position === 'beforebegin' || position === 'afterend' ? this.parentNode : this;
    return markupMutation(parent, null, () => adjacentHTML.call(this, position, text));
  });
  member(Element.prototype, 'insertAdjacentElement', function (position, element) {
    if (arguments.length < 2) throw new TypeError('2 arguments required');
    position = bindingString(position).toLowerCase();
    if (!isDOMNode(element) || element.nodeType !== 1) throw new TypeError('Expected an Element');
    switch (position) {
      case 'beforebegin': {
        const parent = this.parentNode;
        if (!parent) return null;
        parent.insertBefore(element, this);
        return element;
      }
      case 'afterbegin':
        this.insertBefore(element, this.firstChild);
        return element;
      case 'beforeend':
        this.appendChild(element);
        return element;
      case 'afterend': {
        const parent = this.parentNode;
        if (!parent) return null;
        parent.insertBefore(element, this.nextSibling);
        return element;
      }
      default:
        throw platformDOMException('Invalid position', 'SyntaxError');
    }
  });
  const outerHTML = Object.getOwnPropertyDescriptor(Element.prototype, 'outerHTML');
  Object.defineProperty(Element.prototype, 'outerHTML', {
    ...outerHTML,
    set(value) {
      return markupMutation(this.parentNode, this, () => outerHTML.set.call(this, value));
    },
  });
  const convertMutationNodes = (values) => {
    const nodes = values.map((value) =>
      isDOMNode(value) ? value : document.createTextNode(String(value)),
    );
    if (nodes.length === 1) return nodes[0];
    const fragment = document.createDocumentFragment();
    for (const node of nodes) fragment.appendChild(node);
    return fragment;
  };
  for (const prototype of [Element.prototype, DocumentFragment.prototype]) {
    member(prototype, 'append', function (...values) {
      if (values.length) this.appendChild(convertMutationNodes(values));
    });
    member(prototype, 'prepend', function (...values) {
      if (values.length) {
        const node = convertMutationNodes(values);
        this.insertBefore(node, this.firstChild);
      }
    });
    member(prototype, 'replaceChildren', function (...values) {
      const node = values.length ? convertMutationNodes(values) : null;
      if (node) prepareInsertion(this, node, null, true);
      const removed = Array.from(this.childNodes),
        entries = removed.map(nodeSnapshot),
        added = node
          ? node instanceof DocumentFragment
            ? Array.from(node.childNodes)
            : [node]
          : [];
      const sources = added.map(nodeSnapshot);
      for (const child of removed) retainRemoved(child);
      for (const child of added) if (child.parentNode) retainRemoved(child);
      mutationDepth++;
      reactionDepth++;
      try {
        while (this.firstChild) mutationOriginals.removeChild.call(this, this.firstChild);
        if (node) mutationOriginals.appendChild.call(this, node);
      } catch (error) {
        reactionDepth--;
        throw error;
      } finally {
        mutationDepth--;
      }
      try {
        if (node instanceof DocumentFragment) {
          if (added.length) queueRecord('childList', node, { removedNodes: added });
        } else
          for (const source of sources)
            if (source.parent && source.parent !== this) emitRemoval(source);
        if (removed.length || added.length)
          queueRecord('childList', this, { addedNodes: added, removedNodes: removed });
        for (const entry of entries) detachedReaction(entry);
        for (const source of sources) if (source.parent !== this) detachedReaction(source);
        for (const child of added) insertedReaction(child);
      } finally {
        reactionDepth--;
        flushReactions();
      }
    });
  }
  // The null-namespace aliases share the same canonical attribute path.
  const namespaceGet = Element.prototype.getAttributeNS,
    namespaceHas = Element.prototype.hasAttributeNS;
  member(Element.prototype, 'getAttributeNS', function (namespace, name) {
    return namespace == null || namespace === ''
      ? this.getAttribute(name)
      : namespaceGet.call(this, namespace, name);
  });
  member(Element.prototype, 'hasAttributeNS', function (namespace, name) {
    return namespace == null || namespace === ''
      ? this.hasAttribute(name)
      : namespaceHas.call(this, namespace, name);
  });
  const namespaceSet = Element.prototype.setAttributeNS,
    namespaceRemove = Element.prototype.removeAttributeNS;
  member(Element.prototype, 'setAttributeNS', function (namespace, name, value) {
    namespace = namespace == null ? '' : String(namespace);
    name = String(name);
    const local = namespace ? name.split(':').at(-1) : name,
      old = this.getAttributeNS(namespace, local);
    const result = namespaceSet.call(this, namespace, name, value);
    queueRecord('attributes', this, {
      attributeName: local,
      attributeNamespace: namespace || null,
      oldValue: old,
    });
    const definition = upgraded.get(this);
    if (definition && definition.attributes.includes(local))
      reaction(this, 'attributeChangedCallback', [
        local,
        old,
        this.getAttributeNS(namespace, local),
        namespace || null,
      ]);
    return result;
  });
  member(Element.prototype, 'removeAttributeNS', function (namespace, name) {
    return namespace == null || namespace === ''
      ? this.removeAttribute(name)
      : namespaceRemove.call(this, namespace, name);
  });
  const checkedTokens = (tokens) =>
    tokens.map((value) => {
      const token = String(value);
      if (!token) throw platformDOMException('Empty token', 'SyntaxError');
      if (/[\t\n\f\r ]/.test(token))
        throw platformDOMException('Whitespace in token', 'InvalidCharacterError');
      return token;
    });
  member(DOMTokenList.prototype, 'add', function (...values) {
    const tokens = checkedTokens(values);
    writeDOMTokens(this, domTokens(this).concat(tokens));
  });
  member(DOMTokenList.prototype, 'remove', function (...values) {
    const tokens = new Set(checkedTokens(values));
    writeDOMTokens(
      this,
      domTokens(this).filter((value) => !tokens.has(value)),
    );
  });
  member(DOMTokenList.prototype, 'toggle', function (value, force) {
    const [token] = checkedTokens([value]);
    if (
      !observers.size &&
      !otherWorldObservers &&
      !definitions.size &&
      !compatibilityElementState.hasModal?.()
    ) {
      const state = domTokenState(this);
      return host.toggleToken(
        elementSlot(state.element).nodeId,
        state.attribute,
        token,
        force === undefined ? -1 : Boolean(force) ? 1 : 0,
      );
    }
    const tokens = domTokens(this),
      has = tokens.includes(token);
    if (has) {
      if (force === undefined || !Boolean(force)) {
        writeDOMTokens(
          this,
          tokens.filter((value) => value !== token),
        );
        return false;
      }
      return true;
    }
    if (force !== undefined && !Boolean(force)) return false;
    writeDOMTokens(this, [...tokens, token]);
    return true;
  });

  /* shared_abort_encoding */
  Object.defineProperty(Element.prototype, 'matches', {
    value: function (selector) {
      return host.matches(elementSlot(this).nodeId, String(selector));
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Element.prototype, 'webkitMatchesSelector', {
    value: Element.prototype.matches,
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Document.prototype, 'defaultView', {
    get() {
      return window;
    },
    configurable: true,
    enumerable: true,
  });
  let focused = null,
    keyboardFocus = true;
  compatibilityElementState.isDefined = (node) => {
    const slot = elementSlot(node);
    return (
      slot?.namespaceURI !== 'http://www.w3.org/1999/xhtml' ||
      !validName(node.localName) ||
      !!upgraded.get(node)
    );
  };
  compatibilityElementState.focused = () => (focused && focused.isConnected ? focused : null);
  const modalDialogs = new Set(),
    dialogReturnValues = new WeakMap();
  // Non-attribute selector state also participates in observation invalidation.
  compatibilityElementState.observationVersion = () =>
    [elementSlot(focused)?.nodeId || 0, keyboardFocus, upgradeRevision, modalDialogs.size].join(
      ':',
    );
  compatibilityElementState.hasModal = () => modalDialogs.size !== 0;
  compatibilityElementState.modal = (node) => modalDialogs.has(node) && node.isConnected;
  compatibilityElementState.modalNodes = () =>
    Array.from(modalDialogs)
      .filter((node) => node.isConnected)
      .map((node) => elementSlot(node).nodeId);
  compatibilityElementState.detached = (node) => modalDialogs.delete(node);
  if (globalThis.HTMLDetailsElement) {
    const node = (value) => {
      const data = elementSlot(value);
      if (data?.tagName !== 'DETAILS') throw new TypeError('Illegal invocation');
      return data.nodeId;
    };
    Object.defineProperty(HTMLDetailsElement.prototype, 'open', {
      get() {
        return host.getAttribute(node(this), 'open') !== null;
      },
      set(value) {
        const id = node(this),
          old = host.getAttribute(id, 'open');
        if (value) host.setAttribute(id, 'open', '');
        else host.removeAttribute(id, 'open');
        if (value || old !== null) attributeChanged(this, 'open', old);
      },
      enumerable: true,
      configurable: true,
    });
  }
  /* dialog_lifecycle */
  compatibilityElementState.focusVisible = (node) =>
    compatibilityElementState.focused() === node &&
    (keyboardFocus ||
      node.localName === 'textarea' ||
      (node.localName === 'input' &&
        ![
          'button',
          'checkbox',
          'color',
          'file',
          'hidden',
          'image',
          'radio',
          'range',
          'reset',
          'submit',
        ].includes(String(node.type))));
  compatibilityElementState.noteTrustedInput = (type) => {
    const next = type === 'keydown';
    if (next !== keyboardFocus) {
      keyboardFocus = next;
      host.invalidateStyleObservations();
    }
  };
  document.addEventListener(
    'keydown',
    (event) => {
      if (event.isTrusted && !event.altKey && !event.ctrlKey && !event.metaKey)
        compatibilityElementState.noteTrustedInput('keydown');
    },
    true,
  );
  for (const type of ['mousedown', 'pointerdown', 'touchstart'])
    document.addEventListener(
      type,
      (event) => {
        if (event.isTrusted) compatibilityElementState.noteTrustedInput(type);
      },
      true,
    );
  Object.defineProperty(Document.prototype, 'activeElement', {
    get() {
      const child = this === document && host.focusedChildElement();
      if (child) return wrap(child);
      return focused && focused.isConnected ? focused : this.body || this.documentElement;
    },
    configurable: true,
    enumerable: true,
  });
  const hasFocus = function hasFocus() {
    const data = elementSlot(this);
    if (this !== document && data?.type !== 'document') throw new TypeError('Illegal invocation');
    return host.documentHasFocus(this === document ? realmDocumentRootID : data.nodeId);
  };
  markNative(hasFocus, 'hasFocus');
  Object.defineProperty(Document.prototype, 'hasFocus', {
    value: hasFocus,
    writable: true,
    enumerable: true,
    configurable: true,
  });
  const focusEvent = (target, type, related, bubbles = false) => {
    host.invalidateStyleObservations();
    return compatibilityElementState.dispatchFocus
      ? compatibilityElementState.dispatchFocus(target, type, related, bubbles)
      : dispatchTrusted(target, new Event(type, { bubbles, composed: true }));
  };
  Object.defineProperty(HTMLElement.prototype, 'focus', {
    value: function () {
      if (!this.isConnected || focused === this) return;
      // Focusing across same-origin frame boundaries first unfocuses the
      // deepest element in the previous branch. Cross-document relatedTarget
      // is intentionally null, matching Chrome's retargeting boundary.
      let active = document.activeElement;
      while (active?.contentDocument) {
        const nested = active.contentDocument.activeElement;
        if (
          !nested ||
          nested === active.contentDocument.body ||
          nested === active.contentDocument.documentElement
        )
          break;
        active = nested;
      }
      if (
        active &&
        active !== this &&
        active !== document.body &&
        active !== document.documentElement
      )
        active.blur();
      for (let owner = globalThis; owner.parent && owner.parent !== owner; owner = owner.parent) {
        const ancestor = owner.parent.document?.activeElement;
        if (ancestor && ancestor !== owner.frameElement && ancestor !== owner.parent.document.body)
          ancestor.blur();
      }
      host.focusDocument(elementSlot(this).nodeId);
      const previous = focused;
      focused = null;
      if (previous) {
        focusEvent(previous, 'blur', this);
        focusEvent(previous, 'focusout', this, true);
      }
      focused = this;
      focusEvent(this, 'focus', previous);
      focusEvent(this, 'focusin', previous, true);
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(HTMLElement.prototype, 'blur', {
    value: function () {
      if (focused !== this) return;
      focused = null;
      focusEvent(this, 'blur', null);
      focusEvent(this, 'focusout', null, true);
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  for (const name of ['focus', 'blur']) {
    const operation = function () {
      if (this !== globalThis && this !== window) throw new TypeError('Illegal invocation');
      // Script calls on an ordinary top-level browsing context do not move
      // operating-system focus or dispatch FocusEvents in Chrome.
    };
    markNative(operation, name);
    Object.defineProperty(globalThis, name, {
      value: operation,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  const mediaSlots = new WeakMap(),
    observedMedia = new Set(),
    mediaEventSlots = new WeakMap();
  class MediaQueryListEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      mediaEventSlots.set(this, { media: String(init.media ?? ''), matches: !!init.matches });
    }
    get media() {
      const s = mediaEventSlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.media;
    }
    get matches() {
      const s = mediaEventSlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.matches;
    }
  }
  expose('MediaQueryListEvent', MediaQueryListEvent);
  class MediaQueryList extends EventTarget {
    constructor(query) {
      super();
      mediaSlots.set(this, { query: String(query), onchange: null });
    }
    get media() {
      return mediaSlots.get(this).query;
    }
    get matches() {
      return !!cssMediaMatches(mediaSlots.get(this).query);
    }
    get onchange() {
      return mediaSlots.get(this).onchange;
    }
    set onchange(value) {
      const s = mediaSlots.get(this);
      if (s.onchange) this.removeEventListener('change', s.onchange);
      s.onchange = value;
      if (typeof value === 'function') this.addEventListener('change', value);
    }
    addListener(callback) {
      this.addEventListener('change', callback);
    }
    removeListener(callback) {
      this.removeEventListener('change', callback);
    }
  }
  compatibilityElementState.mediaListenerChanged = (target) => {
    if (!mediaSlots.has(target)) return;
    if ((listenersFor(target).get('change') || []).length) observedMedia.add(target);
    else observedMedia.delete(target);
  };
  // Only lists with observers are retained. Evaluation and delivery use the
  // existing Page viewport task, so no independent polling clock is added.
  compatibilityElementState.mediaObservationChange = (before) => {
    for (const list of observedMedia) {
      if (!(listenersFor(list).get('change') || []).length) {
        observedMedia.delete(list);
        continue;
      }
      const s = mediaSlots.get(list),
        matches = list.matches;
      if (before) {
        s.previousMatches = matches;
        continue;
      }
      const changed = s.previousMatches !== undefined && s.previousMatches !== matches;
      s.previousMatches = matches;
      if (changed) {
        dispatchNative(list, new MediaQueryListEvent('change', { media: list.media, matches }));
        compatibilityElementState.mediaListenerChanged(list);
      }
    }
  };
  expose('MediaQueryList', MediaQueryList);
  expose('matchMedia', (query) => new MediaQueryList(query));
  expose('requestIdleCallback', function (callback, options = {}) {
    if (typeof callback !== 'function') throw new TypeError('Expected callback');
    const requested = performance.now();
    return setTimeout(() => {
      const start = performance.now(),
        didTimeout = options.timeout !== undefined && start - requested >= Number(options.timeout);
      callback({
        didTimeout,
        timeRemaining: () => (didTimeout ? 0 : Math.max(0, 50 - (performance.now() - start))),
      });
    }, 1);
  });
  expose('cancelIdleCallback', (id) => clearTimeout(id));
  const resizeSlots = new WeakMap();
  const resizeBox = (node) => {
    const rect = node.getBoundingClientRect(),
      style = getComputedStyle(node),
      number = (name) => Number.parseFloat(style.getPropertyValue(name)) || 0,
      horizontalPadding = number('padding-left') + number('padding-right'),
      verticalPadding = number('padding-top') + number('padding-bottom'),
      horizontalBorder = number('border-left-width') + number('border-right-width'),
      verticalBorder = number('border-top-width') + number('border-bottom-width'),
      contentWidth = Math.max(0, rect.width - horizontalPadding - horizontalBorder),
      contentHeight = Math.max(0, rect.height - verticalPadding - verticalBorder);
    return {
      contentWidth,
      contentHeight,
      borderWidth: rect.width,
      borderHeight: rect.height,
    };
  };
  const scheduleResizeDelivery = (observer) => {
    const s = resizeSlots.get(observer);
    if (!s || s.timer !== null || !s.targets.size) return;
    s.timer = requestAnimationFrame(() => {
      s.timer = null;
      const entries = [];
      for (const [node, observation] of s.targets) {
        const box = resizeBox(node),
          width = observation.box === 'border-box' ? box.borderWidth : box.contentWidth,
          height = observation.box === 'border-box' ? box.borderHeight : box.contentHeight;
        if (width === observation.width && height === observation.height) continue;
        observation.width = width;
        observation.height = height;
        entries.push({
          target: node,
          contentRect: new DOMRect(0, 0, box.contentWidth, box.contentHeight),
          contentBoxSize: [{ inlineSize: box.contentWidth, blockSize: box.contentHeight }],
          borderBoxSize: [{ inlineSize: box.borderWidth, blockSize: box.borderHeight }],
          devicePixelContentBoxSize: [
            {
              inlineSize: box.contentWidth * devicePixelRatio,
              blockSize: box.contentHeight * devicePixelRatio,
            },
          ],
        });
      }
      if (entries.length) s.callback.call(undefined, entries, observer);
      scheduleResizeDelivery(observer);
    });
  };
  class ResizeObserver {
    constructor(callback) {
      if (typeof callback !== 'function') throw new TypeError('Expected callback');
      resizeSlots.set(this, { callback, targets: new Map(), timer: null });
    }
    observe(target, options = {}) {
      if (!(target instanceof Element)) throw new TypeError('Expected Element');
      const s = resizeSlots.get(this);
      s.targets.set(target, { box: options.box || 'content-box', width: null, height: null });
      scheduleResizeDelivery(this);
    }
    unobserve(target) {
      resizeSlots.get(this).targets.delete(target);
    }
    disconnect() {
      const s = resizeSlots.get(this);
      s.targets.clear();
      if (s.timer !== null) cancelAnimationFrame(s.timer);
      s.timer = null;
    }
  }
  expose('ResizeObserver', ResizeObserver);
  const detailSlots = new WeakMap();
  class CustomEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      detailSlots.set(this, init.detail ?? null);
    }
    get detail() {
      return detailSlots.get(this);
    }
    initCustomEvent(type, bubbles, cancelable, detail) {
      const s = eventSlots.get(this);
      s.type = String(type);
      s.bubbles = !!bubbles;
      s.cancelable = !!cancelable;
      detailSlots.set(this, detail);
    }
  }
  expose('CustomEvent', CustomEvent);
  Object.defineProperty(Event.prototype, 'initEvent', {
    value: function (type, bubbles = false, cancelable = false) {
      const s = eventSlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      if (s.dispatching) return;
      s.type = String(type);
      s.bubbles = !!bubbles;
      s.cancelable = !!cancelable;
      s.defaultPrevented = false;
      s.stopped = false;
      s.immediate = false;
      s.target = null;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Document.prototype, 'createEvent', {
    value: function (interfaceName) {
      switch (String(interfaceName).toLowerCase()) {
        case 'event':
        case 'events':
        case 'htmlevents':
          return new Event('');
        case 'customevent':
          return new CustomEvent('');
        case 'uievent':
        case 'uievents':
          return new UIEvent('');
        case 'mouseevent':
        case 'mouseevents':
          return new MouseEvent('');
        default:
          throw platformDOMException('The provided event type is invalid.', 'NotSupportedError');
      }
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Document.prototype, 'domain', {
    get() {
      return host.documentDomain();
    },
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Node.prototype, 'getRootNode', {
    value: function (options = {}) {
      let node = this;
      for (let parent; (parent = canonicalDOMParent(node)); ) node = parent;
      if (options.composed && node instanceof ShadowRoot) return node.host.getRootNode(options);
      return node;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Node.prototype, 'cloneNode', {
    value: function (deep = false) {
      let copy;
      if (this.nodeType === 1) {
        copy =
          this.namespaceURI && this.namespaceURI !== 'http://www.w3.org/1999/xhtml'
            ? document.createElementNS(this.namespaceURI, this.localName)
            : document.createElement(this.localName);
        for (const name of this.getAttributeNames()) {
          const attr = this.getAttributeNode(name);
          if (attr.namespaceURI)
            copy.setAttributeNS(
              attr.namespaceURI,
              name,
              trustedCloneAttribute(this, name, attr.value, attr.namespaceURI),
            );
          else copy.setAttribute(name, trustedCloneAttribute(this, name, attr.value, ''));
        }
        if (this.hasAttribute('style'))
          host.copyInlineStyle(elementSlot(this).nodeId, elementSlot(copy).nodeId);
      } else if (this.nodeType === 3) copy = document.createTextNode(this.textContent);
      else if (this.nodeType === 8) copy = document.createComment(this.textContent);
      else if (this.nodeType === 11) copy = document.createDocumentFragment();
      else throw platformDOMException('Node cannot be cloned', 'NotSupportedError');
      if (deep) {
        for (const child of Array.from(this.childNodes)) copy.appendChild(child.cloneNode(true));
        if (
          typeof globalThis.HTMLTemplateElement === 'function' &&
          this instanceof globalThis.HTMLTemplateElement
        )
          for (const child of Array.from(this.content.childNodes))
            copy.content.appendChild(child.cloneNode(true));
      }
      return copy;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Document.prototype, 'importNode', {
    value: function (node, deep = false) {
      return node.cloneNode(deep);
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Request.prototype, 'signal', {
    get() {
      const s = requestSlots.get(this);
      if (!s.signal) s.signal = new AbortSignal(hostToken);
      return s.signal;
    },
    configurable: true,
    enumerable: true,
  });
  for (const [ctor, property, attribute] of [
    [globalThis.HTMLMetaElement, 'name', 'name'],
    [globalThis.HTMLStyleElement, 'type', 'type'],
  ])
    if (ctor)
      Object.defineProperty(ctor.prototype, property, {
        get() {
          return this.getAttribute(attribute) || '';
        },
        set(value) {
          this.setAttribute(attribute, String(value));
        },
        enumerable: true,
        configurable: true,
      });
  Object.defineProperty(Response.prototype, 'clone', {
    value: function () {
      const s = responseSlots.get(this);
      if (s.bodyUsed) throw new TypeError('Body already used');
      const copy = new Response(s.body, {
        status: s.status,
        statusText: s.statusText,
        headers: new Headers(s.headers),
      });
      Object.assign(responseSlots.get(copy), {
        url: s.url,
        type: s.type,
        redirected: s.redirected,
      });
      return copy;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  Object.defineProperty(Node.prototype, 'nodeValue', {
    get() {
      return this.nodeType === 3 || this.nodeType === 8 ? this.textContent : null;
    },
    set(value) {
      if (this.nodeType === 3 || this.nodeType === 8) this.textContent = value ?? '';
    },
    enumerable: true,
    configurable: true,
  });
  Object.defineProperty(Document.prototype, 'firstElementChild', {
    get() {
      return this.documentElement;
    },
    enumerable: true,
    configurable: true,
  });
  Object.defineProperty(Document.prototype, 'getElementsByName', {
    value: function (name) {
      return nodeList(
        compatibilitySelectors.query(
          this,
          '[name="' + String(name).replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"]',
          false,
          true,
        ),
      );
    },
    writable: true,
    enumerable: true,
    configurable: true,
  });
  // HTMLCollection is live: class mutations, insertion and removal are read
  // from the canonical tree on every access, with ASCII whitespace tokens.
  for (const ctor of [Document, Element])
    Object.defineProperty(ctor.prototype, 'getElementsByClassName', {
      value: function (names) {
        if (arguments.length === 0) throw new TypeError('Expected class names');
        const root = this,
          normalize = (value) =>
            (root instanceof Document ? root : root.ownerDocument)?.compatMode === 'BackCompat'
              ? value.replace(/[A-Z]/g, (c) => c.toLowerCase())
              : value;
        names = String(names);
        const tokens = [
          ...new Set(
            normalize(names)
              .split(/[\t\n\f\r ]+/)
              .filter(Boolean),
          ),
        ];
        // The arena revision includes parser, host and cross-realm writes.
        // Cache membership IDs only; wrappers and mutable fields stay canonical.
        let revision,
          ids = [];
        return cachedHTMLCollection(root, 'class', names, () => {
          if (!tokens.length) return [];
          const current = host.domRevision();
          if (current !== revision) {
            ids = host.classIDs(
              root === document ? documentRootID : elementSlot(root).nodeId,
              tokens.join(' '),
              normalize('A') === 'a' ? 1 : 0,
            );
            revision = current;
          }
          return ids;
        });
      },
      writable: true,
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(DocumentFragment.prototype, 'textContent', {
    get() {
      return Array.from(this.childNodes)
        .map((node) => node.textContent || '')
        .join('');
    },
    set(value) {
      for (const child of Array.from(this.childNodes)) this.removeChild(child);
      if (value != null && String(value) !== '')
        this.appendChild(document.createTextNode(String(value)));
    },
    enumerable: true,
    configurable: true,
  });
  if (typeof globalThis.CSS !== 'undefined')
    Object.defineProperty(globalThis.CSS, 'escape', {
      value: function (value) {
        const s = String(value);
        let out = '';
        for (let i = 0; i < s.length; i++) {
          const c = s.charCodeAt(i);
          if (c === 0) {
            out += '\ufffd';
            continue;
          }
          if (
            c < 32 ||
            c === 127 ||
            (i === 0 && c >= 48 && c <= 57) ||
            (i === 1 && c >= 48 && c <= 57 && s[0] === '-')
          ) {
            out += '\\' + c.toString(16) + ' ';
            continue;
          }
          if (i === 0 && s.length === 1 && s[i] === '-') {
            out += '\\-';
            continue;
          }
          out +=
            c >= 128 ||
            c === 45 ||
            c === 95 ||
            (c >= 48 && c <= 57) ||
            (c >= 65 && c <= 90) ||
            (c >= 97 && c <= 122)
              ? s[i]
              : '\\' + s[i];
        }
        return out;
      },
      writable: true,
      configurable: true,
      enumerable: true,
    });
  if (typeof globalThis.ClipboardItem === 'function') {
    const items = new WeakMap();
    class ClipboardItem {
      constructor(data, options = {}) {
        const types = Object.keys(data);
        if (!types.length) throw new TypeError('Empty clipboard item');
        items.set(this, {
          data: { ...data },
          types,
          presentationStyle: options.presentationStyle || 'unspecified',
        });
      }
      get types() {
        return Object.freeze(items.get(this).types.slice());
      }
      get presentationStyle() {
        return items.get(this).presentationStyle;
      }
      async getType(type) {
        const s = items.get(this);
        if (!s.types.includes(String(type)))
          throw platformDOMException('Type not found', 'NotFoundError');
        const value = await s.data[type];
        return typeof value === 'string' ? new Blob([value], { type }) : value;
      }
      static supports(type) {
        return ['text/plain', 'text/html', 'image/png'].includes(String(type));
      }
    }
    expose('ClipboardItem', ClipboardItem);
  }
}
