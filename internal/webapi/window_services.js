// Stateful Window services. Persistent data is retained by the owning host;
// wrappers and callbacks remain in their creating realm.
{
  const nativeMethod = (proto, name, fn) => {
    markNative(fn, name);
    Object.defineProperty(proto, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const getter = (proto, name, fn) => {
    markNative(fn, name, 'get ');
    Object.defineProperty(proto, name, { get: fn, enumerable: true, configurable: true });
  };
  const requireArgs = (name, type, count, args) => {
    if (args.length < count)
      throw new TypeError(
        `Failed to execute '${name}' on '${type}': ${count} argument${count === 1 ? '' : 's'} required, but only ${args.length} present.`,
      );
  };
  nativeMethod(window, 'open', function open(url = '', target = '_blank', features = '') {
    return host.openWindow(String(url), String(target), String(features));
  });
  if (typeof CrashReportContext === 'function') {
    const report = Object.create(CrashReportContext.prototype);
    replaceableWindow('crashReport', () => report);
    const quote = JSON.stringify;
    const fail = (method, name, message) =>
      platformDOMException(
        `Failed to execute '${method}' on 'CrashReportContext': ${message}`,
        name,
      );
    const check = (receiver) => {
      if (receiver !== report) throw new TypeError('Illegal invocation');
    };
    nativeMethod(CrashReportContext.prototype, 'initialize', function initialize(size) {
      try {
        check(this);
        requireArgs('initialize', 'CrashReportContext', 1, arguments);
        size = Number(size) >>> 0;
        if (!host.requestCrashReport(size))
          throw fail(
            'initialize',
            'InvalidStateError',
            'The initialize() method has already been called.',
          );
        return new platformPromise((resolve, reject) =>
          host.enqueueWebTask(
            () => {
              if (!host.initializeCrashReport()) {
                reject(platformDOMException('The requested size is too large.', 'NotAllowedError'));
                return;
              }
              resolve();
            },
            1,
            0,
            false,
          ),
        );
      } catch (e) {
        return platformPromiseReject(e);
      }
    });
    const mutate = (method, key, value) => {
      const error = host.mutateCrashReport(
        method,
        quote(key),
        value === undefined ? '' : quote(value),
      );
      if (error === 'InvalidStateError')
        throw fail(
          method,
          error,
          'CrashReportContext is not initialized. Call initialize() and wait for it to resolve.',
        );
      if (error === 'NotAllowedError')
        throw fail(
          method,
          error,
          'The crash report data is too large to be stored in the requested buffer.',
        );
    };
    nativeMethod(CrashReportContext.prototype, 'set', function set(key, value) {
      check(this);
      requireArgs('set', 'CrashReportContext', 2, arguments);
      mutate('set', String(key), String(value));
    });
    nativeMethod(CrashReportContext.prototype, 'delete', function delete_(key) {
      check(this);
      requireArgs('delete', 'CrashReportContext', 1, arguments);
      mutate('delete', String(key));
    });
  }
  if (typeof Scheduler === 'function') {
    const schedulerObject = Object.create(Scheduler.prototype),
      signals = new WeakMap(),
      controllers = new WeakMap();
    replaceableWindow('scheduler', () => schedulerObject);
    const priorities = ['user-blocking', 'user-visible', 'background'];
    const priority = (value) => {
      const p = String(value);
      if (!priorities.includes(p))
        throw new TypeError(
          `The provided value '${p}' is not a valid enum value of type TaskPriority.`,
        );
      return p;
    };
    const taskSignalProto = globalThis.TaskSignal?.prototype;
    const priorityEvents = new WeakMap();
    class TaskPriorityChangeEvent extends Event {
      constructor(type, init) {
        requireArgs('constructor', 'TaskPriorityChangeEvent', 2, arguments);
        if (init?.previousPriority === undefined)
          throw new TypeError('previousPriority is required');
        super(type, init);
        priorityEvents.set(this, priority(init.previousPriority));
      }
      get previousPriority() {
        if (!priorityEvents.has(this)) throw new TypeError('Illegal invocation');
        return priorityEvents.get(this);
      }
    }
    Object.defineProperty(TaskPriorityChangeEvent.prototype, Symbol.toStringTag, {
      value: 'TaskPriorityChangeEvent',
      configurable: true,
    });
    markNative(TaskPriorityChangeEvent, 'TaskPriorityChangeEvent');
    Object.defineProperty(globalThis, 'TaskPriorityChangeEvent', {
      value: TaskPriorityChangeEvent,
      writable: true,
      configurable: true,
    });
    if (taskSignalProto) {
      Object.setPrototypeOf(taskSignalProto, AbortSignal.prototype);
      getter(taskSignalProto, 'priority', function () {
        const s = signals.get(this);
        if (!s) throw new TypeError('Illegal invocation');
        return priorities[host.webTaskSignalPriority(s)];
      });
      Object.defineProperty(taskSignalProto, 'onprioritychange', {
        get() {
          return eventHandlerRecord(this, 'prioritychange').value;
        },
        set(value) {
          if (!signals.has(this)) throw new TypeError('Illegal invocation');
          setEventHandlerValue(this, 'prioritychange', value);
        },
        enumerable: true,
        configurable: true,
      });
      class TaskController extends AbortController {
        constructor(options = {}) {
          super();
          const p = priority(options?.priority ?? 'user-visible');
          const signal = this.signal;
          Object.setPrototypeOf(signal, taskSignalProto);
          signals.set(signal, host.newWebTaskSignal(priorities.indexOf(p)));
          controllers.set(this, signal);
        }
        setPriority(value) {
          const signal = controllers.get(this);
          if (!signal) throw new TypeError('Illegal invocation');
          requireArgs('setPriority', 'TaskController', 1, arguments);
          const p = priority(value),
            id = signals.get(signal),
            [previous, status] = host.beginWebTaskPriorityChange(id, priorities.indexOf(p));
          if (status < 0)
            throw platformDOMException(
              'Cannot change priority during a prioritychange event.',
              'NotAllowedError',
            );
          if (!status) return;
          try {
            dispatchTrusted(
              signal,
              new TaskPriorityChangeEvent('prioritychange', {
                previousPriority: priorities[previous],
              }),
            );
          } finally {
            host.endWebTaskPriorityChange(id);
          }
        }
      }
      Object.defineProperty(TaskController.prototype, Symbol.toStringTag, {
        value: 'TaskController',
        configurable: true,
      });
      markNative(TaskController, 'TaskController');
      Object.defineProperty(globalThis, 'TaskController', {
        value: TaskController,
        writable: true,
        configurable: true,
      });
    }
    const enqueue = (callback, options, continuation) => {
      const signal = options.signal;
      if (signal !== undefined && !(signal instanceof AbortSignal))
        throw new TypeError('signal must be an AbortSignal');
      const variable =
        options.priority === undefined && signals.has(signal) ? signals.get(signal) : null;
      const p = priority(
        options.priority ??
          (variable ? priorities[host.webTaskSignalPriority(variable)] : 'user-visible'),
      );
      const delay = options.delay === undefined ? 0 : Number(options.delay);
      if (!Number.isFinite(delay) || delay < 0 || delay > Number.MAX_SAFE_INTEGER)
        throw new TypeError('delay is outside the accepted range');
      if (signal?.aborted) return platformPromiseReject(signal.reason);
      return new platformPromise((resolve, reject) => {
        let taskID = 0;
        const cleanup = () => {
          signal?.removeEventListener('abort', cancel);
        };
        const cancel = () => {
          host.changeWebTask(taskID, -1, false);
          cleanup();
          reject(signal.reason);
        };
        taskID = host.enqueueWebTask(
          () => {
            try {
              platformPromiseResolve(callback()).then(
                (value) => {
                  cleanup();
                  resolve(value);
                },
                (error) => {
                  cleanup();
                  reject(error);
                },
              );
            } catch (e) {
              cleanup();
              reject(e);
            }
          },
          priorities.indexOf(p),
          Math.trunc(delay),
          continuation,
          variable ?? 0,
          signal,
        );
        signal?.addEventListener('abort', cancel, { once: true });
      });
    };
    nativeMethod(Scheduler.prototype, 'postTask', function postTask(callback, options = {}) {
      try {
        if (this !== schedulerObject) throw new TypeError('Illegal invocation');
        requireArgs('postTask', 'Scheduler', 1, arguments);
        if (typeof callback !== 'function') throw new TypeError('The callback must be a function');
        return enqueue(callback, options ?? {}, false);
      } catch (e) {
        return platformPromiseReject(e);
      }
    });
    nativeMethod(Scheduler.prototype, 'yield', function yield_() {
      try {
        if (this !== schedulerObject) throw new TypeError('Illegal invocation');
        const [id, p, variable, signal] = host.currentWebTask(),
          options = id
            ? { signal: signal ?? undefined, priority: variable ? undefined : priorities[p] }
            : {};
        return enqueue(() => undefined, options, true);
      } catch (e) {
        return platformPromiseReject(e);
      }
    });
  }

  if (typeof CookieStore === 'function') {
    const store = new EventTarget();
    Object.setPrototypeOf(store, CookieStore.prototype);
    Object.defineProperty(globalThis, 'cookieStore', {
      get() {
        return store;
      },
      enumerable: true,
      configurable: true,
    });
    const run = (receiver, name, count, args, action) => {
      try {
        if (receiver !== store) throw new TypeError('Illegal invocation');
        requireArgs(name, 'CookieStore', count, args);
        return action();
      } catch (e) {
        return platformPromiseReject(e);
      }
    };
    const io = (action) => {
      if (!host.hasStorageAccess())
        return platformPromiseReject(
          platformDOMException('Access to cookies is denied.', 'SecurityError'),
        );
      return new platformPromise((resolve, reject) =>
        host.enqueueWebTask(
          () => {
            try {
              resolve(action());
            } catch (e) {
              reject(e);
            }
          },
          1,
          0,
          false,
        ),
      );
    };
    const read = (options) => {
      options = typeof options === 'string' ? { name: options } : (options ?? {});
      if (options.url !== undefined) {
        const u = new URL(String(options.url), document.baseURI);
        if (u.href !== document.URL) throw new TypeError('URL must match the document URL');
      }
      const name = options.name === undefined ? undefined : String(options.name);
      return io(() =>
        host.cookieStoreRead().filter((row) => name === undefined || row.name === name),
      );
    };
    nativeMethod(CookieStore.prototype, 'get', function get(options) {
      return run(this, 'get', 1, arguments, () => read(options).then((rows) => rows[0] ?? null));
    });
    nativeMethod(CookieStore.prototype, 'getAll', function getAll(options) {
      return run(this, 'getAll', 0, arguments, () => read(options));
    });
    const write = (options, remove) => {
      if (!options || options.name === undefined || (!remove && options.value === undefined))
        throw new TypeError('Required cookie member is missing');
      const name = String(options.name),
        value = remove ? '' : String(options.value),
        path = String(options.path ?? '/'),
        domain = options.domain == null ? null : String(options.domain),
        sameSite = String(options.sameSite ?? 'strict');
      if (
        /[\x00-\x20\x7f;=]/.test(name) ||
        /[\x00-\x1f\x7f;]/.test(value) ||
        (!name && value.includes('='))
      )
        throw new TypeError('Invalid cookie name or value');
      if (!path.startsWith('/') || path.includes(';')) throw new TypeError('Invalid cookie path');
      if (!['strict', 'lax', 'none'].includes(sameSite))
        throw new TypeError('Invalid SameSite value');
      if (
        domain !== null &&
        (domain.startsWith('.') ||
          !(location.hostname === domain || location.hostname.endsWith('.' + domain)))
      )
        throw new TypeError('Invalid cookie domain');
      const expires = options.expires == null ? null : Number(options.expires);
      if (expires !== null && !Number.isFinite(expires))
        throw new TypeError('Invalid cookie expiration');
      const record = {
        name,
        value,
        path,
        domain,
        sameSite,
        expires,
        partitioned: Boolean(options.partitioned),
        delete: remove,
      };
      return io(() => host.cookieStoreWrite(JSON.stringify(record)));
    };
    nativeMethod(CookieStore.prototype, 'set', function set(name, value) {
      return run(this, 'set', 1, arguments, () =>
        write(typeof name === 'string' ? { name, value } : name, false),
      );
    });
    nativeMethod(CookieStore.prototype, 'delete', function delete_(options) {
      return run(this, 'delete', 1, arguments, () =>
        write(typeof options === 'string' ? { name: options } : options, true),
      );
    });
    Object.defineProperty(CookieStore.prototype, 'onchange', {
      get() {
        if (this !== store) throw new TypeError('Illegal invocation');
        return eventHandlerRecord(store, 'change').value;
      },
      set(value) {
        if (this !== store) throw new TypeError('Illegal invocation');
        setEventHandlerValue(store, 'change', value);
      },
      enumerable: true,
      configurable: true,
    });
    registerBootstrapCallback('installCookieStoreObserver', (row, deleted) => {
      const event = new Event('change');
      if (typeof CookieChangeEvent === 'function')
        Object.setPrototypeOf(event, CookieChangeEvent.prototype);
      Object.defineProperties(event, {
        changed: { value: deleted ? [] : [row], enumerable: true },
        deleted: { value: deleted ? [row] : [], enumerable: true },
      });
      dispatchNative(store, event);
    });
  }

  if (typeof LaunchQueue === 'function' && typeof LaunchParams === 'function') {
    const queue = Object.create(LaunchQueue.prototype),
      params = new WeakMap();
    let consumer = null,
      pending = false;
    Object.defineProperty(globalThis, 'launchQueue', {
      get() {
        return queue;
      },
      enumerable: true,
      configurable: true,
    });
    getter(LaunchParams.prototype, 'targetURL', function () {
      const s = params.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.targetURL;
    });
    getter(LaunchParams.prototype, 'files', function () {
      const s = params.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.files;
    });
    const deliver = () => {
      if (pending || !consumer) return;
      pending = true;
      host.enqueueWebTask(
        () => {
          pending = false;
          if (!consumer) return;
          const targetURL = host.nextLaunch();
          if (targetURL == null) return;
          const launch = Object.create(LaunchParams.prototype);
          params.set(launch, { targetURL, files: Object.freeze([]) });
          try {
            consumer(launch);
          } finally {
            deliver();
          }
        },
        1,
        0,
        false,
      );
    };
    nativeMethod(LaunchQueue.prototype, 'setConsumer', function setConsumer(callback) {
      if (this !== queue) throw new TypeError('Illegal invocation');
      requireArgs('setConsumer', 'LaunchQueue', 1, arguments);
      if (typeof callback !== 'function')
        throw new TypeError(
          "Failed to execute 'setConsumer' on 'LaunchQueue': parameter 1 is not of type 'Function'.",
        );
      consumer = callback;
      deliver();
    });
    registerBootstrapCallback('installLaunchNotifier', deliver);
  }
}
