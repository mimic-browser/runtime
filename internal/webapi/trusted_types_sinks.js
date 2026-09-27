// Binding entry points check brands before string conversion and before any
// mutation. Internal parser/clone/node algorithms do not become extra sinks.
const trustedAttributeValue = (
  element,
  name,
  value,
  ns = '',
  prefix = "Failed to execute 'setAttribute' on 'Element': ",
) => {
  const slot = elementSlot(element);
  if (!slot || slot.type !== 'element') throw new TypeError('Illegal invocation');
  const info = trustedAttributeInfo(slot.tagName, name, slot.namespaceURI, ns);
  return info ? trustedConvert(value, info[0], info[1], prefix, element) : bindingString(value);
};
const trustedRequireElement = (element, iface) => {
  const slot = elementSlot(element),
    tag = {
      HTMLScriptElement: 'SCRIPT',
      HTMLIFrameElement: 'IFRAME',
      HTMLObjectElement: 'OBJECT',
      HTMLEmbedElement: 'EMBED',
    }[iface];
  if (
    !slot ||
    slot.type !== 'element' ||
    slot.namespaceURI !== 'http://www.w3.org/1999/xhtml' ||
    slot.tagName !== tag
  )
    throw new TypeError('Illegal invocation');
  return slot;
};
const trustedSetAttribute = Element.prototype.setAttribute;
const trustedTextContentSetter = Object.getOwnPropertyDescriptor(Node.prototype, 'textContent').set;
const trustedSetScriptSource = (element, value, property) => {
  const slot = trustedRequireElement(element, 'HTMLScriptElement');
  if (property !== 'text' && value == null) value = '';
  const text = trustedConvert(
    value,
    'TrustedScript',
    'HTMLScriptElement ' + property,
    "Failed to set the '" + property + "' property on 'HTMLScriptElement': ",
    element,
  );
  host.markTrustedScriptText(slot.nodeId, text);
  trustedApply(trustedTextContentSetter, element, [text]);
};
const trustedSetAttributeProperty = (element, name, value, kind, iface) => {
  trustedRequireElement(element, iface);
  const text = trustedConvert(
    value,
    kind,
    iface + ' ' + name,
    "Failed to set the '" + name + "' property on '" + iface + "': ",
    element,
  );
  trustedApply(trustedSetAttribute, element, [name, trustedValue(trustedConstructors[kind], text)]);
};
const trustedDocumentWrite = (receiver, args, name) => {
  const allTrusted =
    args.length > 0 && args.every((value) => trustedTypeOf(value) === 'TrustedHTML');
  const text = args
    .map((value) =>
      trustedTypeOf(value) === 'TrustedHTML'
        ? trustedSource(value, 'TrustedHTML')
        : bindingString(value),
    )
    .join('');
  return allTrusted
    ? text
    : trustedConvert(
        text,
        'TrustedHTML',
        'Document ' + name,
        "Failed to execute '" + name + "' on 'Document': ",
        receiver,
      );
};
{
  const method = (proto, name, fn) => {
    Object.defineProperty(fn, 'name', { value: name, configurable: true });
    markNative(fn, name);
    Object.defineProperty(proto, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const htmlSetter = Object.getOwnPropertyDescriptor(Element.prototype, 'innerHTML').set;
  method(Element.prototype, 'setHTMLUnsafe', function (value) {
    if (!elementSlot(this)) throw new TypeError('Illegal invocation');
    if (!arguments.length) throw new TypeError('Not enough arguments');
    const text = trustedConvert(
      value,
      'TrustedHTML',
      'Element setHTMLUnsafe',
      "Failed to execute 'setHTMLUnsafe' on 'Element': ",
      this,
    );
    htmlSetter.call(this, trustedValue(TrustedHTML, text));
  });
  const shadowSetter = Object.getOwnPropertyDescriptor(ShadowRoot.prototype, 'innerHTML').set;
  method(ShadowRoot.prototype, 'setHTMLUnsafe', function (value) {
    if (!shadowSlots.has(this)) throw new TypeError('Illegal invocation');
    if (!arguments.length) throw new TypeError('Not enough arguments');
    const text = trustedConvert(
      value,
      'TrustedHTML',
      'ShadowRoot setHTMLUnsafe',
      "Failed to execute 'setHTMLUnsafe' on 'ShadowRoot': ",
      shadowSlots.get(this).host,
    );
    shadowSetter.call(this, trustedValue(TrustedHTML, text));
  });
  method(Document, 'parseHTMLUnsafe', function (value) {
    if (!arguments.length) throw new TypeError('Not enough arguments');
    return wrap(
      host.parseInertDocument(
        trustedConvert(
          value,
          'TrustedHTML',
          'Document parseHTMLUnsafe',
          "Failed to execute 'parseHTMLUnsafe' on 'Document': ",
        ),
        'text/html',
      ),
    );
  });
  for (const [iface, property] of [
    ['HTMLObjectElement', 'data'],
    ['HTMLObjectElement', 'codeBase'],
    ['HTMLEmbedElement', 'src'],
  ]) {
    const proto = globalThis[iface]?.prototype;
    if (!proto) continue;
    htmlElementInterfaces[iface === 'HTMLObjectElement' ? 'OBJECT' : 'EMBED'] = iface;
    Object.defineProperty(proto, property, {
      get() {
        const raw = this.getAttribute(property.toLowerCase());
        return raw === null ? '' : host.urlParts(raw).href;
      },
      set(value) {
        trustedSetAttributeProperty(this, property, value, 'TrustedScriptURL', iface);
      },
      enumerable: true,
      configurable: true,
    });
  }
  // Contextual fragment parsing uses the existing canonical fragment parser.
  // Range geometry is intentionally coarse (Mimic has no inline fragment
  // renderer), but it must share the element layout model. Accessibility
  // clients use a Range around text nodes to determine their visibility.
  if (globalThis.Range) {
    const rangeOwners = new WeakMap();
    const rangeState = (receiver) => {
      const state = rangeOwners.get(receiver);
      if (!state) throw new TypeError('Illegal invocation');
      return state;
    };
    const rangeAccessor = (proto, name, get) => {
      if (!proto) return;
      Object.defineProperty(proto, name, { get, enumerable: true, configurable: true });
    };
    const boundaryLength = (node) =>
      node?.nodeType === 3 ? (node.textContent || '').length : node?.childNodes?.length || 0;
    const checkedBoundary = (state, node, offset) => {
      if (!(node instanceof Node) || (node.ownerDocument !== state.owner && node !== state.owner))
        throw platformDOMException('The node is not in this document.', 'WrongDocumentError');
      const value = Number(offset) >>> 0;
      if (value > boundaryLength(node))
        throw platformDOMException('The offset is larger than the node length.', 'IndexSizeError');
      return value;
    };
    const commonAncestor = (a, b) => {
      const ancestors = [];
      for (let node = a; node; node = node.parentNode) ancestors.push(node);
      for (let node = b; node; node = node.parentNode) if (ancestors.includes(node)) return node;
      return a;
    };
    const rangeTarget = (node) => (node instanceof Element ? node : node?.parentElement);
    const rangeRect = (state) => {
      const targets = [];
      for (const node of state.nodes) {
        const target = rangeTarget(node);
        if (target && !targets.includes(target)) targets.push(target);
      }
      if (!targets.length) return new DOMRect(0, 0, 0, 0);
      let left = Infinity,
        top = Infinity,
        right = -Infinity,
        bottom = -Infinity;
      for (const target of targets) {
        const rect = Element.prototype.getBoundingClientRect.call(target);
        left = Math.min(left, rect.left);
        top = Math.min(top, rect.top);
        right = Math.max(right, rect.right);
        bottom = Math.max(bottom, rect.bottom);
      }
      return Number.isFinite(left)
        ? new DOMRect(left, top, Math.max(0, right - left), Math.max(0, bottom - top))
        : new DOMRect(0, 0, 0, 0);
    };
    const childUnder = (ancestor, node) => {
      while (node?.parentNode && node.parentNode !== ancestor) node = node.parentNode;
      return node;
    };
    const comparePoints = (aNode, aOffset, bNode, bOffset) => {
      if (aNode === bNode) return Math.sign(aOffset - bOffset);
      if (aNode.contains?.(bNode)) {
        const child = childUnder(aNode, bNode),
          index = Array.from(aNode.childNodes).indexOf(child);
        return aOffset <= index ? -1 : 1;
      }
      if (bNode.contains?.(aNode)) {
        const child = childUnder(bNode, aNode),
          index = Array.from(bNode.childNodes).indexOf(child);
        return index < bOffset ? -1 : 1;
      }
      const ancestor = commonAncestor(aNode, bNode),
        a = childUnder(ancestor, aNode),
        b = childUnder(ancestor, bNode),
        children = Array.from(ancestor.childNodes);
      return children.indexOf(a) < children.indexOf(b) ? -1 : 1;
    };
    const textNodes = (root) => {
      const result = [];
      if (root?.nodeType === 3) return [root];
      for (const child of Array.from(root?.childNodes || [])) result.push(...textNodes(child));
      return result;
    };
    const selectedText = (s) => {
      if (s.collapsed) return '';
      if (s.startContainer === s.endContainer && s.startContainer.nodeType === 3)
        return s.startContainer.data.slice(s.startOffset, s.endOffset);
      const root = commonAncestor(s.startContainer, s.endContainer);
      let result = '';
      for (const node of textNodes(root)) {
        const length = node.data.length;
        if (
          comparePoints(node, length, s.startContainer, s.startOffset) <= 0 ||
          comparePoints(node, 0, s.endContainer, s.endOffset) >= 0
        )
          continue;
        const from = node === s.startContainer ? s.startOffset : 0,
          to = node === s.endContainer ? s.endOffset : length;
        result += node.data.slice(from, to);
      }
      return result;
    };
    const nodeSelected = (s, node) => {
      const parent = node.parentNode;
      if (!parent) return false;
      const index = Array.from(parent.childNodes).indexOf(node);
      return (
        comparePoints(s.startContainer, s.startOffset, parent, index) <= 0 &&
        comparePoints(parent, index + 1, s.endContainer, s.endOffset) <= 0
      );
    };
    const overlaps = (s, node) => {
      const parent = node.parentNode;
      if (!parent) return node === s.startContainer || node === s.endContainer;
      const index = Array.from(parent.childNodes).indexOf(node);
      return (
        comparePoints(parent, index + 1, s.startContainer, s.startOffset) > 0 &&
        comparePoints(parent, index, s.endContainer, s.endOffset) < 0
      );
    };
    const cloneSelected = (s, source, target) => {
      for (const child of Array.from(source.childNodes || [])) {
        if (!overlaps(s, child)) continue;
        if (nodeSelected(s, child)) target.appendChild(child.cloneNode(true));
        else if (child.nodeType === 3) {
          const from = child === s.startContainer ? s.startOffset : 0,
            to = child === s.endContainer ? s.endOffset : child.data.length;
          if (to > from) target.appendChild(s.owner.createTextNode(child.data.slice(from, to)));
        } else {
          const copy = child.cloneNode(false);
          cloneSelected(s, child, copy);
          if (copy.childNodes.length) target.appendChild(copy);
        }
      }
    };
    const contents = (s) => {
      const fragment = s.owner.createDocumentFragment();
      if (s.collapsed) return fragment;
      if (s.startContainer === s.endContainer && s.startContainer.nodeType === 3) {
        fragment.appendChild(
          s.owner.createTextNode(s.startContainer.data.slice(s.startOffset, s.endOffset)),
        );
        return fragment;
      }
      const root = commonAncestor(s.startContainer, s.endContainer);
      cloneSelected(s, root, fragment);
      return fragment;
    };
    const refresh = (s) => {
      s.collapsed = s.startContainer === s.endContainer && s.startOffset === s.endOffset;
      const root = commonAncestor(s.startContainer, s.endContainer);
      s.nodes = s.collapsed ? [] : textNodes(root).filter((node) => overlaps(s, node));
    };
    const deleteSelected = (s) => {
      if (s.collapsed) return;
      if (s.startContainer === s.endContainer && s.startContainer.nodeType === 3) {
        s.startContainer.deleteData(s.startOffset, s.endOffset - s.startOffset);
        s.endOffset = s.startOffset;
        refresh(s);
        return;
      }
      const root = commonAncestor(s.startContainer, s.endContainer),
        removals = [],
        edits = [];
      const visit = (node) => {
        for (const child of Array.from(node.childNodes || [])) {
          if (!overlaps(s, child)) continue;
          if (nodeSelected(s, child)) removals.push(child);
          else if (child.nodeType === 3) {
            const from = child === s.startContainer ? s.startOffset : 0,
              to = child === s.endContainer ? s.endOffset : child.data.length;
            if (to > from) edits.push([child, from, to - from]);
          } else visit(child);
        }
      };
      visit(root);
      for (const [node, offset, count] of edits) node.deleteData(offset, count);
      for (const node of removals) if (node.parentNode) node.parentNode.removeChild(node);
      s.endContainer = s.startContainer;
      s.endOffset = s.startOffset;
      refresh(s);
    };
    method(Document.prototype, 'createRange', function () {
      if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
      const range = Object.create(Range.prototype);
      rangeOwners.set(range, {
        owner: this,
        nodes: [],
        startContainer: this,
        startOffset: 0,
        endContainer: this,
        endOffset: 0,
        collapsed: true,
      });
      return range;
    });
    method(Range.prototype, 'selectNode', function (node) {
      const state = rangeState(this);
      if (!arguments.length) throw new TypeError('Not enough arguments');
      if (!(node instanceof Node) || node.ownerDocument !== state.owner)
        throw platformDOMException('The node is not in this document.', 'WrongDocumentError');
      const parent = node.parentNode;
      if (!parent) throw platformDOMException('The node has no parent.', 'InvalidNodeTypeError');
      const index = Array.from(parent.childNodes).indexOf(node);
      state.nodes = [node];
      state.startContainer = parent;
      state.startOffset = Math.max(0, index);
      state.endContainer = parent;
      state.endOffset = Math.max(0, index) + 1;
      refresh(state);
    });
    method(Range.prototype, 'selectNodeContents', function (node) {
      const state = rangeState(this);
      if (!arguments.length) throw new TypeError('Not enough arguments');
      if (!(node instanceof Node) || (node.ownerDocument !== state.owner && node !== state.owner))
        throw platformDOMException('The node is not in this document.', 'WrongDocumentError');
      state.nodes =
        node instanceof Document
          ? [node.documentElement].filter(Boolean)
          : Array.from(node.childNodes);
      if (!state.nodes.length) state.nodes = [node];
      state.startContainer = node;
      state.startOffset = 0;
      state.endContainer = node;
      state.endOffset = boundaryLength(node);
      refresh(state);
    });
    method(Range.prototype, 'setStart', function (node, offset) {
      const state = rangeState(this);
      if (arguments.length < 2) throw new TypeError('Not enough arguments');
      offset = checkedBoundary(state, node, offset);
      if (comparePoints(node, offset, state.endContainer, state.endOffset) > 0) {
        state.endContainer = node;
        state.endOffset = offset;
      }
      state.startContainer = node;
      state.startOffset = offset;
      refresh(state);
    });
    method(Range.prototype, 'setEnd', function (node, offset) {
      const state = rangeState(this);
      if (arguments.length < 2) throw new TypeError('Not enough arguments');
      offset = checkedBoundary(state, node, offset);
      if (comparePoints(node, offset, state.startContainer, state.startOffset) < 0) {
        state.startContainer = node;
        state.startOffset = offset;
      }
      state.endContainer = node;
      state.endOffset = offset;
      refresh(state);
    });
    const setAround = (receiver, node, start, after) => {
      const state = rangeState(receiver);
      if (!node?.parentNode)
        throw platformDOMException('The node has no parent.', 'InvalidNodeTypeError');
      const parent = node.parentNode,
        index = Array.from(parent.childNodes).indexOf(node) + (after ? 1 : 0);
      if (start) {
        state.startContainer = parent;
        state.startOffset = index;
      } else {
        state.endContainer = parent;
        state.endOffset = index;
      }
      refresh(state);
    };
    method(Range.prototype, 'setStartBefore', function (node) {
      if (!arguments.length) throw new TypeError('Not enough arguments');
      setAround(this, node, true, false);
    });
    method(Range.prototype, 'setStartAfter', function (node) {
      if (!arguments.length) throw new TypeError('Not enough arguments');
      setAround(this, node, true, true);
    });
    method(Range.prototype, 'setEndBefore', function (node) {
      if (!arguments.length) throw new TypeError('Not enough arguments');
      setAround(this, node, false, false);
    });
    method(Range.prototype, 'setEndAfter', function (node) {
      if (!arguments.length) throw new TypeError('Not enough arguments');
      setAround(this, node, false, true);
    });
    method(Range.prototype, 'collapse', function (toStart = false) {
      const state = rangeState(this);
      if (toStart) {
        state.endContainer = state.startContainer;
        state.endOffset = state.startOffset;
      } else {
        state.startContainer = state.endContainer;
        state.startOffset = state.endOffset;
      }
      state.nodes = [];
      state.collapsed = true;
    });
    method(Range.prototype, 'getBoundingClientRect', function () {
      return rangeRect(rangeState(this));
    });
    method(Range.prototype, 'getClientRects', function () {
      const rect = rangeRect(rangeState(this)),
        values = rect.width || rect.height ? [rect] : [];
      return typeof makeDOMRectList === 'function' ? makeDOMRectList(values) : values;
    });
    method(Range.prototype, 'cloneRange', function () {
      const state = rangeState(this),
        range = Object.create(Range.prototype);
      rangeOwners.set(range, { ...state, nodes: [...state.nodes] });
      return range;
    });
    method(Range.prototype, 'cloneContents', function () {
      return contents(rangeState(this));
    });
    method(Range.prototype, 'extractContents', function () {
      const state = rangeState(this),
        fragment = contents(state);
      deleteSelected(state);
      return fragment;
    });
    method(Range.prototype, 'deleteContents', function () {
      deleteSelected(rangeState(this));
    });
    method(Range.prototype, 'insertNode', function (node) {
      const state = rangeState(this);
      if (!arguments.length) throw new TypeError('Not enough arguments');
      if (!(node instanceof Node)) throw new TypeError('Parameter is not a Node');
      let parent = state.startContainer,
        before;
      if (parent.nodeType === 3) {
        before = parent.splitText(state.startOffset);
        parent = parent.parentNode;
      } else before = parent.childNodes[state.startOffset] || null;
      if (!parent)
        throw platformDOMException('The boundary has no parent.', 'HierarchyRequestError');
      parent.insertBefore(node, before);
      refresh(state);
    });
    method(Range.prototype, 'toString', function () {
      return selectedText(rangeState(this));
    });
    rangeAccessor(globalThis.AbstractRange?.prototype, 'startOffset', function () {
      return rangeState(this).startOffset;
    });
    rangeAccessor(globalThis.AbstractRange?.prototype, 'endOffset', function () {
      return rangeState(this).endOffset;
    });
    rangeAccessor(globalThis.AbstractRange?.prototype, 'collapsed', function () {
      return rangeState(this).collapsed;
    });
    rangeAccessor(globalThis.NodeRange?.prototype, 'startContainer', function () {
      return rangeState(this).startContainer;
    });
    rangeAccessor(globalThis.NodeRange?.prototype, 'endContainer', function () {
      return rangeState(this).endContainer;
    });
    rangeAccessor(Range.prototype, 'commonAncestorContainer', function () {
      const state = rangeState(this);
      return commonAncestor(state.startContainer, state.endContainer);
    });
    method(Range.prototype, 'createContextualFragment', function (value) {
      const owner = rangeState(this).owner;
      if (!arguments.length) throw new TypeError('Not enough arguments');
      const text = trustedConvert(
        value,
        'TrustedHTML',
        'Range createContextualFragment',
        "Failed to execute 'createContextualFragment' on 'Range': ",
        owner,
      );
      const parsed = owner.createElement('template');
      host.setInnerHTML(elementSlot(parsed).nodeId, text);
      return parsed.content;
    });
  }
  // SharedWorker execution remains explicitly unsupported. Its URL binding
  // still performs the required synchronous check before that boundary.
  if (globalThis.SharedWorker) {
    const SharedWorker = class SharedWorker {
      constructor(value) {
        if (!arguments.length) throw new TypeError('Not enough arguments');
        trustedConvert(
          value,
          'TrustedScriptURL',
          'SharedWorker constructor',
          "Failed to construct 'SharedWorker': ",
        );
        throw platformDOMException('SharedWorker execution is unsupported.', 'NotSupportedError');
      }
    };
    markNative(SharedWorker, 'SharedWorker');
    Object.defineProperty(globalThis, 'SharedWorker', {
      value: SharedWorker,
      writable: true,
      configurable: true,
    });
  }
}

const trustedCloneAttribute = (element, name, value, ns) => {
  const info = trustedAttributeInfo(
    element.localName,
    ns ? name.split(':').at(-1) : name,
    element.namespaceURI,
    ns,
  );
  return info ? trustedValue(trustedConstructors[info[0]], value) : value;
};
