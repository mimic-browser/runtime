package browser

import "testing"

func TestWebGLInterpolatesFragmentVaryings(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `(() => {
  const canvas = document.createElement('canvas');
  canvas.width = 2;
  canvas.height = 2;
  const gl = canvas.getContext('webgl', {antialias:false});
  const shader = (type, source) => {
    const value = gl.createShader(type);
    gl.shaderSource(value, source);
    gl.compileShader(value);
    return value;
  };
  const program = gl.createProgram();
  gl.attachShader(program, shader(gl.VERTEX_SHADER, `+"`"+`
    attribute vec2 position;
    varying vec2 coordinate;
    void main() {
      coordinate = (position + 1.0) / 2.0;
      gl_Position = vec4(position, 0.0, 1.0);
    }
  `+"`"+`));
  gl.attachShader(program, shader(gl.FRAGMENT_SHADER, `+"`"+`
    precision mediump float;
    varying vec2 coordinate;
    void main() { gl_FragColor = vec4(coordinate, 1.0, 1.0); }
  `+"`"+`));
  gl.linkProgram(program);
  gl.useProgram(program);
  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 1,-1, -1,1, 1,1]), gl.STATIC_DRAW);
  const location = gl.getAttribLocation(program, 'position');
  gl.enableVertexAttribArray(location);
  gl.vertexAttribPointer(location, 2, gl.FLOAT, false, 0, 0);
  gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  const pixels = new Uint8Array(16);
  gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
  return JSON.stringify({linked:gl.getProgramParameter(program, gl.LINK_STATUS), error:gl.getError(), pixels:Array.from(pixels)});
})()`, `{"linked":true,"error":0,"pixels":[64,64,255,255,191,64,255,255,64,191,255,255,191,191,255,255]}`)
	})
}
