// This code belongs to the viewer, never to a Mimic realm. Mirrored frames
// allow parent DOM access but still forbid ALL scripts through their sandbox.
const createPreviewMirror = ({ passive = false } = {}) => {
  const viewerDocument = document;
  const keyAttribute = 'data-mimic-preview-node';
  const emptyDocument =
    "<!doctype html><html><head><meta http-equiv=\"Content-Security-Policy\" content=\"script-src 'none'; object-src 'none'; connect-src 'none'; form-action 'none'\"></head><body></body></html>";
  const frames = new WeakMap(),
    shadows = new WeakMap();
  const key = (node) => (node.nodeType === 1 ? node.getAttribute(keyAttribute) : null);
  const compatible = (a, b) =>
    a &&
    a.nodeType === b.nodeType &&
    a.nodeName === b.nodeName &&
    a.namespaceURI === b.namespaceURI;
  const isFrame = (node) => node.nodeType === 1 && node.localName === 'iframe';

  function update(frame, markup) {
    let state = frames.get(frame);
    if (!state) {
      state = { pending: markup, applied: null, ready: false };
      frames.set(frame, state);
      frame.setAttribute('sandbox', 'allow-same-origin'); // Deliberately NO allow-scripts.
      frame.addEventListener('load', () => {
        state.ready = true;
        state.applied = null;
        if (state.pending !== null) apply(frame, state);
      });
      // The one bootstrap navigation is independent of the mirrored HTML.
      frame.srcdoc = emptyDocument;
      return;
    }
    state.pending = markup;
    if (state.ready) apply(frame, state);
  }

  function apply(frame, state) {
    const markup = state.pending;
    if (markup === state.applied) return;
    const doc = frame.contentDocument;
    if (!doc) return;
    const viewerFocus = passive ? viewerDocument.activeElement : null;
    const next = new DOMParser().parseFromString(
      markup || '<html><head></head><body></body></html>',
      'text/html',
    );
    const nodes = new Map(),
      scroll = [];
    nodes.removals = [];
    nodes.dialogs = [];
    nodes.projectedScroll = [];
    function collect(node) {
      const id = key(node);
      if (id) nodes.set(id, node);
      if (node.nodeType === 1) {
        if (node.scrollTop || node.scrollLeft) scroll.push([node, node.scrollLeft, node.scrollTop]);
        const shadow = shadows.get(node);
        if (shadow) collect(shadow);
      }
      for (const child of node.childNodes) collect(child);
    }
    collect(doc.documentElement);
    const oldRoot = key(doc.documentElement),
      newRoot = key(next.documentElement);
    const navigating = oldRoot && oldRoot !== newRoot;
    const x = frame.contentWindow.scrollX,
      y = frame.contentWindow.scrollY;
    patch(doc.documentElement, next.documentElement, nodes);
    // Delay removals until moves across parents have found their old nodes.
    for (const [parent, node] of nodes.removals) if (node.parentNode === parent) node.remove();
    // `open` is not the modal/top-layer state. Restore it through the viewer's
    // native dialog API only after the keyed tree (including shadows) is attached.
    const modalDialogs = nodes.dialogs
      .filter((node) => node.hasAttribute('data-mimic-preview-modal'))
      .sort(
        (a, b) =>
          Number(a.getAttribute('data-mimic-preview-modal')) -
          Number(b.getAttribute('data-mimic-preview-modal')),
      );
    for (const node of nodes.dialogs)
      if (!node.hasAttribute('data-mimic-preview-modal') && node.matches(':modal')) {
        const open = node.open;
        if (!open) node.setAttribute('open', '');
        node.close();
        if (open) node.setAttribute('open', '');
      }
    const naturalOrder = (state.modalOrder || [])
      .filter((node) => modalDialogs.includes(node) && node.matches(':modal'))
      .concat(modalDialogs.filter((node) => !node.matches(':modal')));
    if (naturalOrder.some((node, index) => node !== modalDialogs[index]))
      for (const node of modalDialogs)
        if (node.matches(':modal')) {
          const open = node.open;
          if (!open) node.setAttribute('open', '');
          node.close();
          if (open) node.setAttribute('open', '');
        }
    for (const node of modalDialogs)
      if (node.isConnected && !node.matches(':modal')) {
        const open = node.open;
        node.removeAttribute('open');
        node.showModal();
        if (!open) node.removeAttribute('open');
      }
    state.modalOrder = modalDialogs;
    // Native showModal focuses its contents even inside an inert iframe. A
    // passive viewer keeps keyboard ownership outside every mirrored realm.
    if (viewerFocus?.isConnected && viewerDocument.activeElement !== viewerFocus)
      viewerFocus.focus({ preventScroll: true });
    if (navigating) frame.contentWindow.scrollTo({ left: 0, top: 0, behavior: 'instant' });
    else {
      for (const [node, left, top] of scroll)
        if (node.isConnected) {
          if (node.scrollLeft !== left) node.scrollLeft = left;
          if (node.scrollTop !== top) node.scrollTop = top;
        }
      if (frame.contentWindow.scrollX !== x || frame.contentWindow.scrollY !== y)
        frame.contentWindow.scrollTo({ left: x, top: y, behavior: 'instant' });
    }
    // Only a changed canonical offset overrides the viewer's own scrolling.
    for (const [node, value, windowScroll] of nodes.projectedScroll) {
      const [left, top] = (value || '0,0').split(',').map(Number);
      if (Number.isFinite(left) && Number.isFinite(top)) {
        if (windowScroll)
          node.ownerDocument.defaultView.scrollTo({ left, top, behavior: 'instant' });
        else node.scrollTo({ left, top, behavior: 'instant' });
      }
    }
    state.applied = markup;
  }

  function patch(node, next, nodes) {
    if (node.nodeType !== 1) {
      if (node.nodeValue !== next.nodeValue) node.nodeValue = next.nodeValue;
      return;
    }
    const frame = isFrame(node),
      textarea = node.localName === 'textarea';
    const valueChanged = textarea
      ? node.textContent !== next.textContent
      : node.getAttribute('value') !== next.getAttribute('value');
    const checkedChanged = node.hasAttribute('checked') !== next.hasAttribute('checked');
    const selectedChanged = node.hasAttribute('selected') !== next.hasAttribute('selected');
    for (const name of ['data-mimic-preview-scroll', 'data-mimic-preview-window-scroll'])
      if (node.getAttribute(name) !== next.getAttribute(name))
        nodes.projectedScroll.push([node, next.getAttribute(name), name.endsWith('window-scroll')]);
    for (const attr of Array.from(node.attributes)) {
      if (frame && (attr.name === 'srcdoc' || attr.name === 'sandbox')) continue;
      if (!next.hasAttributeNS(attr.namespaceURI, attr.localName))
        node.removeAttributeNS(attr.namespaceURI, attr.localName);
    }
    for (const attr of next.attributes) {
      if (/^on/i.test(attr.name) || (frame && ['src', 'srcdoc', 'sandbox'].includes(attr.name)))
        continue;
      if (node.getAttributeNS(attr.namespaceURI, attr.localName) !== attr.value)
        node.setAttributeNS(attr.namespaceURI, attr.name, attr.value);
    }
    if (frame) {
      update(node, next.getAttribute('srcdoc') || '');
      return;
    }
    if (node.localName === 'dialog') nodes.dialogs.push(node);
    if (node.localName === 'template') children(node.content, next.content, nodes);
    else children(node, next, nodes);
    if (valueChanged && (textarea || node.localName === 'input') && node.type !== 'file')
      node.value = textarea ? next.textContent : next.getAttribute('value') || '';
    if (checkedChanged && node.localName === 'input') node.checked = next.hasAttribute('checked');
    if (selectedChanged && node.localName === 'option')
      node.selected = next.hasAttribute('selected');
  }

  function children(parent, next, nodes) {
    const wanted = [],
      kept = new Set();
    for (const source of Array.from(next.childNodes)) {
      if (source.nodeType === 1 && ['script', 'object', 'embed'].includes(source.localName))
        continue;
      if (
        source.nodeType === 1 &&
        source.localName === 'template' &&
        source.hasAttribute('shadowrootmode')
      ) {
        let root = shadows.get(parent);
        if (!root) {
          root = parent.attachShadow({
            mode: 'open',
            delegatesFocus: source.hasAttribute('shadowrootdelegatesfocus'),
          });
          shadows.set(parent, root);
        }
        children(root, source.content, nodes);
        continue;
      }
      wanted.push(source);
    }
    let cursor = parent.firstChild;
    for (const source of wanted) {
      const id = key(source);
      let node = id ? nodes.get(id) : cursor;
      if (!compatible(node, source) || (!id && key(node)) || kept.has(node)) node = null;
      if (!node && !id)
        node = Array.from(parent.childNodes).find(
          (n) => !kept.has(n) && !key(n) && compatible(n, source),
        );
      if (!node) {
        node =
          source.nodeType === 1
            ? parent.ownerDocument.createElementNS(
                source.namespaceURI,
                source.prefix ? source.prefix + ':' + source.localName : source.localName,
              )
            : source.cloneNode(false);
        // Avoid fetching/navigating an imported iframe before it is sandboxed.
        if (isFrame(node)) node.setAttribute('sandbox', 'allow-same-origin');
      }
      if (node !== cursor) {
        if (parent.moveBefore && node.isConnected && parent.isConnected)
          parent.moveBefore(node, cursor);
        else parent.insertBefore(node, cursor);
      }
      patch(node, source, nodes);
      kept.add(node);
      cursor = node.nextSibling;
    }
    for (const node of Array.from(parent.childNodes))
      if (!kept.has(node)) nodes.removals.push([parent, node]);
  }
  return { update };
};
