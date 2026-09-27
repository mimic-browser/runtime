// Per-document state; frame projections read these same accessors through the
// owning realm instead of maintaining a parallel set of defaults.
const documentState = new WeakMap();
const stateForDocument = (value) => {
  validDocument(value);
  let s = documentState.get(value);
  if (!s) {
    s = {
      designMode: 'off',
      xmlVersion: contentType(value) === 'text/html' ? null : '1.0',
      xmlStandalone: false,
    };
    documentState.set(value, s);
  }
  return s;
};
for (const key of ['xmlEncoding', 'xmlVersion', 'xmlStandalone'])
  accessor(
    Document.prototype,
    key,
    function () {
      const s = stateForDocument(this);
      return key === 'xmlEncoding' ? null : s[key];
    },
    key === 'xmlEncoding'
      ? undefined
      : function (value) {
          const s = stateForDocument(this);
          if (key === 'xmlStandalone') {
            s[key] = !!value;
            return;
          }
          value = String(value);
          if (value !== '1.0')
            throw platformDOMException('Only XML version 1.0 is supported', 'NotSupportedError');
          s[key] = value;
        },
  );
accessor(
  Document.prototype,
  'designMode',
  function () {
    return stateForDocument(this).designMode;
  },
  function (value) {
    const s = stateForDocument(this);
    value = String(value).toLowerCase();
    if (value === 'on' || value === 'off') s.designMode = value;
  },
);
for (const [name, attribute] of [
  ['fgColor', 'text'],
  ['linkColor', 'link'],
  ['vlinkColor', 'vlink'],
  ['alinkColor', 'alink'],
  ['bgColor', 'bgcolor'],
])
  accessor(
    Document.prototype,
    name,
    function () {
      validDocument(this);
      return this.body?.getAttribute(attribute) || '';
    },
    function (value) {
      validDocument(this);
      if (this.body) this.body.setAttribute(attribute, value === null ? '' : String(value));
    },
  );
accessor(Document.prototype, 'scrollingElement', function () {
  validDocument(this);
  return this.compatMode === 'BackCompat' ? this.body : this.documentElement;
});
accessor(Document.prototype, 'rootElement', function () {
  validDocument(this);
  const root = this.documentElement;
  return root?.namespaceURI === 'http://www.w3.org/2000/svg' && root.localName === 'svg'
    ? root
    : null;
});
accessor(Document.prototype, 'customElementRegistry', function () {
  validDocument(this);
  return this === document ? globalThis.customElements : null;
});
for (const [key, type] of [
  ['fragmentDirective', 'FragmentDirective'],
  ['timeline', 'DocumentTimeline'],
  ['modelContext', 'ModelContext'],
])
  accessor(Document.prototype, key, function () {
    const s = stateForDocument(this);
    if (!s[key]) s[key] = Object.create(globalThis[type].prototype);
    return s[key];
  });
for (const key of [
  'fullscreenElement',
  'pictureInPictureElement',
  'pointerLockElement',
  'activeViewTransition',
])
  accessor(Document.prototype, key, function () {
    return stateForDocument(this)[key] || null;
  });
for (const key of ['webkitFullscreenElement', 'webkitCurrentFullScreenElement'])
  accessor(Document.prototype, key, function () {
    return this.fullscreenElement;
  });
for (const key of ['fullscreen', 'webkitIsFullScreen'])
  accessor(Document.prototype, key, function () {
    return this.fullscreenElement !== null;
  });
for (const key of ['fullscreenEnabled', 'webkitFullscreenEnabled', 'pictureInPictureEnabled'])
  accessor(Document.prototype, key, function () {
    validDocument(this);
    return (
      this === document &&
      documentPolicy.allowsFeature(
        key === 'pictureInPictureEnabled' ? 'picture-in-picture' : 'fullscreen',
      )
    );
  });
accessor(Document.prototype, 'lastModified', function () {
  validDocument(this);
  const timestamp = this === document ? host.documentLastModified() : 0,
    d = new Date(timestamp || Date.now()),
    pad = (v) => String(v).padStart(2, '0');
  return (
    pad(d.getMonth() + 1) +
    '/' +
    pad(d.getDate()) +
    '/' +
    d.getFullYear() +
    ' ' +
    pad(d.getHours()) +
    ':' +
    pad(d.getMinutes()) +
    ':' +
    pad(d.getSeconds())
  );
});
