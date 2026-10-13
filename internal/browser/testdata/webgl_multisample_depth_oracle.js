(() => {
  const gl = new OffscreenCanvas(2, 2).getContext('webgl', {
    antialias: true,
    depth: true,
  });
  const program = gl.createProgram();
  for (const [type, source] of [
    [
      gl.VERTEX_SHADER,
      `attribute vec2 position;
       uniform float layer;
       void main() { gl_Position = vec4(position, layer, 1.0); }`,
    ],
    [
      gl.FRAGMENT_SHADER,
      `precision highp float;
       uniform vec4 color;
       void main() { gl_FragColor = color; }`,
    ],
  ]) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    gl.attachShader(program, shader);
  }
  gl.linkProgram(program);
  gl.useProgram(program);
  const position = gl.getAttribLocation(program, 'position');
  const layer = gl.getUniformLocation(program, 'layer');
  const color = gl.getUniformLocation(program, 'color');
  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.enableVertexAttribArray(position);
  gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
  gl.viewport(0, 0, 2, 2);
  gl.clearColor(0, 0, 0, 1);
  gl.clearDepth(1);
  gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
  gl.enable(gl.DEPTH_TEST);
  gl.depthFunc(gl.LESS);
  const read = () => {
    const pixels = new Uint8Array(16);
    gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
    return Array.from(pixels);
  };
  const draw = (vertices, depth, rgba) => {
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(vertices), gl.STATIC_DRAW);
    gl.uniform1f(layer, depth);
    gl.uniform4f(color, ...rgba);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    return read();
  };
  const partial = draw([-1, -1, 1, -1, -1, 1], 0, [1, 0, 0, 1]);
  const farther = draw([-1, -1, 3, -1, -1, 3], 0.8, [0, 1, 0, 1]);
  gl.disable(gl.DEPTH_TEST);
  gl.clear(gl.COLOR_BUFFER_BIT);
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  const blendedPartial = draw([-1, -1, 1, -1, -1, 1], 0, [1, 0, 0, 0.5]);
  const blendedOverlap = draw([-1, -1, 3, -1, -1, 3], 0, [0, 1, 0, 0.5]);
  return {
    antialias: gl.getContextAttributes().antialias,
    samples: gl.getParameter(gl.SAMPLES),
    partial,
    farther,
    blendedPartial,
    blendedOverlap,
    error: gl.getError(),
  };
})()
