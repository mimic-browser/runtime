(async () => {
  const empty = new MediaStream();
  const device = (await navigator.mediaDevices.enumerateDevices()).find(
    (device) => device.kind === 'videoinput' && device.label === 'OBS Virtual Camera',
  );
  if (!device) throw new Error('OBS Virtual Camera is required for this diagnostic');
  const stream = await navigator.mediaDevices.getUserMedia({
    video: { deviceId: { exact: device.deviceId } },
  });
  const track = stream.getVideoTracks()[0];
  const clone = track.clone();
  const settings = track.getSettings();
  const output = {
    empty: {
      tag: Object.prototype.toString.call(empty),
      eventTarget: empty instanceof EventTarget,
      active: empty.active,
      tracks: empty.getTracks().length,
      id: typeof empty.id,
    },
    stream: {
      tag: Object.prototype.toString.call(stream),
      eventTarget: stream instanceof EventTarget,
      active: stream.active,
      trackIdentity: stream.getTracks()[0] === track && stream.getTrackById(track.id) === track,
      audioTracks: stream.getAudioTracks().length,
    },
    track: {
      tag: Object.prototype.toString.call(track),
      eventTarget: track instanceof EventTarget,
      kind: track.kind,
      label: track.label,
      enabled: track.enabled,
      muted: track.muted,
      readyState: track.readyState,
      cloneIndependent: track !== clone && track.id !== clone.id,
      cloneSameDevice: settings.deviceId === clone.getSettings().deviceId,
      width: settings.width,
      height: settings.height,
      frameRate: settings.frameRate,
    },
  };
  const video = document.createElement('video');
  video.muted = true;
  video.srcObject = stream;
  await video.play();
  output.video = {
    srcObject: video.srcObject === stream,
    width: video.videoWidth,
    height: video.videoHeight,
    paused: video.paused,
    duration: video.duration === Infinity,
  };
  const bitmap = await createImageBitmap(video);
  output.bitmap = { width: bitmap.width, height: bitmap.height };
  bitmap.close();
  let stopEvents = 0;
  track.onended = () => stopEvents++;
  track.stop();
  await Promise.resolve();
  output.stopped = {
    events: stopEvents,
    readyState: track.readyState,
    streamActive: stream.active,
    cloneReadyState: clone.readyState,
    settingsKeys: Object.keys(track.getSettings()).sort(),
  };
  clone.stop();
  return output;
})()
