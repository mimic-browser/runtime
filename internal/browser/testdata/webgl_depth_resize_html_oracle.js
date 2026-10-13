(() => {
  const canvas = document.createElement('canvas');
  canvas.width = 2;
  canvas.height = 1;
  const gl = canvas.getContext('webgl2', { antialias: false, depth: true });
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
  gl.viewport(0, 0, 2, 1);
  gl.enable(gl.DEPTH_TEST);
  gl.depthFunc(gl.LESS);
  gl.uniform1f(gl.getUniformLocation(program, 'layer'), -0.5);
  gl.uniform4f(gl.getUniformLocation(program, 'inputColor'), 1, 0, 0, 1);
  gl.drawArrays(gl.TRIANGLES, 0, 3);
  canvas.width = canvas.width;
  gl.uniform1f(gl.getUniformLocation(program, 'layer'), 0.5);
  gl.uniform4f(gl.getUniformLocation(program, 'inputColor'), 0, 1, 0, 1);
  gl.drawArrays(gl.TRIANGLES, 0, 3);
  const pixels = new Uint8Array(8);
  gl.readPixels(0, 0, 2, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
  return { pixels: Array.from(pixels), depthEnabled: gl.isEnabled(gl.DEPTH_TEST), error: gl.getError() };
})()
