(() => {
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = 2;
  const gl = canvas.getContext('webgl2', {
    antialias: false,
    preserveDrawingBuffer: true,
  });
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  const initial = new Uint8Array([
    255, 0, 0, 255, 0, 255, 0, 255,
    0, 0, 255, 255, 255, 255, 255, 255,
  ]);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 2, 2, 0, gl.RGBA, gl.UNSIGNED_BYTE, initial);
  const framebuffer = gl.createFramebuffer();
  gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
  const attachment = {
    status: gl.checkFramebufferStatus(gl.FRAMEBUFFER),
    type: gl.getFramebufferAttachmentParameter(
      gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.FRAMEBUFFER_ATTACHMENT_OBJECT_TYPE,
    ),
    same: gl.getFramebufferAttachmentParameter(
      gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.FRAMEBUFFER_ATTACHMENT_OBJECT_NAME,
    ) === texture,
  };
  const read = () => {
    const pixels = new Uint8Array(16);
    gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
    return Array.from(pixels);
  };
  const uploaded = read();
  gl.texSubImage2D(
    gl.TEXTURE_2D, 0, 1, 1, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE,
    new Uint8Array([31, 63, 127, 255]),
  );
  const updated = read();
  gl.enable(gl.SCISSOR_TEST);
  gl.scissor(0, 0, 1, 1);
  gl.clearColor(0.25, 0.5, 0.75, 1);
  gl.clear(gl.COLOR_BUFFER_BIT);
  gl.disable(gl.SCISSOR_TEST);
  const cleared = read();
  gl.bindTexture(gl.TEXTURE_2D, null);
  const unbound = read();
  const renderbuffer = gl.createRenderbuffer();
  gl.bindRenderbuffer(gl.RENDERBUFFER, renderbuffer);
  gl.renderbufferStorage(gl.RENDERBUFFER, gl.RGBA8, 2, 2);
  gl.framebufferRenderbuffer(
    gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.RENDERBUFFER, renderbuffer,
  );
  const renderbufferStatus = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
  gl.clearColor(0, 1, 0, 1);
  gl.clear(gl.COLOR_BUFFER_BIT);
  const renderbufferPixels = read();
  return {
    attachment, uploaded, updated, cleared, unbound,
    renderbufferStatus, renderbufferPixels, error: gl.getError(),
  };
})()
