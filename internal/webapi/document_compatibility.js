// Every Document has its own canonical host root. The wrapper map preserves
// identity when traversal reaches an inert document through one of its nodes.
const documentWrappers = new Map();
let registerDocumentGetterBinding;
const documentImplementations = new WeakMap();
const fragmentOwnerDocuments = new WeakMap();
const documentIsQuirks = (value) => {
  const slot = elementSlot(value);
  return (
    (slot?.contentType || 'text/html') === 'text/html' &&
    (value === document || slot?.parsedDocument) &&
    !host
      .nodeChildren(value === document ? realmDocumentRootID : slot.nodeId)
      .some((node) => node.type === 'doctype')
  );
};
function wrapDocumentNode(data) {
  if (data.nodeId === realmDocumentRootID) return document;
  let value = documentWrappers.get(data.nodeId);
  if (!value) {
    const reference = host.documentReference(data.nodeId);
    if (reference) {
      value = unwrapCrossRealm(reference.frame, reference);
      documentWrappers.set(data.nodeId, value);
    }
  }
  if (!value) {
    value = Object.create(
      (data.contentType && data.contentType !== 'text/html' ? globalThis.XMLDocument : HTMLDocument)
        .prototype,
    );
    elementData.set(value, data);
    Object.defineProperty(value, 'location', documentLocationDescriptor);
    documentWrappers.set(data.nodeId, value);
    if (registerDocumentGetterBinding) registerDocumentGetterBinding(value);
  }
  return value;
}
{
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
  const docID = (value) => (value === document ? realmDocumentRootID : elementSlot(value)?.nodeId);
  const validDocument = (value) => {
    if (value !== document && elementSlot(value)?.type !== 'document')
      throw new TypeError('Illegal invocation');
    return docID(value);
  };
  // Detached documents have no visible browsing context. Prefixes share the
  // canonical accessors, including their receiver checks, rather than state.
  for (const name of ['hidden', 'visibilityState']) {
    const original = Object.getOwnPropertyDescriptor(Document.prototype, name).get;
    const read = function () {
      validDocument(this);
      return this === document ? original.call(this) : name === 'hidden' ? true : 'hidden';
    };
    accessor(Document.prototype, name, read);
    accessor(
      Document.prototype,
      name === 'hidden' ? 'webkitHidden' : 'webkitVisibilityState',
      function () {
        return read.call(this);
      },
    );
  }
  const contentType = (value) => elementSlot(value)?.contentType || 'text/html';
  const xmlName = (name) => validateDOMName(name, false, 'createElement', 'Document');
  const qualifiedName = (namespace, name) =>
    validateDOMQualifiedName(namespace, name, false, 'createElementNS', 'Document');
  const ownerOf = (value) => {
    if (value instanceof Document) return null;
    const slot = elementSlot(value);
    if (slot) return cachedDOMOwnerDocument(value);
    return fragmentOwnerDocuments.get(value) || document;
  };
  accessor(Node.prototype, 'ownerDocument', function () {
    return ownerOf(this);
  });
  const originalType = Object.getOwnPropertyDescriptor(Node.prototype, 'nodeType').get;
  accessor(Node.prototype, 'nodeType', function () {
    const type = elementSlot(this)?.type;
    return this instanceof Document ? 9 : type === 'doctype' ? 10 : originalType.call(this);
  });
  // Node's inherited binding must select private state, not an Element getter
  // moved here during exposure publication or a shadowable public tagName.
  accessor(Node.prototype, 'nodeName', function () {
    const slot = elementSlot(this);
    if (this === document || slot?.type === 'document') return '#document';
    if (slot?.type === 'element') return slot.qualifiedName || slot.tagName;
    if (slot?.type === 'doctype') return slot.tagName;
    if (slot?.type === 'text') return '#text';
    if (slot?.type === 'comment') return '#comment';
    if (isDOMFragment(this)) return '#document-fragment';
    throw new TypeError('Illegal invocation');
  });
  if (globalThis.DocumentType) {
    accessor(globalThis.DocumentType.prototype, 'name', function () {
      return elementSlot(this)?.tagName || '';
    });
    for (const name of ['publicId', 'systemId'])
      accessor(globalThis.DocumentType.prototype, name, function () {
        return (
          host.nodeData(elementSlot(this).nodeId).attributes?.[
            name === 'publicId' ? 'public' : 'system'
          ] || ''
        );
      });
    accessor(
      globalThis.DocumentType.prototype,
      'textContent',
      function () {
        return null;
      },
      function () {},
    );
  }
  const connected = Object.getOwnPropertyDescriptor(Node.prototype, 'isConnected').get;
  accessor(Node.prototype, 'isConnected', function () {
    if (this instanceof Document || connected.call(this)) return true;
    return this.getRootNode({ composed: true }) instanceof Document;
  });
  const adopt = (node, doc) => {
    const id = validDocument(doc),
      slot = elementSlot(node);
    if (slot) {
      host.adoptNodeDocument(slot.nodeId, id);
      invalidateDOMCollections();
    } else if (isDOMFragment(node)) {
      fragmentOwnerDocuments.set(node, doc);
      for (const child of node.childNodes) adopt(child, doc);
    }
    return node;
  };
  for (const name of [
    'createElement',
    'createElementNS',
    'createTextNode',
    'createComment',
    'createDocumentFragment',
  ]) {
    const original = Document.prototype[name];
    member(Document.prototype, name, function (...args) {
      validDocument(this);
      if (
        contentType(this) !== 'text/html' &&
        (name === 'createElement' || name === 'createElementNS')
      ) {
        if (args.length < (name === 'createElement' ? 1 : 2))
          throw new TypeError('Not enough arguments');
        const namespace =
          name === 'createElement'
            ? contentType(this) === 'application/xhtml+xml'
              ? 'http://www.w3.org/1999/xhtml'
              : ''
            : args[0] == null
              ? ''
              : String(args[0]);
        const local =
          name === 'createElement'
            ? xmlName(String(args[0]))
            : qualifiedName(namespace, String(args[1]));
        return wrap(host.createDocumentElement(docID(this), namespace, local));
      }
      // The host creates nodes in the active canonical document. Its root ID
      // is rebound on bootstrap restoration, so ordinary creation needs no
      // second host lookup or adoption pass.
      // A borrowed method must construct in the receiver's document realm.
      // Creation in this realm followed by adoption leaves the wrong prototype.
      if (this !== document && this.defaultView)
        return this.defaultView.Document.prototype[name].apply(this, args);
      const node = original.apply(this, args);
      return this === document ? node : adopt(node, this);
    });
  }
  accessor(Document.prototype, 'documentElement', function () {
    validDocument(this);
    return Array.from(this.childNodes).find((node) => node.nodeType === 1) || null;
  });
  const firstChild = Object.getOwnPropertyDescriptor(Node.prototype, 'firstChild').get;
  accessor(Node.prototype, 'firstChild', function () {
    if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
    if (this === document || elementSlot(this)?.type === 'document')
      return documentFirstChild(this);
    return functionSourceApply(firstChild, this, []);
  });
  delete Document.prototype.firstChild;
  for (const name of ['head', 'body'])
    accessor(Document.prototype, name, function () {
      validDocument(this);
      const root = this.documentElement;
      if (root?.namespaceURI !== 'http://www.w3.org/1999/xhtml' || root.localName !== 'html')
        return null;
      return (
        Array.from(root.children).find(
          (node) =>
            node.namespaceURI === 'http://www.w3.org/1999/xhtml' &&
            (node.localName === name || (name === 'body' && node.localName === 'frameset')),
        ) || null
      );
    });
  const bodyGetter = Object.getOwnPropertyDescriptor(Document.prototype, 'body').get;
  accessor(Document.prototype, 'body', bodyGetter, function (value) {
    validDocument(this);
    const slot = value == null ? null : elementSlot(value);
    if (
      value != null &&
      (!slot || slot.type !== 'element' || slot.namespaceURI !== 'http://www.w3.org/1999/xhtml')
    )
      throw new TypeError('The provided value is not of type HTMLElement');
    if (!slot || !['body', 'frameset'].includes(slot.qualifiedName || slot.tagName.toLowerCase()))
      throw platformDOMException(
        'The new body element must be a body or frameset element',
        'HierarchyRequestError',
      );
    const old = bodyGetter.call(this);
    if (value === old) return;
    // Use canonical mutation paths so replacement also adopts the subtree and
    // delivers the same mutation/lifecycle notifications as ordinary insertion.
    if (old) old.parentNode.replaceChild(value, old);
    else {
      const root = this.documentElement;
      if (!root)
        throw platformDOMException('The document has no document element', 'HierarchyRequestError');
      root.appendChild(value);
    }
  });
  const tagName = Object.getOwnPropertyDescriptor(Element.prototype, 'tagName').get;
  accessor(Element.prototype, 'tagName', function () {
    return elementSlot(this)?.qualifiedName || tagName.call(this);
  });
  accessor(Element.prototype, 'localName', function () {
    const name = elementSlot(this)?.qualifiedName;
    return name ? name.split(':').at(-1) : tagName.call(this).toLowerCase();
  });
  accessor(Element.prototype, 'prefix', function () {
    const name = elementSlot(this)?.qualifiedName;
    return name?.includes(':') ? name.split(':')[0] : null;
  });
  for (const prototype of [Document.prototype, Element.prototype])
    member(prototype, 'getElementsByTagName', function (name) {
      if (this !== document && !['document', 'element'].includes(elementSlot(this)?.type))
        throw new TypeError('Illegal invocation');
      name = String(name);
      const lower = name.toLowerCase(),
        root = this;
      return cachedHTMLCollection(root, 'tag', name, () =>
        compatibilitySelectors
          .query(root, '*')
          .filter(
            (node) =>
              name === '*' ||
              node.localName ===
                (node.namespaceURI === 'http://www.w3.org/1999/xhtml' ? lower : name),
          )
          .map((node) => elementSlot(node).nodeId),
      );
    });
  accessor(Document.prototype, 'defaultView', function () {
    validDocument(this);
    return this === document && host.documentActive() ? window : null;
  });
  const nodeTextContent = Object.getOwnPropertyDescriptor(Node.prototype, 'textContent');
  const nullTextContent = (value) => {
    const type = elementSlot(value)?.type;
    return value === document || type === 'document' || type === 'doctype';
  };
  accessor(
    Node.prototype,
    'textContent',
    function () {
      if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
      return nullTextContent(this) ? null : functionSourceApply(nodeTextContent.get, this, []);
    },
    function (value) {
      if (!isDOMNode(this)) throw new TypeError('Illegal invocation');
      if (nullTextContent(this)) {
        if (value != null) bindingString(value);
        return;
      }
      return functionSourceApply(nodeTextContent.set, this, [value]);
    },
  );
  delete Document.prototype.textContent;
  for (const name of ['URL', 'documentURI'])
    accessor(Document.prototype, name, function () {
      validDocument(this);
      return this === document ? location.href : elementSlot(this)?.documentURL || 'about:blank';
    });
  delete Document.prototype.location;
  const ready = Object.getOwnPropertyDescriptor(Document.prototype, 'readyState').get;
  accessor(Document.prototype, 'readyState', function () {
    validDocument(this);
    return this === document ? ready.call(this) : 'complete';
  });
  accessor(Document.prototype, 'compatMode', function () {
    validDocument(this);
    return documentIsQuirks(this) ? 'BackCompat' : 'CSS1Compat';
  });
  accessor(Document.prototype, 'doctype', function () {
    validDocument(this);
    return Array.from(this.childNodes).find((node) => node.nodeType === 10) || null;
  });
  accessor(Document.prototype, 'contentType', function () {
    validDocument(this);
    return contentType(this);
  });
  for (const name of ['characterSet', 'charset', 'inputEncoding'])
    accessor(Document.prototype, name, function () {
      validDocument(this);
      return 'UTF-8';
    });
  const current = Object.getOwnPropertyDescriptor(Document.prototype, 'currentScript').get;
  accessor(Document.prototype, 'currentScript', function () {
    validDocument(this);
    return this === document ? current.call(this) : null;
  });
  const title = Object.getOwnPropertyDescriptor(Document.prototype, 'title');
  accessor(
    Document.prototype,
    'title',
    function () {
      validDocument(this);
      if (this === document) return title.get.call(this);
      return (compatibilitySelectors.query(this, 'title', true)?.textContent || '')
        .replace(/[\t\n\f\r ]+/g, ' ')
        .trim();
    },
    function (value) {
      validDocument(this);
      if (this === document) return title.set.call(this, value);
      let node = compatibilitySelectors.query(this, 'title', true);
      if (!node && this.head) {
        node = this.createElement('title');
        this.head.appendChild(node);
      }
      if (node) node.textContent = String(value);
    },
  );
  const implSlots = new WeakSet();
  class DOMImplementation {
    constructor(token) {
      if (token !== hostToken) throw new TypeError('Illegal constructor');
      implSlots.add(this);
    }
    createHTMLDocument(title) {
      if (!implSlots.has(this)) throw new TypeError('Illegal invocation');
      return wrap(host.createHTMLDocument(...(title !== undefined ? [String(title)] : [])));
    }
    createDocument(namespace, name, doctype = null) {
      if (!implSlots.has(this)) throw new TypeError('Illegal invocation');
      if (arguments.length < 2) throw new TypeError('Not enough arguments');
      namespace = namespace == null ? '' : String(namespace);
      name = String(name);
      if (doctype !== null && !(doctype instanceof globalThis.DocumentType))
        throw new TypeError('Expected DocumentType');
      if (name) name = qualifiedName(namespace, name);
      const result = wrap(host.createXMLDocument(namespace, name));
      if (doctype) result.insertBefore(doctype, result.firstChild);
      return result;
    }
    hasFeature() {
      if (!implSlots.has(this)) throw new TypeError('Illegal invocation');
      return true;
    }
  }
  Object.defineProperty(DOMImplementation.prototype, Symbol.toStringTag, {
    value: 'DOMImplementation',
    configurable: true,
  });
  markNative(DOMImplementation, 'DOMImplementation');
  for (const name of ['createHTMLDocument', 'createDocument', 'hasFeature'])
    markNative(DOMImplementation.prototype[name], name);
  Object.defineProperty(globalThis, 'DOMImplementation', {
    value: DOMImplementation,
    writable: true,
    configurable: true,
  });
  accessor(Document.prototype, 'implementation', function () {
    validDocument(this);
    let value = documentImplementations.get(this);
    if (!value) {
      value = new DOMImplementation(hostToken);
      documentImplementations.set(this, value);
    }
    return value;
  });
  member(Document.prototype, 'adoptNode', function (node) {
    validDocument(this);
    if (!isDOMNode(node)) throw new TypeError('Expected a Node');
    if (node instanceof Document || node instanceof ShadowRoot)
      throw platformDOMException('Node cannot be adopted', 'NotSupportedError');
    if (node.parentNode) node.parentNode.removeChild(node);
    return adopt(node, this);
  });
  const clone = Node.prototype.cloneNode;
  member(Node.prototype, 'cloneNode', function (deep = false) {
    const result = clone.call(this, deep);
    return adopt(result, this.ownerDocument || document);
  });
  member(Document.prototype, 'importNode', function (node, deep = false) {
    validDocument(this);
    if (!isDOMNode(node)) throw new TypeError('Expected a Node');
    return adopt(node.cloneNode(deep), this);
  });
  for (const name of ['appendChild', 'insertBefore']) {
    const original = Node.prototype[name];
    member(Node.prototype, name, function (node, before = null) {
      if (this instanceof Document) {
        if (!isDOMNode(node)) throw new TypeError('Expected a Node');
        const added = isDOMFragment(node) ? Array.from(node.childNodes) : [node];
        const children = Array.from(this.childNodes).filter((child) => !added.includes(child));
        const index = before == null ? children.length : children.indexOf(before);
        if (before != null && before !== node && index < 0)
          throw platformDOMException('Reference node is not a child', 'NotFoundError');
        children.splice(Math.max(0, index), 0, ...added);
        if (
          children.some((child) => ![1, 7, 8, 10].includes(child.nodeType)) ||
          children.filter((child) => child.nodeType === 1).length > 1 ||
          children.filter((child) => child.nodeType === 10).length > 1 ||
          (children.findIndex((child) => child.nodeType === 10) >
            children.findIndex((child) => child.nodeType === 1) &&
            children.some((child) => child.nodeType === 1))
        )
          throw platformDOMException('Invalid document children', 'HierarchyRequestError');
      }
      return name === 'appendChild' ? original.call(this, node) : original.call(this, node, before);
    });
  }
  const collections = new WeakMap();
  const documentCollection = (doc, name, read) => {
    validDocument(doc);
    let values = collections.get(doc);
    if (!values) {
      values = new Map();
      collections.set(doc, values);
    }
    if (!values.has(name)) values.set(name, htmlCollection(read));
    return values.get(name);
  };
  accessor(Document.prototype, 'children', function () {
    return documentCollection(this, 'children', () =>
      Array.from(this.childNodes)
        .filter((node) => node.nodeType === 1)
        .map((node) => elementSlot(node).nodeId),
    );
  });
  accessor(Document.prototype, 'childElementCount', function () {
    validDocument(this);
    return this.children.length;
  });
  for (const name of ['firstElementChild', 'lastElementChild'])
    accessor(Document.prototype, name, function () {
      validDocument(this);
      const nodes = this.children;
      return nodes.item(name === 'firstElementChild' ? 0 : nodes.length - 1);
    });
  for (const [name, selector] of Object.entries({
    links: 'a[href],area[href]',
    anchors: 'a[name]',
    forms: 'form',
    images: 'img',
    embeds: 'embed',
    scripts: 'script',
    applets: null,
  }))
    accessor(Document.prototype, name, function () {
      return documentCollection(this, name, () =>
        selector
          ? compatibilitySelectors
              .query(this, selector)
              .filter((node) => node.namespaceURI === 'http://www.w3.org/1999/xhtml')
              .map((node) => elementSlot(node).nodeId)
          : [],
      );
    });
  accessor(Document.prototype, 'plugins', function () {
    validDocument(this);
    return this.embeds;
  });
  const disconnectedOrder = new WeakMap();
  let nextDisconnectedOrder = 0;
  const order = (node) => {
    if (!disconnectedOrder.has(node)) disconnectedOrder.set(node, ++nextDisconnectedOrder);
    return disconnectedOrder.get(node);
  };
  const attr = (node) => typeof Attr === 'function' && node instanceof Attr;
  const path = (node) => {
    const nodes = [node];
    let parent = attr(node) ? node.ownerElement : node.parentNode;
    for (; parent; parent = parent.parentNode) nodes.push(parent);
    return nodes;
  };
  member(Node.prototype, 'compareDocumentPosition', function (other) {
    if (!isDOMNode(this) || !isDOMNode(other)) throw new TypeError('Expected a Node');
    if (this === other) return 0;
    const a = path(this),
      b = path(other),
      ar = a[a.length - 1],
      br = b[b.length - 1];
    if (ar !== br) return 1 | 32 | (order(ar) < order(br) ? 4 : 2);
    if (b.includes(this)) return 4 | 16;
    if (a.includes(other)) return 2 | 8;
    let i = a.length - 1,
      j = b.length - 1;
    while (i >= 0 && j >= 0 && a[i] === b[j]) {
      i--;
      j--;
    }
    const left = a[i],
      right = b[j],
      parent = a[i + 1];
    if (attr(left) && attr(right)) {
      const names = parent.getAttributeNames();
      return 32 | (names.indexOf(left.name) < names.indexOf(right.name) ? 4 : 2);
    }
    if (attr(left)) return 4;
    if (attr(right)) return 2;
    return Array.from(parent.childNodes).indexOf(left) <
      Array.from(parent.childNodes).indexOf(right)
      ? 4
      : 2;
  });

  /* shared_document_state */
  const parserSlots = new WeakSet();
  class DOMParser {
    constructor() {
      parserSlots.add(this);
    }
    parseFromString(input, type) {
      if (!parserSlots.has(this)) throw new TypeError('Illegal invocation');
      if (arguments.length < 2) throw new TypeError('Not enough arguments');
      input = trustedConvert(
        input,
        'TrustedHTML',
        'DOMParser parseFromString',
        "Failed to execute 'parseFromString' on 'DOMParser': ",
      );
      type = bindingString(type);
      if (
        ![
          'text/html',
          'text/xml',
          'application/xml',
          'application/xhtml+xml',
          'image/svg+xml',
        ].includes(type)
      )
        throw new TypeError('Invalid supported type');
      return wrap(host.parseInertDocument(input, type));
    }
  }
  Object.defineProperty(DOMParser.prototype, Symbol.toStringTag, {
    value: 'DOMParser',
    configurable: true,
  });
  markNative(DOMParser, 'DOMParser');
  markNative(DOMParser.prototype.parseFromString, 'parseFromString');
  Object.defineProperty(DOMParser.prototype, 'parseFromString', { enumerable: true });
  Object.defineProperty(globalThis, 'DOMParser', {
    value: DOMParser,
    writable: true,
    configurable: true,
  });
  const serializerSlots = new WeakSet(),
    xmlText = (value) =>
      String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
    xmlAttribute = (value) =>
      xmlText(value)
        .replace(/"/g, '&quot;')
        .replace(/\t/g, '&#9;')
        .replace(/\n/g, '&#10;')
        .replace(/\r/g, '&#13;');
  const serializeXML = (node, inherited = new Map()) => {
    switch (node.nodeType) {
      case 1: {
        const namespaces = new Map(inherited),
          declarations = [],
          attributes = [];
        for (const name of node.getAttributeNames()) {
          const attr = node.getAttributeNode(name),
            value = attr.value;
          if (name === 'xmlns') namespaces.set('', value);
          else if (name.startsWith('xmlns:')) namespaces.set(name.slice(6), value);
          attributes.push([name, value]);
        }
        const prefix = node.prefix || '',
          namespace = node.namespaceURI;
        if (namespace && namespaces.get(prefix) !== namespace) {
          const name = prefix ? 'xmlns:' + prefix : 'xmlns';
          if (!attributes.some(([candidate]) => candidate === name))
            declarations.push([name, namespace]);
          namespaces.set(prefix, namespace);
        } else if (!namespace && !prefix && namespaces.get('')) {
          if (!attributes.some(([candidate]) => candidate === 'xmlns'))
            declarations.push(['xmlns', '']);
          namespaces.set('', '');
        }
        let generated = 0;
        for (const pair of attributes) {
          if (pair[0] === 'xmlns' || pair[0].startsWith('xmlns:')) continue;
          const attr = node.getAttributeNode(pair[0]);
          if (
            !attr.namespaceURI ||
            attr.prefix ||
            attr.namespaceURI === 'http://www.w3.org/XML/1998/namespace'
          )
            continue;
          let attrPrefix = [...namespaces].find(([, value]) => value === attr.namespaceURI)?.[0];
          if (!attrPrefix) {
            do attrPrefix = 'ns' + ++generated;
            while (namespaces.has(attrPrefix));
            namespaces.set(attrPrefix, attr.namespaceURI);
            declarations.push(['xmlns:' + attrPrefix, attr.namespaceURI]);
          }
          pair[0] = attrPrefix + ':' + (attr.localName || attr.name);
        }
        const name = node.nodeName,
          serializedAttributes = [...declarations, ...attributes]
            .map(([key, value]) => ' ' + key + '="' + xmlAttribute(value) + '"')
            .join(''),
          children = Array.from(node.childNodes, (child) => serializeXML(child, namespaces)).join(
            '',
          );
        return '<' + name + serializedAttributes + '>' + children + '</' + name + '>';
      }
      case 3:
        return xmlText(node.data);
      case 4:
        return '<![CDATA[' + node.data.replace(/]]>/g, ']]]]><![CDATA[>') + ']]>';
      case 7:
        return '<?' + node.target + (node.data ? ' ' + node.data : '') + '?>';
      case 8:
        return '<!--' + node.data + '-->';
      case 9:
      case 11:
        return Array.from(node.childNodes, (child) => serializeXML(child, inherited)).join('');
      case 10:
        return (
          '<!DOCTYPE ' +
          node.name +
          (node.publicId ? ' PUBLIC "' + node.publicId + '"' : node.systemId ? ' SYSTEM' : '') +
          (node.systemId ? ' "' + node.systemId + '"' : '') +
          '>'
        );
      default:
        return '';
    }
  };
  class XMLSerializer {
    constructor() {
      serializerSlots.add(this);
    }
    serializeToString(root) {
      if (!serializerSlots.has(this)) throw new TypeError('Illegal invocation');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      if (!isDOMNode(root))
        throw new TypeError(
          "Failed to execute 'serializeToString' on 'XMLSerializer': parameter 1 is not of type 'Node'.",
        );
      return serializeXML(root);
    }
  }
  Object.defineProperty(XMLSerializer.prototype, Symbol.toStringTag, {
    value: 'XMLSerializer',
    configurable: true,
  });
  markNative(XMLSerializer, 'XMLSerializer');
  markNative(XMLSerializer.prototype.serializeToString, 'serializeToString');
  Object.defineProperty(XMLSerializer.prototype, 'serializeToString', { enumerable: true });
  Object.defineProperty(globalThis, 'XMLSerializer', {
    value: XMLSerializer,
    writable: true,
    configurable: true,
  });
}

for (const name of ['hasStorageAccess', 'hasUnpartitionedCookieAccess'])
  if (name in Document.prototype) {
    const method = function () {
      if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
      if (this !== document)
        return platformPromiseReject(
          platformDOMException('Document is not fully active', 'InvalidStateError'),
        );
      return platformPromiseResolve(host.hasStorageAccess());
    };
    Object.defineProperty(method, 'name', { value: name, configurable: true });
    if (typeof markNative === 'function') markNative(method, name);
    Object.defineProperty(Document.prototype, name, {
      value: method,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }

// Document's unforgeable Location attribute is installed on each instance;
// Node specializations remain on Node.prototype and dispatch by private brand.
function documentLocation(value) {
  if (value === document) return host.documentActive() ? loc : null;
  const reference = referenceGet(value);
  if (reference?.binding?.kind === 'Document')
    return callRealmBinding(value, reference, 'location', []);
  if (elementSlot(value)?.type === 'document') return null;
  throw new TypeError('Illegal invocation');
}
function documentFirstChild(value) {
  const reference = referenceGet(value);
  if (reference?.binding?.kind === 'Document')
    return callRealmBinding(value, reference, 'firstChild', []);
  return wrap(
    host.firstChild(value === document ? host.documentRootID() : elementSlot(value).nodeId),
  );
}

// Capture installed getters only after all semantic layers have been installed.
// Borrowing an accessor must dispatch through the Document owner, rather than
// mistake a foreign active document for a local inert document. Public property
// and prototype replacements must not change a previously borrowed accessor.
function finalizeDocumentGetterBindings() {
  // Frozen Document WebIDL marks only these three attributes LegacyLenientThis.
  // Their getter returns undefined for a receiver without the Document brand.
  const lenientThis = new Set(['onreadystatechange', 'onmouseenter', 'onmouseleave']);
  const lenientSetter = new Set(['fullscreen', 'fullscreenElement', 'fullscreenEnabled']);
  const stringSetters = new Set([
    'xmlVersion',
    'domain',
    'cookie',
    'title',
    'dir',
    'designMode',
    'fgColor',
    'linkColor',
    'vlinkColor',
    'alinkColor',
    'bgColor',
  ]);
  const nullToEmpty = new Set(['fgColor', 'linkColor', 'vlinkColor', 'alinkColor', 'bgColor']);
  const documentReceiver = (value) =>
    value === document ||
    elementSlot(value)?.type === 'document' ||
    referenceGet(value)?.binding?.kind === 'Document';
  const getters = new Map();
  for (const name of Reflect.ownKeys(Document.prototype)) {
    const descriptor = Object.getOwnPropertyDescriptor(Document.prototype, name);
    if (!descriptor.get || !descriptor.configurable) continue;
    const original = descriptor.get;
    getters.set(name, original);
    descriptor.get = function () {
      const reference = referenceGet(this);
      if (reference?.binding?.kind === 'Document')
        return callRealmBinding(this, reference, 'get', [name]);
      if (this !== document && elementSlot(this)?.type !== 'document') {
        if (lenientThis.has(name)) return undefined;
        throw new TypeError('Illegal invocation');
      }
      return functionSourceApply(original, this, []);
    };
    // LegacyLenientSetter is an actual no-op setter, not an absent setter.
    // All setters check the receiver before touching or converting the value.
    const originalSetter =
      descriptor.set || (lenientSetter.has(name) ? function (value) {} : undefined);
    if (originalSetter)
      descriptor.set = function (value) {
        if (!documentReceiver(this)) {
          if (lenientThis.has(name)) return;
          throw new TypeError('Illegal invocation');
        }
        // Perform WebIDL string coercion in the binding realm, including Symbol
        // rejection; preserve nullable and LegacyNullToEmptyString inputs.
        if (stringSetters.has(name))
          value =
            name === 'xmlVersion' && value == null
              ? null
              : bindingString(value === null && nullToEmpty.has(name) ? '' : value);
        return functionSourceApply(originalSetter, this, [value]);
      };
    Object.defineProperty(Document.prototype, name, descriptor);
  }
  registerDocumentGetterBinding = (value) =>
    registerRealmBinding(value, 'Document', {
      get: (name) => functionSourceApply(getters.get(name), value, []),
      location: () => documentLocation(value),
      firstChild: () => documentFirstChild(value),
    });
  registerDocumentGetterBinding(document);
  for (const value of documentWrappers.values())
    if (!referenceGet(value)) registerDocumentGetterBinding(value);
}
