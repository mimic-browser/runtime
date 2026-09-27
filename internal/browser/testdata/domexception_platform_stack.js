(() => {
  const constructed = new DOMException('author', 'InvalidCharacterError');
  if (
    Object.getOwnPropertyNames(constructed).length !== 0 ||
    constructed.stack !== undefined ||
    !Error.isError(constructed)
  ) {
    throw new Error('author DOMException gained platform stack state');
  }
  const previous = Error.prepareStackTrace;
  const prepared = [];
  Error.prepareStackTrace = (error, frames) => {
    prepared.push(error);
    return frames;
  };
  try {
    const calls = {
      invalidBase64() {
        try {
          atob('!');
        } catch (error) {
          return error;
        }
      },
      abortStatic: () => AbortSignal.abort().reason,
      abortController() {
        const controller = new AbortController();
        controller.abort();
        return controller.signal.reason;
      },
    };
    if (typeof document !== 'undefined') {
      calls.invalidTag = function invalidTag() {
        try {
          document.createElement('<bad>');
        } catch (error) {
          return error;
        }
      };
    }
    for (const [name, make] of Object.entries(calls)) {
      const before = prepared.length;
      const error = make();
      if (prepared.length !== before) throw new Error(name + ': stack formatted during creation');
      const descriptor = Object.getOwnPropertyDescriptor(error, 'stack');
      if (
        !Error.isError(error) ||
        Object.getOwnPropertyNames(error).join(',') !== 'stack' ||
        typeof descriptor?.get !== 'function' ||
        typeof descriptor?.set !== 'function' ||
        descriptor.enumerable ||
        !descriptor.configurable
      ) {
        throw new Error(name + ': platform exception stack descriptor differs');
      }
      const frames = error.stack;
      if (
        prepared.length !== before + 1 ||
        prepared[before] !== error ||
        frames.length !== 3 ||
        frames[0].getFunctionName() !== name ||
        frames.some((frame) => /mimic:|bootstrap/.test(frame.getFileName() || ''))
      ) {
        throw new Error(
          name + ': platform frames entered the author stack: ' +
          JSON.stringify(frames.map((frame) => ({ name: frame.getFunctionName(), file: frame.getFileName() }))),
        );
      }
      if (error.stack !== frames || prepared.length !== before + 1) {
        throw new Error(name + ': formatted stack was not cached');
      }
    }
    if (Object.hasOwn(globalThis, '__workerPlatformDOMException')) {
      throw new Error('Worker bootstrap transport property leaked');
    }
    Error.prepareStackTrace = previous;
    for (const platform of [false, true]) {
      let error;
      if (platform) {
        try {
          atob('!');
        } catch (failure) {
          error = failure;
        }
      } else {
        error = new DOMException('author', 'InvalidCharacterError');
        Error.captureStackTrace(error);
      }
      Object.defineProperty(error, 'name', { value: 'ChangedName', configurable: true });
      Object.defineProperty(error, 'message', { value: 'changed', configurable: true });
      if (error.stack.split('\n')[0] !== 'ChangedName: changed') {
        throw new Error('DOMException stack used ordinary Error original-message state');
      }
    }
    return 'ok';
  } finally {
    Error.prepareStackTrace = previous;
  }
})();
