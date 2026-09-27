(() => {
  const marker = Object.freeze(new TypeError('author conversion'));
  for (const name of ['setTimeout', 'setInterval', 'clearTimeout', 'clearInterval']) {
    const invoke = (value) =>
      name.startsWith('set') ? globalThis[name](() => {}, value) : globalThis[name](value);
    for (const value of [Symbol(), 1n, { valueOf: () => 1n }]) {
      let failure;
      try {
        invoke(value);
      } catch (error) {
        failure = error;
      }
      if (!(failure instanceof TypeError) || /at Number \(/.test(failure.stack)) {
        throw new Error(name + ': numeric conversion used the Number constructor');
      }
    }
    let authorFailure;
    try {
      invoke({
        valueOf() {
          throw marker;
        },
      });
    } catch (error) {
      authorFailure = error;
    }
    if (authorFailure !== marker || marker.message !== 'author conversion') {
      throw new Error(name + ': author exception was replaced');
    }
    const originalNumber = Number;
    globalThis.Number = () => {
      throw marker;
    };
    try {
      clearTimeout(invoke(0));
    } finally {
      globalThis.Number = originalNumber;
    }
  }
  return 'ok';
})();
