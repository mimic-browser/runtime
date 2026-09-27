// Oracle expression embedded after await.
// prettier-ignore
(async () => {
  const frame = document.createElement('iframe');
  await new Promise((resolve) => {
    frame.onload = resolve;
    frame.srcdoc = '<!doctype html><body></body>';
    document.body.append(frame);
  });
  const blob = new frame.contentWindow.Blob(['child-owned'], { type: 'text/plain' });
  const childCreateURL = frame.contentWindow.URL.createObjectURL;
  const childURL = frame.contentWindow.URL.createObjectURL(blob);
  const parentURL = URL.createObjectURL(new Blob(['parent-owned']));
  const read = async (url) => {
    try {
      return { body: await (await fetch(url)).text() };
    } catch (error) {
      return { error: error.name };
    }
  };
  const result = { beforeChildRemoval: await read(childURL), beforeParent: await read(parentURL) };
  frame.remove();
  result.afterChildRemoval = await read(childURL);
  result.afterParent = await read(parentURL);
  try {
    const lateURL = childCreateURL(blob);
    result.inactiveURLCreated = typeof lateURL === 'string';
    result.inactiveURLValue = lateURL;
    const lateRead = await read(lateURL);
    result.inactiveURLRead = { readable: !lateRead.error, error: lateRead.error || null };
    URL.revokeObjectURL(lateURL);
  } catch (error) {
    result.inactiveURLCreated = { error: error.name, message: error.message };
  }
  result.retainedBlobSize = blob.size;
  URL.revokeObjectURL(parentURL);
  return result;
})()
