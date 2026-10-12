(() => {
  const gl = new OffscreenCanvas(4, 4).getContext('webgl2', {
    antialias: false,
    depth: false,
    stencil: false,
  });
  const clip = gl.getExtension('EXT_clip_control');
  const depth = gl.getExtension('EXT_depth_clamp');
  const polygon = gl.getExtension('EXT_polygon_offset_clamp');
  const stateBefore = [
    gl.getParameter(clip.CLIP_ORIGIN_EXT),
    gl.getParameter(clip.CLIP_DEPTH_MODE_EXT),
    gl.isEnabled(depth.DEPTH_CLAMP_EXT),
    gl.getParameter(polygon.POLYGON_OFFSET_CLAMP_EXT),
  ];
  clip.clipControlEXT(clip.UPPER_LEFT_EXT, clip.ZERO_TO_ONE_EXT);
  gl.enable(depth.DEPTH_CLAMP_EXT);
  polygon.polygonOffsetClampEXT(1, 2, 3);
  const stateAfter = [
    gl.getParameter(clip.CLIP_ORIGIN_EXT),
    gl.getParameter(clip.CLIP_DEPTH_MODE_EXT),
    gl.isEnabled(depth.DEPTH_CLAMP_EXT),
    gl.getParameter(depth.DEPTH_CLAMP_EXT),
    gl.getParameter(gl.POLYGON_OFFSET_FACTOR),
    gl.getParameter(gl.POLYGON_OFFSET_UNITS),
    gl.getParameter(polygon.POLYGON_OFFSET_CLAMP_EXT),
    gl.getError(),
  ];
  clip.clipControlEXT(0, 0);
  const invalid = [
    gl.getParameter(clip.CLIP_ORIGIN_EXT),
    gl.getParameter(clip.CLIP_DEPTH_MODE_EXT),
    gl.getError(),
  ];
  const other = new OffscreenCanvas(1, 1).getContext('webgl2');
  const otherClip = other.getExtension('EXT_clip_control');
  const otherDepth = other.getExtension('EXT_depth_clamp');
  const otherPolygon = other.getExtension('EXT_polygon_offset_clamp');
  const isolation = [
    other.getParameter(otherClip.CLIP_ORIGIN_EXT),
    other.getParameter(otherClip.CLIP_DEPTH_MODE_EXT),
    other.isEnabled(otherDepth.DEPTH_CLAMP_EXT),
    other.getParameter(otherPolygon.POLYGON_OFFSET_CLAMP_EXT),
  ];

  const raster = new OffscreenCanvas(4, 4).getContext('webgl2', {
    antialias: false,
    depth: false,
    stencil: false,
  });
  const rasterClip = raster.getExtension('EXT_clip_control');
  const rasterDepth = raster.getExtension('EXT_depth_clamp');
  const shader = (type, source) => {
    const value = raster.createShader(type);
    raster.shaderSource(value, source);
    raster.compileShader(value);
    if (!raster.getShaderParameter(value, raster.COMPILE_STATUS))
      throw new Error(raster.getShaderInfoLog(value));
    return value;
  };
  const program = raster.createProgram();
  raster.attachShader(
    program,
    shader(
      raster.VERTEX_SHADER,
      '#version 300 es\nin vec2 a;void main(){gl_Position=vec4(a,-0.5,1.);}',
    ),
  );
  raster.attachShader(
    program,
    shader(
      raster.FRAGMENT_SHADER,
      '#version 300 es\nprecision mediump float;out vec4 color;void main(){color=vec4(1.,0.,0.,1.);}',
    ),
  );
  raster.linkProgram(program);
  if (!raster.getProgramParameter(program, raster.LINK_STATUS))
    throw new Error(raster.getProgramInfoLog(program));
  raster.useProgram(program);
  const buffer = raster.createBuffer();
  raster.bindBuffer(raster.ARRAY_BUFFER, buffer);
  raster.bufferData(raster.ARRAY_BUFFER, new Float32Array([-1, 0, 1, 0, 0, 1]), raster.STATIC_DRAW);
  const location = raster.getAttribLocation(program, 'a');
  raster.enableVertexAttribArray(location);
  raster.vertexAttribPointer(location, 2, raster.FLOAT, false, 0, 0);
  const sample = () => {
    raster.clearColor(0, 0, 0, 0);
    raster.clear(raster.COLOR_BUFFER_BIT);
    raster.drawArrays(raster.TRIANGLES, 0, 3);
    const bytes = new Uint8Array(4 * 4 * 4);
    raster.readPixels(0, 0, 4, 4, raster.RGBA, raster.UNSIGNED_BYTE, bytes);
    return [
      Array.from({ length: 4 }, (_, y) =>
        Array.from({ length: 4 }, (_, x) => bytes[(y * 4 + x) * 4]),
      ),
      raster.getError(),
    ];
  };
  const lower = sample();
  rasterClip.clipControlEXT(rasterClip.UPPER_LEFT_EXT, rasterClip.ZERO_TO_ONE_EXT);
  const clipped = sample();
  raster.enable(rasterDepth.DEPTH_CLAMP_EXT);
  const clamped = sample();
  return { stateBefore, stateAfter, invalid, isolation, lower, clipped, clamped };
})()
