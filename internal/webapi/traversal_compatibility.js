// TreeWalker and NodeIterator share these live canonical document-order
// primitives. In particular, traversal is never snapshotted at construction.
{
  const ACCEPT = 1,
    REJECT = 2,
    SKIP = 3,
    slots = new WeakMap();
  // Traversal algorithms read the canonical tree, never author-shadowed Node
  // accessors. Reuse the existing revision-owned readbacks so repeated walks
  // do not cross into the host for every edge. Filter callbacks can mutate the
  // tree: the next read must observe the new canonical revision.
  const firstChild = (node) => {
    const fragment = fragmentSlots.get(node);
    if (fragment) return fragment.children[0] || null;
    const slot = elementSlot(node);
    if (slot && (slot.type === 'text' || slot.type === 'comment')) return null;
    const id = slot ? slot.nodeId : realmDocumentRootID;
    const record = domReadRecord(node);
    if (!record) return wrap(host.firstChild(id));
    if (!Object.hasOwn(record, 'traversalFirstChild'))
      record.traversalFirstChild = wrap(host.firstChild(id));
    return record.traversalFirstChild;
  };
  const lastChild = (node) => {
    const children = canonicalDOMChildren(node);
    return children[children.length - 1] || null;
  };
  const siblingNode = (node, direction) => {
    const slot = elementSlot(node);
    if (slot && !syntheticParents.has(node)) {
      const record = domReadRecord(node);
      const key = direction === 1 ? 'traversalNextSibling' : 'traversalPreviousSibling';
      if (!record) return wrap(host.sibling(slot.nodeId, direction));
      if (!Object.hasOwn(record, key)) record[key] = wrap(host.sibling(slot.nodeId, direction));
      return record[key];
    }
    const parent = canonicalDOMParent(node);
    if (!parent) return null;
    const children = canonicalDOMChildren(parent);
    return children[children.indexOf(node) + direction] || null;
  };
  const nodeType = (node) => {
    const slot = elementSlot(node);
    if (slot)
      return slot.type === 'element'
        ? 1
        : slot.type === 'text'
          ? 3
          : slot.type === 'comment'
            ? 8
            : slot.type === 'fragment'
              ? 11
              : 0;
    return fragmentSlots.has(node) ? 11 : 9;
  };
  const state = (receiver, kind) => {
    const value = slots.get(receiver);
    if (!value || value.kind !== kind) throw new TypeError('Illegal invocation');
    return value;
  };
  const accept = (s, node) => {
    if (!(s.whatToShow & (1 << (nodeType(node) - 1)))) return SKIP;
    if (!s.filter) return ACCEPT;
    if (s.active) throw platformDOMException('The filter is already active.', 'InvalidStateError');
    s.active = true;
    try {
      return (
        Number(typeof s.filter === 'function' ? s.filter(node) : s.filter.acceptNode(node)) & 65535
      );
    } finally {
      s.active = false;
    }
  };
  const next = (root, node) => {
    const child = firstChild(node);
    if (child) return child;
    while (node && node !== root) {
      const sibling = siblingNode(node, 1);
      if (sibling) return sibling;
      node = canonicalDOMParent(node);
    }
    return null;
  };
  const after = (root, node) => {
    while (node && node !== root) {
      const sibling = siblingNode(node, 1);
      if (sibling) return sibling;
      node = canonicalDOMParent(node);
    }
    return null;
  };
  const previous = (root, node) => {
    if (node === root) return null;
    const sibling = siblingNode(node, -1);
    if (sibling) {
      node = sibling;
      for (let child; (child = lastChild(node)); ) node = child;
      return node;
    }
    return canonicalDOMParent(node);
  };
  const validate = (root, filter) => {
    if (!isDOMNode(root))
      throw new TypeError("Failed to create traversal object: parameter 1 is not of type 'Node'.");
    if (filter !== null && typeof filter !== 'function' && typeof filter !== 'object')
      throw new TypeError('The provided filter is not an object.');
  };

  if (typeof globalThis.TreeWalker === 'function') {
    const proto = globalThis.TreeWalker.prototype,
      walkerState = (receiver) => state(receiver, 'walker');
    const child = (s, reverse) => {
      let node = reverse ? lastChild(s.current) : firstChild(s.current);
      while (node) {
        const result = accept(s, node);
        if (result === ACCEPT) return (s.current = node);
        if (result === SKIP && (reverse ? lastChild(node) : firstChild(node))) {
          node = reverse ? lastChild(node) : firstChild(node);
          continue;
        }
        while (node) {
          const sibling = reverse ? siblingNode(node, -1) : siblingNode(node, 1);
          if (sibling) {
            node = sibling;
            break;
          }
          node = canonicalDOMParent(node);
          if (!node || node === s.current) return null;
        }
      }
      return null;
    };
    const sibling = (s, reverse) => {
      if (s.current === s.root) return null;
      let node = s.current;
      while (node && node !== s.root) {
        let candidate = reverse ? siblingNode(node, -1) : siblingNode(node, 1);
        while (candidate) {
          const result = accept(s, candidate);
          if (result === ACCEPT) return (s.current = candidate);
          if (result === SKIP && (reverse ? lastChild(candidate) : firstChild(candidate)))
            candidate = reverse ? lastChild(candidate) : firstChild(candidate);
          else candidate = reverse ? siblingNode(candidate, -1) : siblingNode(candidate, 1);
        }
        node = canonicalDOMParent(node);
        if (node && node !== s.root && accept(s, node) === ACCEPT) return null;
      }
      return null;
    };
    Object.defineProperties(proto, {
      root: {
        get() {
          return walkerState(this).root;
        },
        configurable: true,
        enumerable: true,
      },
      whatToShow: {
        get() {
          return walkerState(this).whatToShow;
        },
        configurable: true,
        enumerable: true,
      },
      filter: {
        get() {
          return walkerState(this).filter;
        },
        configurable: true,
        enumerable: true,
      },
      currentNode: {
        get() {
          return walkerState(this).current;
        },
        set(node) {
          if (!isDOMNode(node)) throw new TypeError('currentNode must be a Node');
          walkerState(this).current = node;
        },
        configurable: true,
        enumerable: true,
      },
      parentNode: {
        value: function () {
          const s = walkerState(this);
          for (let node = canonicalDOMParent(s.current); node; node = canonicalDOMParent(node)) {
            if (node === s.root || accept(s, node) === ACCEPT) return (s.current = node);
          }
          return null;
        },
        configurable: true,
        writable: true,
      },
      firstChild: {
        value: function () {
          return child(walkerState(this), false);
        },
        configurable: true,
        writable: true,
      },
      lastChild: {
        value: function () {
          return child(walkerState(this), true);
        },
        configurable: true,
        writable: true,
      },
      nextSibling: {
        value: function () {
          return sibling(walkerState(this), false);
        },
        configurable: true,
        writable: true,
      },
      previousSibling: {
        value: function () {
          return sibling(walkerState(this), true);
        },
        configurable: true,
        writable: true,
      },
      nextNode: {
        value: function () {
          const s = walkerState(this);
          let node = next(s.root, s.current);
          while (node) {
            const result = accept(s, node);
            if (result === ACCEPT) return (s.current = node);
            node = result === REJECT ? after(s.root, node) : next(s.root, node);
          }
          return null;
        },
        configurable: true,
        writable: true,
      },
      previousNode: {
        value: function () {
          const s = walkerState(this);
          let node = s.current;
          while ((node = previous(s.root, node))) {
            const result = accept(s, node);
            if (result === ACCEPT) return (s.current = node);
            if (result === REJECT)
              while (
                canonicalDOMParent(node) &&
                canonicalDOMParent(node) !== s.root &&
                accept(s, canonicalDOMParent(node)) === REJECT
              )
                node = canonicalDOMParent(node);
          }
          return null;
        },
        configurable: true,
        writable: true,
      },
    });
    Object.defineProperty(Document.prototype, 'createTreeWalker', {
      value: function (root, whatToShow = 0xffffffff, filter = null) {
        validate(root, filter);
        const value = Object.create(proto);
        slots.set(value, {
          kind: 'walker',
          root,
          current: root,
          whatToShow: Number(whatToShow) >>> 0,
          filter,
          active: false,
        });
        return value;
      },
      configurable: true,
      writable: true,
      enumerable: true,
    });
  }

  if (typeof globalThis.NodeIterator === 'function') {
    const proto = globalThis.NodeIterator.prototype,
      iteratorState = (receiver) => state(receiver, 'iterator');
    Object.defineProperties(proto, {
      root: {
        get() {
          return iteratorState(this).root;
        },
        configurable: true,
        enumerable: true,
      },
      whatToShow: {
        get() {
          return iteratorState(this).whatToShow;
        },
        configurable: true,
        enumerable: true,
      },
      filter: {
        get() {
          return iteratorState(this).filter;
        },
        configurable: true,
        enumerable: true,
      },
      referenceNode: {
        get() {
          return iteratorState(this).reference;
        },
        configurable: true,
        enumerable: true,
      },
      pointerBeforeReferenceNode: {
        get() {
          return iteratorState(this).before;
        },
        configurable: true,
        enumerable: true,
      },
      nextNode: {
        value: function () {
          const s = iteratorState(this);
          let node = s.before ? s.reference : next(s.root, s.reference);
          while (node) {
            s.reference = node;
            s.before = false;
            if (accept(s, node) === ACCEPT) return node;
            node = next(s.root, node);
          }
          return null;
        },
        configurable: true,
        writable: true,
      },
      previousNode: {
        value: function () {
          const s = iteratorState(this);
          let node = s.before ? previous(s.root, s.reference) : s.reference;
          while (node) {
            s.reference = node;
            s.before = true;
            if (accept(s, node) === ACCEPT) return node;
            node = previous(s.root, node);
          }
          return null;
        },
        configurable: true,
        writable: true,
      },
      detach: {
        value: function () {
          iteratorState(this);
        },
        configurable: true,
        writable: true,
      },
    });
    Object.defineProperty(Document.prototype, 'createNodeIterator', {
      value: function (root, whatToShow = 0xffffffff, filter = null) {
        validate(root, filter);
        const value = Object.create(proto);
        slots.set(value, {
          kind: 'iterator',
          root,
          reference: root,
          before: true,
          whatToShow: Number(whatToShow) >>> 0,
          filter,
          active: false,
        });
        return value;
      },
      configurable: true,
      writable: true,
      enumerable: true,
    });
  }
}
