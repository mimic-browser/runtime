// prettier-ignore
(async () => {
  const source = new MediaSource();
  const video = document.createElement('video');
  const result = { events: [] };
  const waitFor = (object, name, action) => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error('Timeout waiting for ' + name)), 2000);
    object.addEventListener(name, () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
    action();
  });
  for (const name of ['sourceopen', 'sourceclose']) {
    source.addEventListener(name, event => result.events.push({
      name, trusted: event.isTrusted, state: source.readyState,
    }));
  }
  const url = URL.createObjectURL(source);
  document.body.appendChild(video);
  await waitFor(source, 'sourceopen', () => {
    video.src = url;
    video.load();
    result.immediateAttachment = source.readyState;
  });
  result.open = {
    state: source.readyState,
    duration: String(source.duration),
    networkState: video.networkState,
    readyState: video.readyState,
    currentSrc: video.currentSrc === url,
  };
  try {
    await fetch(url);
    result.fetch = 'success';
  } catch (error) {
    result.fetch = error.name;
  }
  URL.revokeObjectURL(url);
  result.revokedWhileAttached = source.readyState;
  await waitFor(source, 'sourceclose', () => {
    video.removeAttribute('src');
    video.load();
    result.immediateDetachment = source.readyState;
  });
  result.closed = {
    state: source.readyState,
    duration: String(source.duration),
    buffers: source.sourceBuffers.length,
    currentSrc: video.currentSrc === url,
  };
  video.remove();
  return result;
})()
