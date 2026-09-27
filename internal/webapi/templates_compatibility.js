// HTML template contents use host-owned fragment nodes. No parallel child array
// is maintained for these fragments: traversal, mutation and serialization all
// read the same canonical IDs.
(() => {
  const Template = globalThis.HTMLTemplateElement;
  if (typeof Template !== 'function') return;
  htmlElementInterfaces.TEMPLATE = 'HTMLTemplateElement';
  const define = (prototype, name, descriptor) =>
    Object.defineProperty(prototype, name, { enumerable: true, configurable: true, ...descriptor });
  const canonicalFragment = (node) => elementSlot(node)?.type === 'fragment';
  let contentObserved = false,
    importing = false;
  const contentRoots = new WeakSet();
  templateTreeIsInert = (node) => contentObserved && contentRoots.has(node.getRootNode());
  define(Template.prototype, 'content', {
    get() {
      const slot = elementSlot(this);
      if (!slot || slot.tagName !== 'TEMPLATE') throw new TypeError('Illegal invocation');
      ensureCycleValidation();
      const content = wrap(host.templateContent(slot.nodeId));
      contentRoots.add(content);
      return content;
    },
  });
  define(Template.prototype, 'innerHTML', {
    get() {
      const slot = elementSlot(this);
      if (!slot || slot.tagName !== 'TEMPLATE') throw new TypeError('Illegal invocation');
      return host.innerHTML(slot.nodeId);
    },
    set(value) {
      const slot = elementSlot(this);
      if (!slot || slot.tagName !== 'TEMPLATE') throw new TypeError('Illegal invocation');
      // Parse once into a temporary canonical template context, then use the
      // shared replace-all algorithm so observers see one content record.
      const parsed = document.createElement('template');
      host.setInnerHTML(
        elementSlot(parsed).nodeId,
        trustedConvert(
          value === null ? '' : value,
          'TrustedHTML',
          'Element innerHTML',
          "Failed to set the 'innerHTML' property on 'Element': ",
          this,
        ),
      );
      this.content.replaceChildren(parsed.content);
    },
  });
  for (const name of ['children', 'firstElementChild', 'lastElementChild', 'childElementCount']) {
    const original = Object.getOwnPropertyDescriptor(DocumentFragment.prototype, name);
    define(DocumentFragment.prototype, name, {
      get() {
        if (!canonicalFragment(this)) return original.get.call(this);
        const slot = elementSlot(this);
        if (name === 'children') return htmlCollection(() => host.elementChildren(slot.nodeId));
        if (name === 'firstElementChild') return wrap(host.firstElementChild(slot.nodeId));
        const nodes = host.elementChildren(slot.nodeId);
        return name === 'childElementCount' ? nodes.length : wrap(nodes[nodes.length - 1]);
      },
    });
  }
  define(DocumentFragment.prototype, 'textContent', {
    get() {
      return canonicalFragment(this)
        ? canonicalTextContent(elementSlot(this).nodeId)
        : Array.from(this.childNodes)
            .filter((node) => node.nodeType !== 8)
            .map((node) => node.textContent || '')
            .join('');
    },
    set(value) {
      const text = value == null ? '' : String(value);
      this.replaceChildren(...(text ? [document.createTextNode(text)] : []));
    },
  });
  // Parent links intentionally stop at the fragment. Cycle validation alone
  // follows template-host links, like shadow-host links in the platform model.
  function ensureCycleValidation() {
    if (contentObserved) return;
    contentObserved = true;
    validateTemplateInsertion = (parent, node) => {
      const parentSlot = elementSlot(parent),
        childSlot = elementSlot(node);
      const candidates = childSlot
        ? [node]
        : node instanceof DocumentFragment
          ? Array.from(node.childNodes)
          : [];
      if (parentSlot)
        for (const candidate of candidates) {
          const slot = elementSlot(candidate);
          if (slot && host.hostIncludingContains(slot.nodeId, parentSlot.nodeId))
            throw platformDOMException(
              'Insertion would create a host-inclusive cycle',
              'HierarchyRequestError',
            );
        }
    };
  }
  const originalClone = Node.prototype.cloneNode;
  define(Node.prototype, 'cloneNode', {
    value: function (deep = false) {
      let copy;
      if (this instanceof Template) {
        copy = originalClone.call(this, false);
        if (deep) {
          for (const child of this.childNodes) copy.appendChild(child.cloneNode(true));
          customElementCloneInert++;
          try {
            for (const child of this.content.childNodes)
              copy.content.appendChild(child.cloneNode(true));
          } finally {
            customElementCloneInert--;
          }
        }
      } else {
        const inert = !importing && templateTreeIsInert(this);
        if (inert) customElementCloneInert++;
        try {
          copy = originalClone.call(this, deep);
        } finally {
          if (inert) customElementCloneInert--;
        }
      }
      const sourceSlot = elementSlot(this),
        copySlot = elementSlot(copy);
      if (sourceSlot && copySlot) host.copyNodeState(sourceSlot.nodeId, copySlot.nodeId);
      return copy;
    },
    writable: true,
  });
  define(Document.prototype, 'importNode', {
    value: function (node, deep = false) {
      if (!isDOMNode(node)) throw new TypeError('Expected Node');
      const old = importing;
      importing = true;
      try {
        return node.cloneNode(deep);
      } finally {
        importing = old;
      }
    },
    writable: true,
  });
})();
