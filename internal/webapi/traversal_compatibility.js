// TreeWalker and NodeIterator share these live canonical document-order
// primitives. In particular, traversal is never snapshotted at construction.
{
  const ACCEPT = 1,
    REJECT = 2,
    SKIP = 3,
    slots = new WeakMap();
  const state = (receiver, kind) => {
    const value = slots.get(receiver);
    if (!value || value.kind !== kind) throw new TypeError('Illegal invocation');
    return value;
  };
  const accept = (s, node) => {
    if (!(s.whatToShow & (1 << (node.nodeType - 1)))) return SKIP;
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
    if (node.firstChild) return node.firstChild;
    while (node && node !== root) {
      if (node.nextSibling) return node.nextSibling;
      node = node.parentNode;
    }
    return null;
  };
  const after = (root, node) => {
    while (node && node !== root) {
      if (node.nextSibling) return node.nextSibling;
      node = node.parentNode;
    }
    return null;
  };
  const previous = (root, node) => {
    if (node === root) return null;
    if (node.previousSibling) {
      node = node.previousSibling;
      while (node.lastChild) node = node.lastChild;
      return node;
    }
    return node.parentNode;
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
      let node = reverse ? s.current.lastChild : s.current.firstChild;
      while (node) {
        const result = accept(s, node);
        if (result === ACCEPT) return (s.current = node);
        if (result === SKIP && (reverse ? node.lastChild : node.firstChild)) {
          node = reverse ? node.lastChild : node.firstChild;
          continue;
        }
        while (node) {
          const sibling = reverse ? node.previousSibling : node.nextSibling;
          if (sibling) {
            node = sibling;
            break;
          }
          node = node.parentNode;
          if (!node || node === s.current) return null;
        }
      }
      return null;
    };
    const sibling = (s, reverse) => {
      if (s.current === s.root) return null;
      let node = s.current;
      while (node && node !== s.root) {
        let candidate = reverse ? node.previousSibling : node.nextSibling;
        while (candidate) {
          const result = accept(s, candidate);
          if (result === ACCEPT) return (s.current = candidate);
          if (result === SKIP && (reverse ? candidate.lastChild : candidate.firstChild))
            candidate = reverse ? candidate.lastChild : candidate.firstChild;
          else candidate = reverse ? candidate.previousSibling : candidate.nextSibling;
        }
        node = node.parentNode;
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
          for (let node = s.current.parentNode; node; node = node.parentNode) {
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
          let node = s.current;
          for (;;) {
            node = next(s.root, node);
            if (!node) return null;
            const result = accept(s, node);
            if (result === ACCEPT) return (s.current = node);
            if (result === REJECT) node = after(s.root, node);
            while (node) {
              const result = accept(s, node);
              if (result === ACCEPT) return (s.current = node);
              node = result === REJECT ? after(s.root, node) : next(s.root, node);
            }
            return null;
          }
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
                node.parentNode &&
                node.parentNode !== s.root &&
                accept(s, node.parentNode) === REJECT
              )
                node = node.parentNode;
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
