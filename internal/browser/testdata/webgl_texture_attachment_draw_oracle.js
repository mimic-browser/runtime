(() => {
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = 2;
  const gl = canvas.getContext('webgl2', { antialias: false });
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 2, 2, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
  const framebuffer = gl.createFramebuffer();
  gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
  const shader = (type, source) => {
    const value = gl.createShader(type);
    gl.shaderSource(value, source);
    gl.compileShader(value);
    return value;
  };
  const program = gl.createProgram();
  gl.attachShader(program, shader(gl.VERTEX_SHADER,
    '#version 300 es\nin vec2 position; void main(){gl_Position=vec4(position,0.,1.);}'));
  gl.attachShader(program, shader(gl.FRAGMENT_SHADER,
    '#version 300 es\nprecision highp float; out vec4 color; void main(){color=vec4(1.,0.,0.,1.);}'));
  gl.linkProgram(program);
  gl.useProgram(program);
  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.bufferData(gl.ARRAY_BUFFER,
    new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
  const location = gl.getAttribLocation(program, 'position');
  gl.enableVertexAttribArray(location);
  gl.vertexAttribPointer(location, 2, gl.FLOAT, false, 0, 0);
  gl.viewport(0, 0, 2, 2);
  gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  const read = () => {
    const pixels = new Uint8Array(16);
    gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
    return Array.from(pixels);
  };
  const drawn = read();
  gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  gl.texSubImage2D(gl.TEXTURE_2D, 0, 1, 1, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE,
    new Uint8Array([0, 0, 255, 255]));
  const subimageAfterDraw = read();
  return {
    linked: gl.getProgramParameter(program, gl.LINK_STATUS),
    status: gl.checkFramebufferStatus(gl.FRAMEBUFFER),
    drawn, subimageAfterDraw, error: gl.getError(),
  };
})()
