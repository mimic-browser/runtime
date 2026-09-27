(() => {
  const out = {}, check = (name, a, b) => out[name] = a.isEqualNode(b);
  const a = document.createElement('div'), b = document.createElement('div');
  check('self', a, a); check('empty', a, b); check('null', a, null); check('undefined', a, undefined);
  a.setAttribute('x', '1'); a.setAttribute('y', '2'); b.setAttribute('y', '2'); b.setAttribute('x', '1');
  check('attributeOrder', a, b); b.setAttribute('x', '3'); check('attributeValue', a, b);
  b.setAttribute('x', '1'); a.append(document.createTextNode('same')); b.append(document.createTextNode('same'));
  check('children', a, b); b.firstChild.data = 'other'; check('childValue', a, b);
  check('surrogate', document.createTextNode('\ud800'), document.createTextNode('\ud801'));
  check('surrogateSame', document.createTextNode('\ud800'), document.createTextNode('\ud800'));
  check('textComment', document.createTextNode('x'), document.createComment('x'));
  const p = document.createElementNS('urn:e', 'p:x'), q = document.createElementNS('urn:e', 'q:x');
  check('elementPrefix', p, q);
  p.setAttributeNS('urn:a', 'p:a', 'v'); q.setAttributeNS('urn:a', 'q:a', 'v');
  const r = document.createElementNS('urn:e', 'p:x'); r.setAttributeNS('urn:a', 'q:a', 'v');
  check('attributePrefix', p, r); check('attrPrefix', p.getAttributeNodeNS('urn:a', 'a'), r.getAttributeNodeNS('urn:a', 'a'));
  check('doctype', new DOMParser().parseFromString('<!doctype html PUBLIC "a" "b">', 'text/html').doctype, new DOMParser().parseFromString('<!doctype html PUBLIC "a" "c">', 'text/html').doctype);
  const t = document.createElement('template'), u = document.createElement('template'); t.innerHTML = '<b>x</b>'; u.innerHTML = '<i>y</i>';
  check('templateContentExcluded', t, u); check('templateFragments', t.content, u.content);
  const s = document.createElement('div'), z = document.createElement('div'); s.attachShadow({mode:'open'}).textContent = 'x';
  check('shadowExcluded', s, z); check('shadowFragment', s.shadowRoot, document.createDocumentFragment());
  const poisoned = document.createElement('div'); Object.defineProperty(poisoned, 'nodeType', {get(){throw Error('public getter')}});
  check('privateState', poisoned, document.createElement('div'));
  for (const [name, fn] of Object.entries({missing:()=>a.isEqualNode(), badArgument:()=>a.isEqualNode({}), badReceiver:()=>Node.prototype.isEqualNode.call({}, a)})) {
    try { fn(); out[name] = 'no-error'; } catch(e) { out[name] = e.name; }
  }
  const text = "\\\"\n\t\ud800\ud801\ud83d\ude00";
  const created = document.createTextNode(text), mutated = document.createTextNode(''); mutated.data = text;
  check('creationMutationCodeUnits', created, mutated);
  out.characterCodeUnits = Array.from({length: created.data.length}, (_, i) => created.data.charCodeAt(i));
  const ca = document.createComment(''), cb = document.createComment(''); ca.data = '\ud800'; cb.data = '\ud801';
  check('commentCodeUnits', ca, cb);
  check('differentOwners', new DOMParser().parseFromString('<p>x</p>', 'text/html').body, new DOMParser().parseFromString('<p>x</p>', 'text/html').body);
  check('emptyShadowFragment', document.createElement('div').attachShadow({mode:'open'}), document.createDocumentFragment());
  return JSON.stringify(out);
})()
