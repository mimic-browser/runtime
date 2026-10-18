// Small Window objects whose behavior is independent of a renderer. Other
// unimplemented capability services deliberately retain their explicit boundary.
{
  if (typeof ReportingObserver === 'function') {
    const reportingObservers = new WeakMap();
    class CompatibleReportingObserver {
      constructor(callback, options = {}) {
        if (typeof callback !== 'function') throw new TypeError('Callback must be callable');
        reportingObservers.set(this, {
          callback,
          types: options.types === undefined ? null : Array.from(options.types, String),
          buffered: Boolean(options.buffered),
          observing: false,
          records: [],
        });
      }
      observe() {
        const state = reportingObservers.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        state.observing = true;
      }
      disconnect() {
        const state = reportingObservers.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        state.observing = false;
        state.records.length = 0;
      }
      takeRecords() {
        const state = reportingObservers.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        return state.records.splice(0);
      }
    }
    Object.defineProperty(CompatibleReportingObserver, 'name', { value: 'ReportingObserver' });
    Object.defineProperty(CompatibleReportingObserver.prototype, Symbol.toStringTag, {
      value: 'ReportingObserver',
      configurable: true,
    });
    for (const name of ['observe', 'disconnect', 'takeRecords'])
      markNative(CompatibleReportingObserver.prototype[name], name);
    markNative(CompatibleReportingObserver, 'ReportingObserver');
    Object.defineProperty(globalThis, 'ReportingObserver', {
      value: CompatibleReportingObserver,
      writable: true,
      configurable: true,
    });
  }
  const bars = new WeakSet(),
    externals = new WeakSet(),
    media = new WeakSet();
  const check = (set, value) => {
    if (!set.has(value)) throw new TypeError('Illegal invocation');
  };
  const getter = (prototype, name, get) => {
    markNative(get, name, 'get ');
    Object.defineProperty(prototype, name, { get, enumerable: true, configurable: true });
  };
  const method = (prototype, name, fn) => {
    markNative(fn, name);
    Object.defineProperty(prototype, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const singleton = (name, prototype, brand) => {
    const object = Object.create(prototype);
    brand.add(object);
    replaceableWindow(name, () => object);
    return object;
  };
  const viewportNotifiers = [];
  viewportNotifiers.push((before) => compatibilityElementState.mediaObservationChange(before));
  if (typeof ScreenOrientation === 'function') {
    const orientation = new EventTarget();
    Object.setPrototypeOf(orientation, ScreenOrientation.prototype);
    getter(Screen.prototype, 'orientation', function () {
      if (this !== scr) throw new TypeError('Illegal invocation');
      return orientation;
    });
    getter(ScreenOrientation.prototype, 'type', function () {
      if (this !== orientation) throw new TypeError('Illegal invocation');
      return host.screen().orientationType;
    });
    getter(ScreenOrientation.prototype, 'angle', function () {
      if (this !== orientation) throw new TypeError('Illegal invocation');
      return host.screen().orientationAngle;
    });
    Object.defineProperty(ScreenOrientation.prototype, 'onchange', {
      get() {
        if (this !== orientation) throw new TypeError('Illegal invocation');
        return eventHandlerRecord(orientation, 'change').value;
      },
      set(value) {
        if (this !== orientation) throw new TypeError('Illegal invocation');
        setEventHandlerValue(orientation, 'change', value);
      },
      enumerable: true,
      configurable: true,
    });
    let previousOrientation = null;
    viewportNotifiers.push((before) => {
      if (!(listenersFor(orientation).get('change') || []).length) return;
      const current = host.screen(),
        previous = previousOrientation;
      previousOrientation = current;
      if (
        !before &&
        previous &&
        (current.orientationType !== previous.orientationType ||
          current.orientationAngle !== previous.orientationAngle)
      )
        dispatchNative(orientation, new Event('change'));
    });
  }
  // Runtime-enabled after the static exposure snapshot in Chrome 152.
  // Unknown machine profiles leave this measurement unsupported.
  if (host.navigator().cpuPerformance !== undefined)
    getter(Navigator.prototype, 'cpuPerformance', function () {
      if (this !== navigator) throw new TypeError('Illegal invocation');
      return host.navigator().cpuPerformance;
    });
  if (typeof BarProp === 'function') {
    for (const name of [
      'locationbar',
      'menubar',
      'personalbar',
      'scrollbars',
      'statusbar',
      'toolbar',
    ])
      singleton(name, BarProp.prototype, bars);
    getter(BarProp.prototype, 'visible', function () {
      check(bars, this);
      return true;
    });
  }
  if (typeof External === 'function') {
    singleton('external', External.prototype, externals);
    // These obsolete search-provider methods are native no-ops in Chrome 152.
    method(External.prototype, 'AddSearchProvider', function AddSearchProvider() {
      check(externals, this);
    });
    method(External.prototype, 'IsSearchProviderInstalled', function IsSearchProviderInstalled() {
      check(externals, this);
    });
  }
  if (Object.hasOwn(window, 'styleMedia')) {
    const styleMediaPrototype = Object.create(Object.prototype);
    Object.defineProperty(styleMediaPrototype, Symbol.toStringTag, {
      value: 'StyleMedia',
      configurable: true,
    });
    singleton('styleMedia', styleMediaPrototype, media);
    getter(styleMediaPrototype, 'type', function () {
      check(media, this);
      return 'screen';
    });
    method(styleMediaPrototype, 'matchMedium', function matchMedium(query) {
      check(media, this);
      return cssMediaMatches(query === undefined ? '' : String(query));
    });
  }
  if (Object.hasOwn(window, 'fence'))
    Object.defineProperty(window, 'fence', {
      get: () => null,
      enumerable: true,
      configurable: true,
    });
  if (typeof VisualViewport === 'function') {
    const visual = new EventTarget();
    Object.setPrototypeOf(visual, VisualViewport.prototype);
    replaceableWindow('visualViewport', () => visual);
    const visualMetrics = () => {
      const v = host.viewport(),
        scroll = compatibilityScrolling.position(null);
      return {
        offsetLeft: 0,
        offsetTop: 0,
        pageLeft: scroll.x,
        pageTop: scroll.y,
        width: document.documentElement?.clientWidth ?? v.width,
        height: v.height,
        scale: 1,
      };
    };
    for (const name of [
      'offsetLeft',
      'offsetTop',
      'pageLeft',
      'pageTop',
      'width',
      'height',
      'scale',
    ])
      getter(VisualViewport.prototype, name, function () {
        if (this !== visual) throw new TypeError('Illegal invocation');
        return visualMetrics()[name];
      });
    for (const name of ['onresize', 'onscroll', 'onscrollend'])
      Object.defineProperty(VisualViewport.prototype, name, {
        get() {
          if (this !== visual) throw new TypeError('Illegal invocation');
          return eventHandlerRecord(visual, name.slice(2)).value;
        },
        set(value) {
          if (this !== visual) throw new TypeError('Illegal invocation');
          setEventHandlerValue(visual, name.slice(2), value);
        },
        enumerable: true,
        configurable: true,
      });
    let previousVisual = null;
    viewportNotifiers.push((before) => {
      const observed =
        (listenersFor(visual).get('resize') || []).length ||
        (listenersFor(window).get('resize') || []).length;
      if (!observed) return;
      const current = visualMetrics(),
        previous = previousVisual;
      previousVisual = current;
      if (
        !before &&
        previous &&
        (current.width !== previous.width || current.height !== previous.height)
      ) {
        dispatchNative(window, new Event('resize'));
        dispatchNative(visual, new Event('resize'));
      }
    });
  }
  if (typeof Viewport === 'function') {
    const viewport = Object.create(Viewport.prototype);
    replaceableWindow('viewport', () => viewport);
    getter(Viewport.prototype, 'segments', function () {
      if (this !== viewport) throw new TypeError('Illegal invocation');
      const relations = host.windowRelations();
      if (relations.parent !== relations.self) return null;
      const v = host.viewport();
      return Object.freeze([new DOMRect(0, 0, v.width, v.height)]);
    });
  }
  registerBootstrapCallback('installViewportNotifier', (before) => {
    for (const notify of viewportNotifiers) notify(before);
  });
}
