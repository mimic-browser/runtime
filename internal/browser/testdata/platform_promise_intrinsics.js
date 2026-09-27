(() => {
  const IntrinsicPromise = Promise;
  const statics = Object.fromEntries(
    ['resolve', 'reject', 'all', 'race'].map((name) => [name, Promise[name]]),
  );
  const then = Promise.prototype.then;
  let calls = 0;
  try {
    for (const mode of ['constructor', 'statics']) {
      globalThis.Promise =
        mode === 'constructor'
          ? function AuthorPromise(executor) {
              calls++;
              return new IntrinsicPromise(executor);
            }
          : IntrinsicPromise;
      if (mode === 'statics') {
        for (const name of Object.keys(statics)) {
          IntrinsicPromise[name] = () => {
            calls++;
            throw new Error('author Promise.' + name);
          };
        }
      }
      const operations = {
        fetch: () => fetch('data:text/plain,ok'),
        blobText: () => new Blob(['ok']).text(),
        blobBytes: () => new Blob(['ok']).bytes(),
        blobBuffer: () => new Blob(['ok']).arrayBuffer(),
        responseText: () => new Response('ok').text(),
      };
      for (const [name, make] of Object.entries(operations)) {
        const promise = make();
        if (Object.getPrototypeOf(promise) !== IntrinsicPromise.prototype || calls !== 0) {
          throw new Error(mode + ': ' + name + ' consulted author Promise machinery');
        }
        then.call(promise, () => {}, () => {});
      }
    }
    return 'ok';
  } finally {
    globalThis.Promise = IntrinsicPromise;
    for (const [name, original] of Object.entries(statics)) IntrinsicPromise[name] = original;
  }
})();
