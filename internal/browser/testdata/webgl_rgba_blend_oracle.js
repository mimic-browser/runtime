(() => {
  const cases = [];
  for (const attachment of ['default', 'renderbuffer', 'texture']) {
    const gl = new OffscreenCanvas(2, 2).getContext('webgl2', { antialias: false });
    let framebuffer = null;
    if (attachment !== 'default') {
      framebuffer = gl.createFramebuffer();
      gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
      if (attachment === 'renderbuffer') {
        const renderbuffer = gl.createRenderbuffer();
        gl.bindRenderbuffer(gl.RENDERBUFFER, renderbuffer);
        gl.renderbufferStorage(gl.RENDERBUFFER, gl.RGBA8, 2, 2);
        gl.framebufferRenderbuffer(
          gl.FRAMEBUFFER,
          gl.COLOR_ATTACHMENT0,
          gl.RENDERBUFFER,
          renderbuffer,
        );
      } else {
        const texture = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 2, 2, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
        gl.framebufferTexture2D(
          gl.FRAMEBUFFER,
          gl.COLOR_ATTACHMENT0,
          gl.TEXTURE_2D,
          texture,
          0,
        );
      }
    }
    const status = attachment === 'default' ? null : gl.checkFramebufferStatus(gl.FRAMEBUFFER);
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
        uniform vec4 inputColor;
        out vec4 color;
        void main() { color = inputColor; }`,
      ],
    ]) {
      const shader = gl.createShader(type);
      gl.shaderSource(shader, source);
      gl.compileShader(shader);
      gl.attachShader(program, shader);
    }
    gl.linkProgram(program);
    gl.useProgram(program);
    const inputColor = gl.getUniformLocation(program, 'inputColor');
    gl.viewport(0, 0, 2, 2);
    gl.clearColor(0, 0, 1, 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.uniform4f(inputColor, 1, 0, 0, 0.5);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    const first = new Uint8Array(4);
    gl.readPixels(1, 1, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, first);
    gl.uniform4f(inputColor, 0, 1, 0, 0.5);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    const second = new Uint8Array(4);
    gl.readPixels(1, 1, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, second);
    cases.push({
      attachment,
      status,
      linked: gl.getProgramParameter(program, gl.LINK_STATUS),
      first: Array.from(first),
      second: Array.from(second),
      error: gl.getError(),
    });
  }
  return cases;
})()
