// An auxiliary top Window uses the canonical cross-realm bridge. No DOM
// iframe or secondary JavaScript-only document stands in for its context.
{
  if (typeof DocumentPictureInPicture === 'function') {
    const pip = Object.create(DocumentPictureInPicture.prototype);
    Object.setPrototypeOf(DocumentPictureInPicture.prototype, EventTarget.prototype);
    Object.defineProperty(window, 'documentPictureInPicture', {
      get: () => pip,
      enumerable: true,
      configurable: true,
    });
    const check = (value) => {
      if (value !== pip) throw new TypeError('Illegal invocation');
    };
    const getWindow = function () {
      check(this);
      const id = host.pictureInPictureState().window;
      return id ? remoteWindow(id) : null;
    };
    markNative(getWindow, 'window', 'get ');
    Object.defineProperty(DocumentPictureInPicture.prototype, 'window', {
      get: getWindow,
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(DocumentPictureInPicture.prototype, 'onenter', {
      get() {
        check(this);
        return eventHandlerRecord(this, 'enter').value;
      },
      set(value) {
        check(this);
        setEventHandlerValue(this, 'enter', value);
      },
      enumerable: true,
      configurable: true,
    });
    const eventWindows = new WeakMap();
    class DocumentPictureInPictureEvent extends Event {
      constructor(type, init) {
        if (arguments.length < 2) throw new TypeError('2 arguments required');
        if (init?.window === undefined) throw new TypeError('window is required');
        const value = init.window;
        originWindowID(value);
        super(type, init);
        eventWindows.set(this, value);
      }
      get window() {
        if (!eventWindows.has(this)) throw new TypeError('Illegal invocation');
        return eventWindows.get(this);
      }
    }
    markNative(DocumentPictureInPictureEvent, 'DocumentPictureInPictureEvent');
    Object.defineProperty(DocumentPictureInPictureEvent.prototype, Symbol.toStringTag, {
      value: 'DocumentPictureInPictureEvent',
      configurable: true,
    });
    Object.defineProperty(globalThis, 'DocumentPictureInPictureEvent', {
      value: DocumentPictureInPictureEvent,
      writable: true,
      configurable: true,
    });
    const requestWindow = function requestWindow(options = {}) {
      try {
        check(this);
        if (options !== null && typeof options !== 'object' && typeof options !== 'function')
          throw new TypeError(
            "The provided value is not of type 'DocumentPictureInPictureOptions'.",
          );
        const o = options ?? {};
        // WebIDL dictionaries read members alphabetically and validate before the
        // algorithm consumes activation or touches an existing PiP context.
        Boolean(o.disallowReturnToOpener);
        const dimension = (name) => {
          const raw = o[name];
          if (raw === undefined) return 0;
          const n = Number(raw);
          if (!Number.isFinite(n) || n < 0 || n >= 2 ** 64)
            throw new TypeError(
              `Failed to execute 'requestWindow' on 'DocumentPictureInPicture': Failed to read the '${name}' property from 'DocumentPictureInPictureOptions': Value is outside the 'unsigned long long' value range.`,
            );
          return Math.trunc(n);
        };
        const height = dimension('height');
        Boolean(o.preferInitialWindowPlacement);
        const width = dimension('width');
        const state = host.pictureInPictureState();
        const fail = (text) =>
          platformDOMException(
            "Failed to execute 'requestWindow' on 'DocumentPictureInPicture': " + text,
            'NotAllowedError',
          );
        if (state.auxiliary) throw fail('Opening a PiP window from a PiP window is not allowed');
        if (!state.top)
          throw fail('Opening a PiP window from a non-top-level document is not allowed');
        if (!state.activation) throw fail('Document PiP requires user activation');
        if (width && !height)
          throw new RangeError(
            "Failed to execute 'requestWindow' on 'DocumentPictureInPicture': Height must be specified if width is specified",
          );
        if (height && !width)
          throw new RangeError(
            "Failed to execute 'requestWindow' on 'DocumentPictureInPicture': Width must be specified if height is specified",
          );
        const id = host.openPictureInPicture(width, height),
          w = remoteWindow(id);
        return new platformPromise((resolve) =>
          host.enqueueWebTask(
            () => {
              dispatchNative(pip, new DocumentPictureInPictureEvent('enter', { window: w }));
              resolve(w);
            },
            1,
            0,
            false,
          ),
        );
      } catch (e) {
        return platformPromiseReject(e);
      }
    };
    markNative(requestWindow, 'requestWindow');
    Object.defineProperty(DocumentPictureInPicture.prototype, 'requestWindow', {
      value: requestWindow,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  registerBootstrapCallback('installPictureInPictureLifecycle', () => {
    const event = new Event('pagehide');
    if (typeof PageTransitionEvent === 'function')
      Object.setPrototypeOf(event, PageTransitionEvent.prototype);
    Object.defineProperty(event, 'persisted', { value: false, enumerable: true });
    dispatchNative(window, event);
    dispatchNative(window, new Event('unload'));
  });
  const close = function close() {
    host.closeAuxiliaryWindow();
  };
  markNative(close, 'close');
  Object.defineProperty(window, 'close', {
    value: close,
    writable: true,
    enumerable: true,
    configurable: true,
  });
}
