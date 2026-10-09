// The DOM update lifecycle is independent of rendering. Snapshot/animation
// readiness remains an explicit unsupported boundary in this non-rendering
// runtime; skipping never suppresses the author's update callback.
(() => {
  if (!globalThis.ViewTransition) return;
  const slots = new WeakMap(),
    active = new WeakMap();
  const state = (receiver) => {
    const value = slots.get(receiver);
    if (!value) throw new TypeError('Illegal invocation');
    return value;
  };
  const deferred = () => {
    let resolve, reject;
    const promise = new platformPromise((yes, no) => {
      resolve = yes;
      reject = no;
    });
    // Browser-owned promises can reject before author code reads them.
    promise.catch(() => {});
    return { promise, resolve, reject };
  };
  const skip = (transition, error) => {
    const value = state(transition);
    if (value.skipped) return;
    value.skipped = true;
    value.ready.reject(error);
    if (value.completed) value.finished.resolve();
  };
  Object.defineProperties(ViewTransition.prototype, {
    ready: {
      get: function () {
        return state(this).ready.promise;
      },
      enumerable: true,
      configurable: true,
    },
    updateCallbackDone: {
      get: function () {
        return state(this).updated.promise;
      },
      enumerable: true,
      configurable: true,
    },
    finished: {
      get: function () {
        return state(this).finished.promise;
      },
      enumerable: true,
      configurable: true,
    },
    types: {
      get: function () {
        return state(this).types;
      },
      enumerable: true,
      configurable: true,
    },
    skipTransition: {
      value: function skipTransition() {
        skip(this, platformDOMException('Transition was skipped', 'AbortError'));
      },
      enumerable: true,
      writable: true,
      configurable: true,
    },
  });
  Object.defineProperty(Document.prototype, 'startViewTransition', {
    value: function startViewTransition(callback) {
      if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
      let update = callback,
        types = [];
      if (callback != null && typeof callback === 'object') {
        update = callback.update;
        types = callback.types === undefined ? [] : Array.from(callback.types, String);
      }
      if (update == null) update = () => {};
      if (typeof update !== 'function') throw new TypeError('Update must be callable');
      const transition = Object.create(ViewTransition.prototype);
      const previous = active.get(this);
      if (previous)
        skip(previous, platformDOMException('A new transition was started', 'AbortError'));
      const value = {
        ready: deferred(),
        updated: deferred(),
        finished: deferred(),
        types: new Set(types),
        skipped: false,
        completed: false,
      };
      slots.set(transition, value);
      active.set(this, transition);
      skip(
        transition,
        platformDOMException(
          this.hidden ? 'Document is hidden' : 'Rendering view transition snapshots is unsupported',
          this.hidden ? 'InvalidStateError' : 'NotSupportedError',
        ),
      );
      setTimeout(() => {
        let result;
        try {
          result = Reflect.apply(update, undefined, []);
        } catch (error) {
          result = platformPromiseReject(error);
        }
        platformPromiseResolve(result).then(
          () => {
            value.completed = true;
            value.updated.resolve();
            value.finished.resolve();
            if (active.get(this) === transition) active.delete(this);
          },
          (error) => {
            value.completed = true;
            value.updated.reject(error);
            value.ready.reject(error);
            value.finished.reject(error);
            if (active.get(this) === transition) active.delete(this);
          },
        );
      }, 0);
      return transition;
    },
    enumerable: true,
    writable: true,
    configurable: true,
  });
})();
