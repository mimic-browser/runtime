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

// The frozen headful Chrome 152 D3D11 capture of this triangle has red
// values 207, 223, and 240 at these three interior pixels. The latter two
// distinguish raster interpolation quantization from full-precision lerp.
func TestWebGLVaryingQuantizationMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `(() => {
  const canvas = document.createElement('canvas');
  canvas.width = 300; canvas.height = 150;
  const gl = canvas.getContext('webgl', {preserveDrawingBuffer:true});
  const shader = (type, source) => {const s=gl.createShader(type);gl.shaderSource(s,source);gl.compileShader(s);return s};
  const program=gl.createProgram();
  gl.attachShader(program,shader(gl.VERTEX_SHADER,'attribute vec2 position;varying vec2 coordinate;uniform vec2 offset;void main(){coordinate=position+offset;gl_Position=vec4(position,0.0,1.0);}'));
  gl.attachShader(program,shader(gl.FRAGMENT_SHADER,'precision mediump float;varying vec2 coordinate;void main(){gl_FragColor=vec4(coordinate,1.0,1.0);}'));
  gl.linkProgram(program);gl.useProgram(program);
  const data=new Float32Array([-.9,-.7,.8,-.7,0,.5]);
  const buffer=gl.createBuffer();gl.bindBuffer(gl.ARRAY_BUFFER,buffer);gl.bufferData(gl.ARRAY_BUFFER,data,gl.STATIC_DRAW);
  const location=gl.getAttribLocation(program,'position');gl.enableVertexAttribArray(location);gl.vertexAttribPointer(location,2,gl.FLOAT,false,0,0);
  gl.uniform2f(gl.getUniformLocation(program,'offset'),1,1);
  gl.clearColor(0,0,0,1);gl.clear(gl.COLOR_BUFFER_BIT);gl.drawArrays(gl.TRIANGLE_STRIP,0,3);
  const pixel=new Uint8Array(4);
  return JSON.stringify([121,131,141].map(x=>{gl.readPixels(x,39,1,1,gl.RGBA,gl.UNSIGNED_BYTE,pixel);return Array.from(pixel)}));
})()`, `[[207,134,255,255],[223,134,255,255],[240,134,255,255]]`)
	})
}
