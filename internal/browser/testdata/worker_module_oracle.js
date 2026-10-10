(async () => {
  const run = (path, options = { type: 'module' }, messages = 1) =>
    new Promise((resolve) => {
      const worker = new Worker(path, options);
      const values = [];
      worker.onmessage = (event) => {
        values.push(event.data);
        if (values.length === messages) {
          worker.terminate();
          resolve(values);
        }
      };
      worker.onerror = (event) => {
        event.preventDefault();
        worker.terminate();
        resolve(['error']);
      };
      worker.postMessage('queued');
    });
  return {
    graph: await run('/module/entry.js'),
    awaitOrder: await run('/module/await.js', { type: 'module' }, 2),
    mime: await run('/module/mime.js'),
    missing: await run('/module/missing-entry.js'),
    throwing: await run('/module/throw.js'),
  };
})()
