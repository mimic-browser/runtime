(() => {
  const results = [];
  for (const attachment of ['default', 'renderbuffer']) {
    const gl = new OffscreenCanvas(2, 2).getContext('webgl2', {
      antialias: false,
      depth: true,
    });
    let status = null;
    if (attachment === 'renderbuffer') {
      const framebuffer = gl.createFramebuffer();
      gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
      const color = gl.createRenderbuffer();
      gl.bindRenderbuffer(gl.RENDERBUFFER, color);
      gl.renderbufferStorage(gl.RENDERBUFFER, gl.RGBA8, 2, 2);
      gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.RENDERBUFFER, color);
      const depth = gl.createRenderbuffer();
      gl.bindRenderbuffer(gl.RENDERBUFFER, depth);
      gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT16, 2, 2);
      gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depth);
      status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
    }
    const program = gl.createProgram();
    for (const [type, source] of [
      [
        gl.VERTEX_SHADER,
        `#version 300 es
        uniform float layer;
        void main() {
          if (gl_VertexID == 0) gl_Position = vec4(-1.0, -1.0, layer, 1.0);
          if (gl_VertexID == 1) gl_Position = vec4(3.0, -1.0, layer, 1.0);
          if (gl_VertexID == 2) gl_Position = vec4(-1.0, 3.0, layer, 1.0);
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
    const layer = gl.getUniformLocation(program, 'layer');
    const inputColor = gl.getUniformLocation(program, 'inputColor');
    gl.viewport(0, 0, 2, 2);
    gl.clearColor(0, 0, 1, 1);
    gl.clearDepth(1);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    gl.enable(gl.DEPTH_TEST);
    gl.depthFunc(gl.LESS);
    const read = () => {
      const pixels = new Uint8Array(16);
      gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
      return Array.from(pixels);
    };
    const draw = (z, color) => {
      gl.uniform1f(layer, z);
      gl.uniform4f(inputColor, ...color);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      return read();
    };
    const first = draw(0.5, [1, 0, 0, 1]);
    const occluded = draw(0.8, [0, 1, 0, 1]);
    const nearer = draw(-0.5, [0, 1, 0, 1]);
    gl.depthMask(false);
    const noWrite = draw(-0.8, [1, 1, 0, 1]);
    const afterNoWrite = draw(-0.6, [1, 0, 1, 1]);
    gl.depthMask(true);
    gl.enable(gl.SCISSOR_TEST);
    gl.scissor(1, 1, 1, 1);
    gl.clear(gl.DEPTH_BUFFER_BIT);
    gl.disable(gl.SCISSOR_TEST);
    const localClear = draw(0.5, [0, 1, 1, 1]);
    results.push({
      attachment,
      status,
      linked: gl.getProgramParameter(program, gl.LINK_STATUS),
      depthFunc: gl.getParameter(gl.DEPTH_FUNC),
      first,
      occluded,
      nearer,
      noWrite,
      afterNoWrite,
      localClear,
      error: gl.getError(),
    });
  }
  return results;
})()
