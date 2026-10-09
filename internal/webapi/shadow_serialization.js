// Shadow attachment state lives in the realm, while every materialized child
// remains a canonical DOM node. Export only IDs and immutable attachment data.
{
  const serializeShadow = (root) => {
    const children = Array.from(root.childNodes);
    return children.length
      ? host.serializeNodeList(children.map((n) => elementSlot(n).nodeId))
      : fragmentState(root).html || '';
  };
  Object.defineProperty(ShadowRoot.prototype, 'innerHTML', {
    configurable: true,
    enumerable: true,
    get() {
      if (!shadowSlots.has(this)) throw new TypeError('Illegal invocation');
      return serializeShadow(this);
    },
    set(value) {
      if (!shadowSlots.has(this)) throw new TypeError('Illegal invocation');
      // Fragment parsing uses the shadow host's context, not template insertion
      // mode. Create the parser context directly to avoid invoking a custom
      // element constructor for a temporary, unobservable parsing container.
      const owner = shadowSlots.get(this).host;
      const parsed = wrap(host.createNS(owner.namespaceURI, owner.localName));
      host.setInnerHTML(
        elementSlot(parsed).nodeId,
        trustedConvert(
          value === null ? '' : value,
          'TrustedHTML',
          'ShadowRoot innerHTML',
          "Failed to set the 'innerHTML' property on 'ShadowRoot': ",
          owner,
        ),
      );
      const children = Array.from(parsed.childNodes);
      for (const child of children) parsed.removeChild(child);
      this.replaceChildren(...children);
      fragmentState(this).html = '';
    },
  });
  registerBootstrapCallback(
    'registerShadowSnapshot',
    () => {
      const result = [];
      const documentStyles = constructedStyleSheets.snapshot(document);
      if (documentStyles.length)
        result.push({ hostID: host.documentRootID(), styles: documentStyles });
      // Only native attachments belong in declarative shadow templates. Public
      // polyfill wrappers describe logical light DOM, not a native attachment;
      // promoting them to real roots changes CSS scope and slot composition.
      // Their already-composed children remain in the canonical DOM projection.
      for (const node of shadowHosts) {
        const root = elementShadows.get(node);
        if (!root) continue;
        const state = shadowSlots.get(root),
          children = Array.from(root.childNodes);
        result.push({
          hostID: elementSlot(node).nodeId,
          rootID: 2147483648 + elementSlot(node).nodeId,
          mode: state.mode,
          delegatesFocus: state.delegatesFocus,
          children: children.map((n) => elementSlot(n).nodeId),
          html: children.length ? '' : fragmentState(root)?.html || '',
          styles: constructedStyleSheets.snapshot(root),
        });
      }
      return JSON.stringify(result);
    },
    (encoded) => {
      for (const state of JSON.parse(encoded) || []) {
        if (!state.mode) continue;
        const node = wrap(state.hostID);
        if (!(node instanceof Element)) continue;
        let root = elementShadows.get(node);
        if (!root) {
          root = new ShadowRoot(hostToken, node, state.mode, {
            delegatesFocus: !!state.delegatesFocus,
          });
          setElementShadow(node, root);
        }
        const fragment = fragmentState(root);
        for (const child of fragment.children) deleteSyntheticParent(child);
        fragment.children = Array.from(state.children || [], (id) => wrap(id)).filter(Boolean);
        fragment.html = state.html || '';
        for (const child of fragment.children) setSyntheticParent(child, root);
      }
    },
    () => String(shadowSnapshotRevision),
  );
}
