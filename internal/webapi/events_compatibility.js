// A single dispatch path serves script-created events and host lifecycle
// events. Listener records belong to their EventTarget, never a global Page.
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
    markNative(get, name, 'get ');
    markNative(set, name, 'set ');
    Object.defineProperty(prototype, name, { get, set, enumerable: true, configurable: true });
  };
  const stateOf = (event) => {
    const state = eventSlots.get(event);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const optionCapture = (options) => (typeof options === 'boolean' ? options : !!options?.capture);
  let contentHandlerWorld = host.isIsolatedInputWorld();
  bootstrapRestoreHooks.push(() => {
    contentHandlerWorld = host.isIsolatedInputWorld();
    // Weak collections are part of the serialized bootstrap graph, but their
    // entries describe the creator context's Window. A restored realm starts
    // with a fresh Window listener state just like an ordinary new document.
    eventListeners = new WeakMap();
    eventHandlerListeners = new WeakMap();
    eventHandlerWrappers = new WeakMap();
  });
  const ensureContentHandler = (target, type) => {
    if (contentHandlerWorld || !(target instanceof HTMLElement)) return;
    const source = target.getAttribute('on' + type),
      state = eventHandlerRecord(target, type);
    if (state.attributeRead && state.attributeSource === source) return;
    state.attributeRead = true;
    state.attributeSource = source;
    if (source === null) {
      if (state.attributeOwned) {
        state.attributeOwned = false;
        setEventHandlerValue(target, type, null);
      }
      return;
    }
    state.attributeOwned = true;
    let callback = null;
    if (host.allowContentEventHandler(source)) {
      try {
        callback = host.compileContentHandler(
          'function ' +
            ('on' + type).replace(/[^A-Za-z0-9_$]/g, '_') +
            '(event){with(document){with(this.form||{}){with(this){' +
            source +
            '\n}}}}',
        );
      } catch (error) {
        try {
          console.error(error);
        } catch {}
      }
    }
    setEventHandlerValue(target, type, callback);
  };
  member(EventTarget.prototype, 'addEventListener', function (type, callback, options) {
    const target = this == null ? window : this;
    type = String(type);
    if (callback == null) return;
    if (typeof callback !== 'function' && typeof callback !== 'object')
      throw new TypeError('Invalid event listener');
    const capture = optionCapture(options),
      signal = typeof options === 'object' ? options?.signal : null;
    if (signal?.aborted) return;
    ensureContentHandler(target, type);
    const listeners = listenersFor(target),
      list = listeners.get(type) || [];
    if (
      list.some(
        (record) => record.callback === callback && record.capture === capture && !record.removed,
      )
    )
      return;
    const record = {
      callback,
      capture,
      once: !!options?.once,
      passive: !!options?.passive,
      removed: false,
    };
    list.push(record);
    listeners.set(type, list);
    if (type === 'change') compatibilityElementState.mediaListenerChanged(target);
    if (signal)
      signal.addEventListener(
        'abort',
        () => {
          record.removed = true;
          const index = list.indexOf(record);
          if (index >= 0) list.splice(index, 1);
          if (type === 'change') compatibilityElementState.mediaListenerChanged(target);
        },
        { once: true },
      );
  });
  member(EventTarget.prototype, 'removeEventListener', function (type, callback, options) {
    const target = this == null ? window : this;
    type = String(type);
    const list = listenersFor(target).get(type) || [],
      capture = optionCapture(options),
      index = list.findIndex(
        (record) => record.callback === callback && record.capture === capture,
      );
    if (index >= 0) {
      list[index].removed = true;
      list.splice(index, 1);
    }
    if (type === 'change') compatibilityElementState.mediaListenerChanged(target);
  });
  // Handler properties occupy their first assigned position in the same
  // listener list. Replacing a callback keeps order; clearing removes it.
  for (const C of [Document, HTMLElement]) {
    const prototype = C.prototype;
    // Blink installs these runtime-enabled handlers after the static members.
    for (const name of C === Document
      ? ['onpointerrawupdate', 'onscrollsnapchange', 'onscrollsnapchanging']
      : ['onscrollsnapchange', 'onscrollsnapchanging', 'onpointerrawupdate']) {
      const descriptor = Object.getOwnPropertyDescriptor(prototype, name);
      if (descriptor) {
        delete prototype[name];
        Object.defineProperty(prototype, name, descriptor);
      }
    }
    for (const name of Object.getOwnPropertyNames(prototype).filter((name) => /^on/.test(name))) {
      const type = name.slice(2),
        state = (target) => {
          if (!(target instanceof C)) throw new TypeError('Illegal invocation');
          return eventHandlerRecord(target, type);
        };
      const get = function () {
          if (name === 'onreadystatechange' && !(this instanceof C)) return undefined;
          ensureContentHandler(this, type);
          return state(this).value;
        },
        set = function (value) {
          if (name === 'onreadystatechange' && !(this instanceof C)) return;
          const record = state(this);
          if (this instanceof HTMLElement) {
            record.attributeRead = true;
            record.attributeSource = this.getAttribute(name);
            record.attributeOwned = false;
          }
          setEventHandlerValue(this, type, value);
        };
      Object.defineProperty(get, 'name', { value: 'get ' + name, configurable: true });
      Object.defineProperty(set, 'name', { value: 'set ' + name, configurable: true });
      accessor(prototype, name, get, set);
    }
  }
  // Window's handler attributes are own properties in the frozen exposure.
  // Use the same listener records as Document/HTMLElement, so replacement,
  // primitive conversion and dispatch order do not depend on publication stubs.
  for (const name of Object.getOwnPropertyNames(window).filter((name) => /^on/.test(name))) {
    const descriptor = Object.getOwnPropertyDescriptor(window, name);
    if (!descriptor?.get || !descriptor.set || !descriptor.configurable) continue;
    const type = name.slice(2),
      target = (receiver) => {
        if (receiver == null || receiver === window) return window;
        const reference = referenceGet(receiver);
        if (reference?.type === 'window') {
          bridgeAccess(reference.frame);
          return receiver;
        }
        throw new TypeError('Illegal invocation');
      };
    const get = function () {
      const owner = target(this);
      return owner === window ? eventHandlerRecord(window, type).value : owner[name];
    };
    const set = function (value) {
      const owner = target(this);
      if (owner === window) setEventHandlerValue(window, type, value);
      else owner[name] = value;
    };
    Object.defineProperty(get, 'name', { value: 'get ' + name, configurable: true });
    Object.defineProperty(set, 'name', { value: 'set ' + name, configurable: true });
    accessor(window, name, get, set);
  }
  for (const [name, key, fallback] of [
    ['target', 'target', null],
    ['srcElement', 'target', null],
    ['currentTarget', 'currentTarget', null],
    ['eventPhase', 'phase', 0],
    ['composed', 'composed', false],
  ])
    accessor(Event.prototype, name, function () {
      return stateOf(this)[key] ?? fallback;
    });
  accessor(
    Event.prototype,
    'cancelBubble',
    function () {
      return !!stateOf(this).stopped;
    },
    function (value) {
      if (value) stateOf(this).stopped = true;
    },
  );
  accessor(
    Event.prototype,
    'returnValue',
    function () {
      return !stateOf(this).defaultPrevented;
    },
    function (value) {
      if (!value) this.preventDefault();
    },
  );
  member(Event.prototype, 'stopPropagation', function () {
    stateOf(this).stopped = true;
  });
  member(Event.prototype, 'stopImmediatePropagation', function () {
    const state = stateOf(this);
    state.stopped = true;
    state.immediate = true;
  });
  member(Event.prototype, 'preventDefault', function () {
    const state = stateOf(this);
    if (state.cancelable && !state.passive) state.defaultPrevented = true;
  });
  const insideRoot = (node, root) => {
    for (
      let current = node;
      isDOMNode(current);
      current =
        canonicalDOMParent(current) ||
        (current instanceof ShadowRoot ? shadowSlots.get(current)?.host || current.host : null)
    )
      if (current === root) return true;
    return false;
  };
  member(Event.prototype, 'composedPath', function () {
    const state = stateOf(this);
    return (state.path || []).filter((node) => {
      for (let current = node; isDOMNode(current); ) {
        const root = current.getRootNode();
        if (!(root instanceof ShadowRoot)) break;
        if (root.mode === 'closed' && !insideRoot(state.currentTarget, root)) return false;
        current = root.host;
      }
      return true;
    });
  });
  const retarget = (target, current) => {
    let node = target;
    while (isDOMNode(node)) {
      const root = node.getRootNode();
      if (!(root instanceof ShadowRoot) || (isDOMNode(current) && current.getRootNode() === root))
        return node;
      node = root.host;
    }
    return node;
  };
  let currentWindowEvent,
    eventCallbackDepth = 0;
  const eventGet = function () {
      return currentWindowEvent;
    },
    eventSet = function (value) {
      Object.defineProperty(this, 'event', {
        value,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    };
  accessor(window, 'event', eventGet, eventSet);
  const invokeOwnedEvent = (callback, receiver, event, inShadow, native, errorHandler = false) => {
    const previous = currentWindowEvent,
      checkpoint = native && eventCallbackDepth === 0;
    if (!inShadow) currentWindowEvent = event;
    eventCallbackDepth++;
    try {
      if (typeof callback === 'function')
        return errorHandler
          ? callback.call(
              receiver,
              event.message,
              event.filename,
              event.lineno,
              event.colno,
              event.error,
            )
          : callback.call(receiver, event);
      const handle = callback.handleEvent;
      if (typeof handle === 'function') return handle.call(callback, event);
    } finally {
      eventCallbackDepth--;
      try {
        if (checkpoint) host.eventCallbackCheckpoint();
      } finally {
        currentWindowEvent = previous;
      }
    }
  };
  registerBootstrapCallback(
    'installEventInvoker',
    (callback, receiver, event, inShadow, native, errorHandler) => {
      const outcome = (threw, value) => ({
        threw,
        value,
        valueType: typeof value,
        symbol: typeof value === 'symbol' ? value : undefined,
      });
      try {
        return outcome(
          false,
          invokeOwnedEvent(callback, receiver, event, inShadow, native, errorHandler),
        );
      } catch (value) {
        return outcome(true, value);
      }
    },
  );
  const invokeEventCallback = (callback, receiver, event, inShadow, native) => {
    const handler = eventHandlerWrappers.get(callback);
    const errorHandler =
      !!handler && receiver === window && event.type === 'error' && errorEventSlots.has(event);
    if (handler) {
      if (typeof handler.value !== 'function') return;
      callback = handler.value;
    }
    const reference = referenceGet(callback);
    let value;
    if (reference && (reference.type === 'function' || reference.type === 'object')) {
      const outcome = host.frameCall(
        reference.frame,
        reference.handle,
        [encodeCrossRealmArgument(event)],
        encodeCrossRealmArgument(receiver),
        reference.realm,
        false,
        true,
        inShadow,
        native && eventCallbackDepth === 0,
        errorHandler,
      );
      value = unwrapCrossRealm(reference.frame, outcome.value);
      if (outcome.threw) throw value;
    } else value = invokeOwnedEvent(callback, receiver, event, inShadow, native, errorHandler);
    if (handler && (errorHandler ? value === true : value === false)) event.preventDefault();
    return value;
  };
  dispatchEventCore = (target, event, trusted, native = false) => {
    const state = stateOf(event);
    if (state.dispatching || !state.type)
      throw platformDOMException(
        'Event is already being dispatched or uninitialized',
        'InvalidStateError',
      );
    state.trusted = !!trusted;
    state.dispatching = true;
    state.stopped = false;
    state.immediate = false;
    const path = [target];
    let current = target;
    while (isDOMNode(current)) {
      let parent = canonicalDOMParent(current);
      if (!parent && current instanceof ShadowRoot && state.composed) parent = current.host;
      if (!parent && current instanceof Document && state.type !== 'load')
        parent = current.defaultView;
      if (!parent) break;
      path.push(parent);
      current = parent;
    }
    if (!isDOMNode(target))
      while (eventParents.has(current)) {
        current = eventParents.get(current);
        path.push(current);
      }
    state.path = path;
    const report = (error) => {
      state.listenerException = true;
      reportWindowException(error);
    };
    const invoke = (current, capture, phase) => {
      state.currentTarget = current;
      state.target = retarget(target, current);
      state.phase = phase;
      ensureContentHandler(current, state.type);
      const list = listenersFor(current).get(state.type) || [];
      for (const record of list.slice()) {
        if (state.immediate) break;
        if (typeof record === 'function') {
          if (capture) continue;
          try {
            invokeEventCallback(
              record,
              current,
              event,
              isDOMNode(current) && current.getRootNode() instanceof ShadowRoot,
              native,
            );
          } catch (error) {
            report(error);
          }
          continue;
        }
        if (record.removed || record.capture !== capture) continue;
        if (record.once) {
          record.removed = true;
          const index = list.indexOf(record);
          if (index >= 0) list.splice(index, 1);
        }
        state.passive = record.passive;
        try {
          if (typeof record.callback === 'function')
            invokeEventCallback(
              record.callback,
              current,
              event,
              isDOMNode(current) && current.getRootNode() instanceof ShadowRoot,
              native,
            );
          else
            invokeEventCallback(
              record.callback,
              record.callback,
              event,
              isDOMNode(current) && current.getRootNode() instanceof ShadowRoot,
              native,
            );
        } catch (error) {
          report(error);
        }
        state.passive = false;
      }
      if (!capture && !state.immediate) {
        const handler = internalEventHandler(current, state.type);
        if (typeof handler === 'function')
          try {
            if (
              invokeEventCallback(
                handler,
                current,
                event,
                isDOMNode(current) && current.getRootNode() instanceof ShadowRoot,
                native,
              ) === false
            )
              event.preventDefault();
          } catch (error) {
            report(error);
          }
      }
    };
    try {
      for (let i = path.length - 1; i > 0 && !state.stopped; i--)
        invoke(path[i], true, retarget(target, path[i]) === path[i] ? 2 : 1);
      if (!state.stopped) {
        invoke(target, true, 2);
        if (!state.immediate) invoke(target, false, 2);
      }
      for (let i = 1; i < path.length && !state.stopped; i++) {
        const atTarget = retarget(target, path[i]) === path[i];
        if (state.bubbles || atTarget) invoke(path[i], false, atTarget ? 2 : 3);
      }
    } finally {
      state.currentTarget = null;
      state.phase = 0;
      state.path = [];
      state.target = retarget(target, path[path.length - 1]);
      state.dispatching = false;
      state.passive = false;
      state.stopped = false;
      state.immediate = false;
    }
    return !state.defaultPrevented;
  };
  member(EventTarget.prototype, 'dispatchEvent', function (event) {
    const target = this == null ? window : this;
    const reference = referenceGet(target);
    if (reference?.nodeId && host.nodeOwnerDocument(reference.nodeId) !== realmDocumentRootID) {
      const owner = target.ownerDocument || target;
      return owner.defaultView.EventTarget.prototype.dispatchEvent.call(target, event);
    }
    const allowed = dispatchEventCore(target, event, false);
    const slot = elementSlot(target);
    if (slot?.nodeId) {
      const state = stateOf(event);
      const kind =
        event instanceof PointerEvent
          ? 'PointerEvent'
          : event instanceof MouseEvent
            ? 'MouseEvent'
            : event instanceof InputEvent
              ? 'InputEvent'
              : 'Event';
      const others = host.broadcastInputEvent(
        slot.nodeId,
        JSON.stringify({
          kind,
          type: state.type,
          init: {
            bubbles: state.bubbles,
            cancelable: state.cancelable,
            composed: state.composed,
          },
          canceled: !allowed,
          trusted: false,
          native: false,
        }),
      );
      if (!others) event.preventDefault();
    }
    return !stateOf(event).defaultPrevented;
  });
  const uiSlots = new WeakMap(),
    mouseSlots = new WeakMap(),
    pointerSlots = new WeakMap();
  class UIEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      uiSlots.set(this, { view: init.view ?? null, detail: Number(init.detail) || 0, which: 0 });
    }
  }
  for (const name of ['view', 'detail', 'which'])
    accessor(UIEvent.prototype, name, function () {
      return uiSlots.get(this)?.[name];
    });
  const initializeUIEvent = (event, type, bubbles, cancelable, view, detail) => {
    if (!uiSlots.has(event)) throw new TypeError('Illegal invocation');
    if (stateOf(event).dispatching) return false;
    Event.prototype.initEvent.call(event, type, bubbles, cancelable);
    uiSlots.set(event, { view, detail: Number(detail) >> 0, which: 0 });
    return true;
  };
  member(
    UIEvent.prototype,
    'initUIEvent',
    function (type, bubbles = false, cancelable = false, view = null, detail = 0) {
      initializeUIEvent(this, type, bubbles, cancelable, view, detail);
    },
  );
  class MouseEvent extends UIEvent {
    constructor(type, init = {}) {
      super(type, init);
      const data = {};
      for (const name of [
        'screenX',
        'screenY',
        'clientX',
        'clientY',
        'button',
        'buttons',
        'movementX',
        'movementY',
      ])
        data[name] = Number(init[name]) || 0;
      for (const name of ['ctrlKey', 'shiftKey', 'altKey', 'metaKey']) data[name] = !!init[name];
      data.relatedTarget = init.relatedTarget ?? null;
      mouseSlots.set(this, data);
    }
  }
  member(
    MouseEvent.prototype,
    'initMouseEvent',
    function (
      type,
      bubbles = false,
      cancelable = false,
      view = null,
      detail = 0,
      screenX = 0,
      screenY = 0,
      clientX = 0,
      clientY = 0,
      ctrlKey = false,
      altKey = false,
      shiftKey = false,
      metaKey = false,
      button = 0,
      relatedTarget = null,
    ) {
      if (!mouseSlots.has(this)) throw new TypeError('Illegal invocation');
      if (!initializeUIEvent(this, type, bubbles, cancelable, view, detail)) return;
      mouseSlots.set(this, {
        screenX: Number(screenX) >> 0,
        screenY: Number(screenY) >> 0,
        clientX: Number(clientX) >> 0,
        clientY: Number(clientY) >> 0,
        ctrlKey: !!ctrlKey,
        altKey: !!altKey,
        shiftKey: !!shiftKey,
        metaKey: !!metaKey,
        button: (Number(button) << 16) >> 16,
        buttons: 0,
        movementX: 0,
        movementY: 0,
        relatedTarget,
      });
    },
  );
  for (const name of [
    'screenX',
    'screenY',
    'clientX',
    'clientY',
    'button',
    'buttons',
    'movementX',
    'movementY',
    'ctrlKey',
    'shiftKey',
    'altKey',
    'metaKey',
    'relatedTarget',
  ])
    accessor(MouseEvent.prototype, name, function () {
      return mouseSlots.get(this)?.[name];
    });
  accessor(MouseEvent.prototype, 'which', function () {
    return this.button + 1;
  });
  for (const [name, source] of [
    ['x', 'clientX'],
    ['y', 'clientY'],
    ['offsetX', 'clientX'],
    ['offsetY', 'clientY'],
  ])
    accessor(MouseEvent.prototype, name, function () {
      return this[source];
    });
  accessor(MouseEvent.prototype, 'pageX', function () {
    return this.clientX + window.scrollX;
  });
  accessor(MouseEvent.prototype, 'pageY', function () {
    return this.clientY + window.scrollY;
  });
  member(MouseEvent.prototype, 'getModifierState', function (key) {
    return !!mouseSlots.get(this)?.[
      { Alt: 'altKey', Control: 'ctrlKey', Meta: 'metaKey', Shift: 'shiftKey' }[String(key)]
    ];
  });
  class PointerEvent extends MouseEvent {
    constructor(type, init = {}) {
      super(type, init);
      const data = {};
      for (const [name, fallback] of [
        ['pointerId', 0],
        ['width', 1],
        ['height', 1],
        ['pressure', 0],
        ['tangentialPressure', 0],
        ['tiltX', 0],
        ['tiltY', 0],
        ['twist', 0],
        ['altitudeAngle', Math.PI / 2],
        ['azimuthAngle', 0],
        ['persistentDeviceId', 0],
      ])
        data[name] = init[name] === undefined ? fallback : Number(init[name]);
      data.pointerType = String(init.pointerType || '');
      data.isPrimary = !!init.isPrimary;
      pointerSlots.set(this, data);
    }
  }
  for (const name of [
    'pointerId',
    'width',
    'height',
    'pressure',
    'tangentialPressure',
    'tiltX',
    'tiltY',
    'twist',
    'altitudeAngle',
    'azimuthAngle',
    'persistentDeviceId',
    'pointerType',
    'isPrimary',
  ])
    accessor(PointerEvent.prototype, name, function () {
      return pointerSlots.get(this)?.[name];
    });
  for (const name of ['getCoalescedEvents', 'getPredictedEvents'])
    member(PointerEvent.prototype, name, function () {
      return [];
    });
  for (const [name, value] of Object.entries({ UIEvent, MouseEvent, PointerEvent })) {
    markNative(value, name);
    Object.defineProperty(globalThis, name, { value, writable: true, configurable: true });
    Object.defineProperty(value.prototype, Symbol.toStringTag, { value: name, configurable: true });
  }
  const clicking = new WeakSet();
  member(HTMLElement.prototype, 'click', function () {
    if (!(this instanceof HTMLElement)) throw new TypeError('Illegal invocation');
    if (
      clicking.has(this) ||
      (['BUTTON', 'INPUT', 'SELECT', 'TEXTAREA', 'OPTION'].includes(this.tagName) && this.disabled)
    )
      return;
    clicking.add(this);
    try {
      const event = new PointerEvent('click', {
        bubbles: true,
        cancelable: true,
        composed: true,
        view: this.ownerDocument?.defaultView ?? null,
        pointerId: -1,
        pointerType: '',
        isPrimary: false,
      });
      const checkable = this.tagName === 'INPUT' && ['checkbox', 'radio'].includes(this.type),
        previous = checkable ? this.checked : null;
      if (checkable) this.checked = this.type === 'radio' ? true : !previous;
      if (!dispatchEventCore(this, event, false)) {
        if (checkable) this.checked = previous;
        return;
      }
      if (checkable) {
        dispatchEventCore(this, new Event('input', { bubbles: true, composed: true }), true);
        dispatchEventCore(this, new Event('change', { bubbles: true }), true);
      }
      if (this.tagName === 'A' && this.hasAttribute('href')) host.navigate(this.href);
    } finally {
      clicking.delete(this);
    }
  });
}
