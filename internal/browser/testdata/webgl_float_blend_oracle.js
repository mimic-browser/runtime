(() => {
  const canvas = new OffscreenCanvas(4, 4);
  const gl = canvas.getContext('webgl2', { antialias: false });
  const extension = gl.getExtension('EXT_float_blend');
  gl.getExtension('EXT_color_buffer_float');

  const framebuffer = gl.createFramebuffer();
  const renderbuffer = gl.createRenderbuffer();
  gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
  gl.bindRenderbuffer(gl.RENDERBUFFER, renderbuffer);
  gl.renderbufferStorage(gl.RENDERBUFFER, gl.RGBA32F, 4, 4);
  gl.framebufferRenderbuffer(
    gl.FRAMEBUFFER,
    gl.COLOR_ATTACHMENT0,
    gl.RENDERBUFFER,
    renderbuffer,
  );
  const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);

  const program = gl.createProgram();
  for (const [type, source] of [
    [
      gl.VERTEX_SHADER,
      `#version 300 es
      void main() {
        if (gl_VertexID == 0) gl_Position = vec4(-1.0, -1.0, 0.0, 1.0);
        if (gl_VertexID == 1) gl_Position = vec4(3.0, -1.0, 0.0, 1.0);
        if (gl_VertexID == 2) gl_Position = vec4(-1.0, 3.0, 0.0, 1.0);
      }`,
    ],
    [
      gl.FRAGMENT_SHADER,
      `#version 300 es
      precision highp float;
      out vec4 color;
      void main() { color = vec4(0.25, 0.5, 0.75, 0.4); }`,
    ],
  ]) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    gl.attachShader(program, shader);
  }
  gl.linkProgram(program);
  gl.useProgram(program);
  gl.viewport(0, 0, 4, 4);
  gl.clearColor(0.1, 0.2, 0.3, 0.4);
  gl.clear(gl.COLOR_BUFFER_BIT);
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  gl.drawArrays(gl.TRIANGLES, 0, 3);
  const drawError = gl.getError();
  const pixel = new Float32Array(4);
  gl.readPixels(2, 2, 1, 1, gl.RGBA, gl.FLOAT, pixel);
  return {
    extension: !!extension,
    status,
    linked: gl.getProgramParameter(program, gl.LINK_STATUS),
    drawError,
    readError: gl.getError(),
    pixel: Array.from(pixel),
  };
})()
