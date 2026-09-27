// Synthetic initialization metadata only; no coded samples.
// prettier-ignore
(async () => {
  const bytes = Uint8Array.from(atob('AAAAHGZ0eXBpc281AAACAGlzbzVpc282bXA0MQAAAuVtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAAAAABAAABAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAAB6HRyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAEAAAABAAAAAAAYRtZGlhAAAAIG1kaGQAAAAAAAAAAAAAAAAAAEAAAAAAAFXEAAAAAAAtaGRscgAAAAAAAAAAdmlkZQAAAAAAAAAAAAAAAFZpZGVvSGFuZGxlcgAAAAEvbWluZgAAABR2bWhkAAAAAQAAAAAAAAAAAAAAJGRpbmYAAAAcZHJlZgAAAAAAAAABAAAADHVybCAAAAABAAAA73N0YmwAAACjc3RzZAAAAAAAAAABAAAAk2F2YzEAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAEAAQAEgAAABIAAAAAAAAAAEVTGF2YzYyLjIzLjEwMiBsaWJ4MjY0AAAAAAAAAAAAAAAY//8AAAAtYXZjQwFCwAr/4QAVZ0LACt3sBEAAAAMAQAAAAwCDxIngAQAFaM4PLIAAAAAQcGFzcAAAAAEAAAABAAAAEHN0dHMAAAAAAAAAAAAAABBzdHNjAAAAAAAAAAAAAAAUc3RzegAAAAAAAAAAAAAAAAAAABBzdGNvAAAAAAAAAAAAAAAobXZleAAAACB0cmV4AAAAAAAAAAEAAAABAAAAAAAAAAAAAAAAAAAAYXVkdGEAAABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSpdG9vAAAAHGRhdGEAAAABAAAAAExhdmY2Mi44LjEwMg=='), value => value.charCodeAt(0));
  const source = new MediaSource();
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  document.body.append(video);
  await new Promise(resolve => { source.addEventListener('sourceopen', resolve, {once:true}); video.src=url; video.load(); });
  const snapshot = range => Array.from({length:range.length}, (_, index) => [range.start(index),range.end(index)]);
  source.duration = 7;
  const result = {beforeInitialization:snapshot(video.seekable)};
  const buffer = source.addSourceBuffer('video/mp4; codecs="avc1.42C00A"');
  await new Promise(resolve => { buffer.addEventListener('updateend',resolve,{once:true}); buffer.appendBuffer(bytes); });
  await new Promise(resolve=>setTimeout(resolve,20));
  const saved = video.seekable;
  result.afterInitialization = snapshot(saved);
  source.duration = 0;
  result.zeroDuration = snapshot(video.seekable);
  result.savedAfterChange = snapshot(saved);
  source.duration = Infinity;
  result.infiniteDuration = snapshot(video.seekable);
  source.duration = 3;
  result.changedDuration = snapshot(video.seekable);
  result.fresh = video.seekable !== video.seekable;
  video.removeAttribute('src'); video.load();
  await new Promise(resolve=>setTimeout(resolve,20));
  result.afterReset = snapshot(video.seekable);
  URL.revokeObjectURL(url); video.remove();
  return result;
})()
