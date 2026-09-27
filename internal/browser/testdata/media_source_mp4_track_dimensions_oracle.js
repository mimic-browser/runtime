// Independent authored dimension declaration probe.
// prettier-ignore
(async () => {
  const init = Uint8Array.from(atob('AAAAHGZ0eXBpc281AAACAGlzbzVpc282bXA0MQAAAuVtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAAAAABAAABAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAAB6HRyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAIAAAADAAAAAAAYRtZGlhAAAAIG1kaGQAAAAAAAAAAAAAAAAAAEAAAAAAAFXEAAAAAAAtaGRscgAAAAAAAAAAdmlkZQAAAAAAAAAAAAAAAFZpZGVvSGFuZGxlcgAAAAEvbWluZgAAABR2bWhkAAAAAQAAAAAAAAAAAAAAJGRpbmYAAAAcZHJlZgAAAAAAAAABAAAADHVybCAAAAABAAAA73N0YmwAAACjc3RzZAAAAAAAAAABAAAAk2F2YzEAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAEAAQAEgAAABIAAAAAAAAAAEVTGF2YzYyLjIzLjEwMiBsaWJ4MjY0AAAAAAAAAAAAAAAY//8AAAAtYXZjQwFCwAr/4QAVZ0LACt3sBEAAAAMAQAAAAwCDxIngAQAFaM4PLIAAAAAQcGFzcAAAAAEAAAABAAAAEHN0dHMAAAAAAAAAAAAAABBzdHNjAAAAAAAAAAAAAAAUc3RzegAAAAAAAAAAAAAAAAAAABBzdGNvAAAAAAAAAAAAAAAobXZleAAAACB0cmV4AAAAAAAAAAEAAAABAAAAAAAAAAAAAAAAAAAAYXVkdGEAAABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSpdG9vAAAAHGRhdGEAAAABAAAAAExhdmY2Mi44LjEwMg=='), value => value.charCodeAt(0));
  const result = { events: [] };
  const source = new MediaSource();
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  document.body.append(video);
  await new Promise(resolve => {
    source.addEventListener('sourceopen', resolve, {once: true});
    video.src = url;
    video.load();
  });
  const buffer = source.addSourceBuffer('video/mp4; codecs="avc1.42C00A"');
  const observe = () => ({
    updating: buffer.updating,
    active: source.activeSourceBuffers.length,
    duration: String(source.duration),
    readyState: video.readyState,
    videoDuration: String(video.duration),
    width: video.videoWidth,
    height: video.videoHeight,
    ranges: buffer.buffered.length,
  });
  for (const name of ['updatestart', 'update', 'updateend', 'error']) {
    buffer.addEventListener(name, event => result.events.push({name, trusted:event.isTrusted}));
  }
  const append = bytes => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error('append did not settle')), 2000);
    buffer.addEventListener('updateend', () => { clearTimeout(timer); resolve(); }, {once:true});
    buffer.appendBuffer(bytes);
  });
  try {
    await append(init.subarray(0, 37));
    result.partial = observe();
    await append(init.subarray(37));
    await new Promise(resolve => setTimeout(resolve, 20));
    result.complete = observe();
    result.activeIdentity = source.activeSourceBuffers[0] === buffer;
  } catch (error) {
    result.failure = {name:error.name, message:error.message};
  }
  video.removeAttribute('src');
  video.load();
  URL.revokeObjectURL(url);
  video.remove();
  return result;
})()
