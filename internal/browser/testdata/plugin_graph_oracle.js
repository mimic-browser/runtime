Object.assign(
  {},
  (() => {
    const p = navigator.plugins,
      m = navigator.mimeTypes
    const describe = (o) => ({
      tag: Object.prototype.toString.call(o),
      keys: Object.keys(o),
      own: Reflect.ownKeys(o).map(String),
      descriptors: Object.fromEntries(
        Object.entries(Object.getOwnPropertyDescriptors(o)).map(([k, d]) => [
          k,
          {
            writable: d.writable,
            enumerable: d.enumerable,
            configurable: d.configurable,
            valueTag: Object.prototype.toString.call(d.value),
          },
        ]),
      ),
      json: JSON.stringify(o),
    })
    const attempt = (f) => {
      try {
        return f()
      } catch (e) {
        return e.name
      }
    }
    return {
      collections: [describe(p), describe(m)],
      plugins: Array.from(p, describe),
      mimes: Array.from(m, describe),
      identity: Array.from(p, (x) =>
        Array.from(x, (y) => [y === m.namedItem(y.type), y.enabledPlugin === p[0]]),
      ),
      lookup: [
        p.item(0) === p[0],
        p.namedItem(p[0].name) === p[0],
        m.item(0) === m[0],
        m.namedItem(m[0].type) === m[0],
        p.item(4294967296) === p[0],
        p.item(-1),
        attempt(() => p.item()),
        attempt(() => p.namedItem()),
      ],
      brands: [
        attempt(() => Plugin.prototype.item.call({}, 0)),
        attempt(() => Object.getOwnPropertyDescriptor(MimeType.prototype, 'type').get.call({})),
      ],
      prototypes: [Plugin, PluginArray, MimeType, MimeTypeArray].map((c) => ({
        name: c.name,
        keys: Reflect.ownKeys(c.prototype).map(String),
        iterator: c.prototype[Symbol.iterator] === Array.prototype.values,
      })),
    }
  })(),
)
