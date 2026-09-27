// Oracle expression embedded after await.
// prettier-ignore
(async () => {
  const scriptURL = URL.createObjectURL(new Blob([
    "onmessage=()=>postMessage(URL.createObjectURL(new Blob(['worker-owned'])))",
  ], { type: 'text/javascript' }));
  const worker = new Worker(scriptURL);
  const workerURL = await new Promise((resolve, reject) => {
    worker.onmessage = (event) => resolve(event.data);
    worker.onerror = (event) => reject(Error(event.message));
    worker.postMessage(null);
  });
  const read = async () => {
    try {
      return { body: await (await fetch(workerURL)).text() };
    } catch (error) {
      return { error: error.name };
    }
  };
  const result = { beforeTermination: await read() };
  worker.terminate();
  result.immediateTermination = await read();
  await new Promise((resolve) => setTimeout(resolve, 20));
  result.afterTermination = await read();
  URL.revokeObjectURL(scriptURL);
  return { beforeTermination: result.beforeTermination, afterTermination: result.afterTermination };
})()
