(async () => {
  const run = (path) =>
    new Promise((resolve) => {
      const worker = new Worker(path, { type: 'module' });
      worker.onmessage = (event) => {
        worker.terminate();
        resolve(event.data);
      };
      worker.onerror = (event) => {
        event.preventDefault();
        worker.terminate();
        resolve('error');
      };
    });
  return {
    redirect: await run('/redirect-start.js'),
    failedImports: await run('/module/retry.js'),
    asyncThrow: await run('/module/async-throw.js'),
  };
})()
