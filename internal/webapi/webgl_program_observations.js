// Program/resource state and lazy arithmetic readbacks. Parsed programs retain
// immutable expression trees; each draw captures its inputs before later writes.
const shaderResource = (s, value) => resource(s, value, 'WebGLShader');
const programResource = (s, value) => resource(s, value, 'WebGLProgram');
const typeEnum = (type) =>
  ({
    float: 5126,
    int: 5124,
    bool: 35670,
    vec2: 35664,
    vec3: 35665,
    vec4: 35666,
    ivec2: 35667,
    ivec3: 35668,
    ivec4: 35669,
    sampler2D: 35678,
  })[type];
method('compileShader', (s, value) => {
  const shader = shaderResource(s, value);
  if (!shader) return;
  shader.compiled = false;
  shader.log = '';
  try {
    shader.evaluation = glslObservations.compile(shader.source, shader.shaderType, s.extensions);
    shader.compiled = true;
  } catch (e) {
    shader.evaluation = null;
    if (e.unsupported) fail('compileShader: ' + e.message);
    shader.log = 'ERROR: ' + e.message;
  }
});
method('getShaderParameter', (s, value, p) => {
  const r = shaderResource(s, value);
  if (!r) return null;
  p = Number(p) >>> 0;
  if (p === 37297 && s.extensions.has('KHR_parallel_shader_compile')) return true;
  if (p === 35663) return r.shaderType;
  if (p === 35713) return !!r.compiled;
  if (p === 35712) return !!r.deleteRequested;
  error(s, 1280);
  return null;
});
method('getShaderInfoLog', (s, value) => {
  const r = shaderResource(s, value);
  return r ? r.log || '' : null;
});
method('deleteShader', (s, value) => {
  if (value === null) return;
  const r = resources.get(value);
  if (r?.owner === s && r.deleted) return;
  const own = shaderResource(s, value);
  if (!own) return;
  own.deleteRequested = true;
  if (!own.attached?.size) own.deleted = true;
});
method('createProgram', (s) => {
  const object = Object.create(WebGLProgram.prototype);
  resources.set(object, {
    owner: s,
    type: 'WebGLProgram',
    deleted: false,
    deleteRequested: false,
    shaders: [],
    linked: false,
    validated: false,
    log: '',
    bindings: new Map(),
    attributes: [],
    uniforms: [],
    values: new Map(),
  });
  return object;
});
method('isProgram', (s, value) => {
  const r = resources.get(value);
  return !!r && r.owner === s && r.type === 'WebGLProgram' && !r.deleted;
});
method('attachShader', (s, program, shader) => {
  const p = programResource(s, program),
    r = shaderResource(s, shader);
  if (!p || !r) return;
  if (p.shaders.includes(shader)) {
    error(s, 1282);
    return;
  }
  p.shaders.push(shader);
  (r.attached || (r.attached = new Set())).add(program);
});
method('detachShader', (s, program, shader) => {
  const p = programResource(s, program),
    r = shaderResource(s, shader);
  if (!p || !r) return;
  const i = p.shaders.indexOf(shader);
  if (i < 0) {
    error(s, 1282);
    return;
  }
  p.shaders.splice(i, 1);
  r.attached?.delete(program);
  if (r.deleteRequested && !r.attached?.size) r.deleted = true;
});
method('getAttachedShaders', (s, program) => {
  const p = programResource(s, program);
  return p ? p.shaders.slice() : null;
});
method('bindAttribLocation', (s, program, index, name) => {
  const p = programResource(s, program);
  if (!p) return;
  index = Number(index) >>> 0;
  name = String(name);
  if (index >= 16) {
    error(s, 1281);
    return;
  }
  if (name.startsWith('gl_')) {
    error(s, 1282);
    return;
  }
  p.bindings.set(name, index);
});
method('linkProgram', (s, program) => {
  const p = programResource(s, program);
  if (!p) return;
  p.linked = false;
  p.validated = false;
  p.log = '';
  const shaders = p.shaders.map((value) => resources.get(value)),
    vertex = shaders.filter((r) => r.shaderType === 35633),
    fragment = shaders.filter((r) => r.shaderType === 35632);
  if (vertex.length !== 1 || fragment.length !== 1 || shaders.some((r) => !r.compiled)) {
    p.log = 'Program requires one compiled vertex and fragment shader.';
    return;
  }
  p.vertex = vertex[0].evaluation;
  p.fragment = fragment[0].evaluation;
  p.varyings = p.fragment.globals
    .filter(
      (g) => (g.qualifier === 'varying' || g.qualifier === 'in') && p.fragment.used.has(g.name),
    )
    .map((g) => ({ ...g }));
  for (const varying of p.varyings) {
    const output = p.vertex.globals.find(
      (g) => (g.qualifier === 'varying' || g.qualifier === 'out') && g.name === varying.name,
    );
    if (!output || output.type !== varying.type) {
      p.log = 'Varying types differ or a fragment input has no vertex output.';
      return;
    }
  }
  p.attributes = p.vertex.globals
    .filter(
      (g) => (g.qualifier === 'attribute' || g.qualifier === 'in') && p.vertex.used.has(g.name),
    )
    .map((g) => ({ ...g }));
  const used = new Set();
  for (const a of p.attributes) {
    a.location = p.bindings.get(a.name);
    if (a.location !== undefined) {
      if (used.has(a.location)) {
        p.log = 'Attribute locations overlap.';
        return;
      }
      used.add(a.location);
    }
  }
  for (const a of p.attributes)
    if (a.location === undefined) {
      let i = 0;
      while (used.has(i)) i++;
      a.location = i;
      used.add(i);
    }
  p.uniforms = [];
  p.values = new Map();
  for (const shader of [p.vertex, p.fragment])
    for (const g of shader.globals.filter(
      (g) => g.qualifier === 'uniform' && shader.used.has(g.name),
    )) {
      const prior = p.uniforms.find((u) => u.name === g.name);
      if (prior && prior.type !== g.type) {
        p.log = 'Uniform types differ.';
        return;
      }
      if (!prior) {
        p.uniforms.push({ ...g });
        p.values.set(g.name, glslObservations.initial(g.type));
      }
    }
  p.linked = true;
});
const destroyProgram = (s, program) => {
  const p = resources.get(program);
  if (!p) return;
  p.deleted = true;
  for (const shader of p.shaders) {
    const r = resources.get(shader);
    r.attached?.delete(program);
    if (r.deleteRequested && !r.attached?.size) r.deleted = true;
  }
};
method('deleteProgram', (s, program) => {
  if (program === null) return;
  const prior = resources.get(program);
  if (prior?.owner === s && prior.deleted) return;
  const p = programResource(s, program);
  if (!p) return;
  p.deleteRequested = true;
  if (s.program !== program) destroyProgram(s, program);
});
method('useProgram', (s, program) => {
  if (program !== null) {
    const p = programResource(s, program);
    if (!p) return;
    if (!p.linked) {
      error(s, 1282);
      return;
    }
  }
  const previous = s.program;
  s.program = program;
  if (previous && previous !== program && resources.get(previous)?.deleteRequested)
    destroyProgram(s, previous);
});
method('validateProgram', (s, program) => {
  const p = programResource(s, program);
  if (p) p.validated = p.linked;
});
method('getProgramParameter', (s, program, pname) => {
  const p = programResource(s, program);
  if (!p) return null;
  switch (Number(pname) >>> 0) {
    case 37297:
      if (s.extensions.has('KHR_parallel_shader_compile')) return true;
      error(s, 1280);
      return null;
    case 35712:
      return p.deleteRequested;
    case 35714:
      return p.linked;
    case 35715:
      return p.validated;
    case 35717:
      return p.shaders.length;
    case 35721:
      return p.attributes.length;
    case 35718:
      return p.uniforms.length;
    default:
      error(s, 1280);
      return null;
  }
});
method('getProgramInfoLog', (s, program) => {
  const p = programResource(s, program);
  return p ? p.log : null;
});
method('getAttribLocation', (s, program, name) => {
  const p = programResource(s, program);
  if (!p) return -1;
  if (!p.linked) {
    error(s, 1282);
    return -1;
  }
  return p.attributes.find((a) => a.name === String(name))?.location ?? -1;
});
const activeInfo = (s, program, index, uniform) => {
  const p = programResource(s, program);
  if (!p) return null;
  const entry = (uniform ? p.uniforms : p.attributes)[Number(index) >>> 0];
  if (!entry) {
    error(s, 1281);
    return null;
  }
  const object = Object.create(WebGLActiveInfo.prototype);
  activeInfos.set(object, { name: entry.name, size: 1, type: typeEnum(entry.type) });
  return object;
};
method('getActiveAttrib', (s, program, index) => activeInfo(s, program, index, false));
method('getActiveUniform', (s, program, index) => activeInfo(s, program, index, true));
method('getUniformLocation', (s, program, name) => {
  const p = programResource(s, program);
  if (!p) return null;
  if (!p.linked) {
    error(s, 1282);
    return null;
  }
  name = String(name);
  if (!p.uniforms.some((u) => u.name === name)) return null;
  const value = Object.create(WebGLUniformLocation.prototype);
  resources.set(value, { owner: s, type: 'WebGLUniformLocation', program, name, values: p.values });
  return value;
});
method('getUniform', (s, program, location) => {
  const p = programResource(s, program),
    u = resource(s, location, 'WebGLUniformLocation');
  if (!p || !u) return null;
  if (u.program !== program || u.values !== p.values) {
    error(s, 1282);
    return null;
  }
  const v = p.values.get(u.name),
    type = p.uniforms.find((g) => g.name === u.name).type;
  return Array.isArray(v)
    ? type.startsWith('i')
      ? new Int32Array(v)
      : new Float32Array(v)
    : type === 'bool'
      ? !!v
      : v;
});
for (const count of [1, 2, 3, 4])
  for (const integer of [false, true])
    for (const array of [false, true]) {
      const name = 'uniform' + count + (integer ? 'i' : 'f') + (array ? 'v' : '');
      if (!(name in proto)) continue;
      method(name, (s, location, ...args) => {
        if (location === null) return;
        const u = resource(s, location, 'WebGLUniformLocation');
        if (!u) return;
        const p = resources.get(s.program);
        if (!p || u.program !== s.program || u.values !== p.values) {
          error(s, 1282);
          return;
        }
        const type = p.uniforms.find((g) => g.name === u.name).type,
          expected = type.includes('vec') ? Number(type.slice(-1)) : 1;
        if (
          expected !== count ||
          (type !== 'bool' &&
            (type === 'int' || type === 'sampler2D' || type.startsWith('ivec')) !== integer)
        ) {
          error(s, 1282);
          return;
        }
        if (array && args.length > 1) fail('uniform source offset');
        const input = array ? Array.from(args[0] || []) : args;
        if (input.length < count || (array && input.length % count)) {
          error(s, 1281);
          return;
        }
        const values = input.slice(0, count).map(integer ? (v) => Number(v) | 0 : Math.fround);
        p.values.set(u.name, count === 1 ? values[0] : values);
      });
    }
const attrib = (s, index) => {
  index = Number(index) >>> 0;
  if (index >= 16) {
    error(s, 1281);
    return null;
  }
  const map = vertexState(s).attribs;
  if (!s.genericAttribs) s.genericAttribs = new Map();
  if (!s.genericAttribs.has(index)) s.genericAttribs.set(index, [0, 0, 0, 1]);
  if (!map.has(index))
    map.set(index, {
      enabled: false,
      size: 4,
      type: 5126,
      normalized: false,
      stride: 0,
      offset: 0,
      buffer: null,
      divisor: 0,
      get value() {
        return s.genericAttribs.get(index);
      },
      set value(v) {
        s.genericAttribs.set(index, v);
      },
    });
  return map.get(index);
};
method('enableVertexAttribArray', (s, index) => {
  const a = attrib(s, index);
  if (a) a.enabled = true;
});
method('disableVertexAttribArray', (s, index) => {
  const a = attrib(s, index);
  if (a) a.enabled = false;
});
method('vertexAttribPointer', (s, index, size, type, normalized, stride, offset) => {
  const a = attrib(s, index);
  if (!a) return;
  size = Number(size) | 0;
  type = Number(type) >>> 0;
  stride = Number(stride) | 0;
  offset = Number(offset);
  if (size < 1 || size > 4 || stride < 0 || stride > 255 || offset < 0) {
    error(s, 1281);
    return;
  }
  if (type !== 5126) fail('vertex attribute type');
  if (stride % 4 || offset % 4) {
    error(s, 1282);
    return;
  }
  const buffer = s.bindings.get(34962);
  if (!buffer) {
    error(s, 1282);
    return;
  }
  Object.assign(a, { size, type, normalized: !!normalized, stride, offset, buffer });
});
for (const count of [1, 2, 3, 4])
  for (const array of [false, true])
    method('vertexAttrib' + count + 'f' + (array ? 'v' : ''), (s, index, ...values) => {
      const a = attrib(s, index);
      if (!a) return;
      const input = array ? Array.from(values[0] || []) : values;
      if (input.length < count) {
        error(s, 1281);
        return;
      }
      a.value = [0, 0, 0, 1];
      for (let i = 0; i < count; i++) a.value[i] = Math.fround(input[i]);
    });
method('getVertexAttrib', (s, index, pname) => {
  const a = attrib(s, index);
  if (!a) return null;
  switch (Number(pname) >>> 0) {
    case 35070:
      if (kind === 'webgl2' || s.extensions.has('ANGLE_instanced_arrays')) return a.divisor;
      error(s, 1280);
      return null;
    case 34338:
      return a.enabled;
    case 34339:
      return a.size;
    case 34340:
      return a.stride;
    case 34341:
      return a.type;
    case 34922:
      return a.normalized;
    case 34975:
      return a.buffer;
    case 34342:
      return new Float32Array(a.value);
    default:
      error(s, 1280);
      return null;
  }
});
method('getVertexAttribOffset', (s, index, pname) => {
  const a = attrib(s, index);
  if (!a) return 0;
  if (Number(pname) !== 34373) {
    error(s, 1280);
    return 0;
  }
  return a.offset;
});
const drawVertices = (s, mode, first, count, indices = null, instance = 0) => {
  const p = resources.get(s.program);
  if (!p?.linked) {
    error(s, 1282);
    return;
  }
  mode = Number(mode) >>> 0;
  first = Number(first) | 0;
  count = Number(count) | 0;
  if (first < 0 || count < 0) {
    error(s, 1281);
    return;
  }
  if (mode > 6) {
    error(s, 1280);
    return;
  }
  if (!count) return;
  if (![4, 5, 6].includes(mode)) fail('primitive mode');
  if (s.polygonMode === 0x1b01) fail('polygon line rasterization');
  if (count > 65536) fail('vertex observation limit');
  const blendEnabled = s.indexedBuffers?.get(0)?.blend ?? s.enabled.get(3042);
  let colorAttachment = null;
  if (s.drawFramebuffer) {
    colorAttachment = framebufferColor(s, s.drawFramebuffer);
    const format = colorAttachment?.internalformat || colorAttachment?.storageFormat;
    if (!colorAttachment?.pixels || !colorAttachment.width || !colorAttachment.height) {
      error(s, 1282);
      return;
    }
    if (![6408, 32856, 34836].includes(format)) fail('shader framebuffer format');
    if (blendEnabled && colorAttachment.floatPixels && !s.extensions.has('EXT_float_blend')) {
      error(s, 1282);
      return;
    }
  }
  const uniforms = { __sampleTexture: textureInputs(s, p) };
  for (const [name, value] of p.values)
    uniforms[name] = Array.isArray(value) ? value.slice() : value;
  const vertices = [];
  for (let at = first; at < first + count; at++) {
    const i = indices ? indices[at - first] : at;
    const input = { ...uniforms, gl_VertexID: i, gl_InstanceID: instance };
    for (const attribute of p.attributes) {
      const a = attrib(s, attribute.location);
      let values = a.value.slice();
      if (a.enabled) {
        const b = resources.get(a.buffer);
        if (!b || b.deleted) {
          error(s, 1282);
          return;
        }
        const offset =
          a.offset + (a.divisor ? Math.floor(instance / a.divisor) : i) * (a.stride || a.size * 4);
        const view = new DataView(b.data.buffer, b.data.byteOffset, b.data.byteLength);
        values = [0, 0, 0, 1];
        for (let j = 0; j < a.size; j++)
          values[j] =
            offset + a.size * 4 <= b.data.length ? view.getFloat32(offset + j * 4, true) : 0;
      }
      const n = Number(attribute.type.slice(-1));
      input[attribute.name] = Number.isFinite(n) ? values.slice(0, n) : values[0];
    }
    let vertexResult;
    try {
      vertexResult = glslObservations.execute(
        p.vertex,
        input,
        p.varyings.map((varying) => varying.name),
      );
    } catch (e) {
      fail('vertex evaluation: ' + e.message);
    }
    const position = vertexResult.output;
    if (!Array.isArray(position) || position.length !== 4) fail('vertex output');
    if (!position.every(Number.isFinite) || position[3] <= 0)
      fail('clip-space observation boundary');
    const depthMin = s.clipDepthMode === 0x935f ? 0 : -position[3];
    const clippedPosition = position.slice();
    if (s.enabled.get(0x864f))
      clippedPosition[2] = Math.max(depthMin, Math.min(position[3], clippedPosition[2]));
    vertices.push({ position: clippedPosition, varyings: vertexResult.captured });
  }
  if (s.enabled.get(2960) || s.enabled.get(2884)) fail('shader depth/stencil/blend observations');
  if (!s.drawFramebuffer) ensureStorage(s);
  const depthEnabled = !!s.enabled.get(2929);
  const depthSurface = depthEnabled
    ? depthTarget(
        s,
        s.drawFramebuffer,
        colorAttachment?.width ?? s.dim.width,
        colorAttachment?.height ?? s.dim.height,
      )
    : null;
  if (depthEnabled && s.drawFramebuffer && framebufferDepth(s, s.drawFramebuffer) && !depthSurface)
    fail('shader depth attachment');
  (s.drawCommands || (s.drawCommands = [])).push({
    queries: [...(s.activeQueries?.values() || [])].map((value) => resources.get(value)),
    vertices,
    mode,
    fragment: p.fragment,
    varyings: p.varyings,
    uniforms,
    viewport: s.viewport.slice(),
    clipOrigin: s.clipOrigin || 0x8ca1,
    clipDepthMode: s.clipDepthMode || 0x935e,
    depthClamp: !!s.enabled.get(0x864f),
    mask: s.mask.slice(),
    colorAttachment,
    depthEnabled,
    depthSurface,
    depthFunc: s.depthFunc,
    depthMask: s.depthMask,
    blend: !!blendEnabled,
    blendFactors: [
      s.blendSrcRGB ?? 1,
      s.blendDstRGB ?? 0,
      s.blendSrcAlpha ?? 1,
      s.blendDstAlpha ?? 0,
    ],
    scissor: s.enabled.get(3089) ? s.scissor.slice() : null,
  });
  if (!s.shaderObserved) {
    s.shaderObserved = true;
    host.semanticMissingAt(
      'webgl_program_observations.js:40',
      'WebGL.approximateShaderObservations',
    );
  }
};
method('drawArrays', (s, mode, first, count) => drawVertices(s, mode, first, count));
const blendFactorValue = (mode, source, destination, channel) => {
  if (mode === 0) return 0;
  if (mode === 1) return 1;
  if (mode === 0x0302) return source[3];
  if (mode === 0x0303) return 1 - source[3];
  if (mode === 0x0304) return destination[3];
  if (mode === 0x0305) return 1 - destination[3];
  if (mode === 0x0300) return source[channel];
  if (mode === 0x0301) return 1 - source[channel];
  if (mode === 0x0306) return destination[channel];
  if (mode === 0x0307) return 1 - destination[channel];
  fail('blend factor');
};
// Raster boundaries are approximated; ordered triangle samples share the same
// authoritative bytes used by clear, readPixels and canvas snapshots.
const materializePrograms = (s) => {
  const commands = s.drawCommands;
  if (!commands?.length) return;
  s.drawCommands = [];
  for (const command of commands) {
    const surface = command.colorAttachment || s;
    const width = command.colorAttachment ? surface.width : s.width;
    const height = command.colorAttachment ? surface.height : s.height;
    const pixels = surface.pixels;
    if (width * height > 65536) fail('shader sample observation limit');
    const samples =
      !command.colorAttachment && s.attributes.antialias
        ? [
            [3 / 8, 1 / 8],
            [7 / 8, 3 / 8],
            [1 / 8, 5 / 8],
            [5 / 8, 7 / 8],
          ]
        : [[0.5, 0.5]];
    if (!command.colorAttachment && s.attributes.antialias && !s.samplePixels)
      s.samplePixels = samples.map(() => s.pixels.slice());
    // The retained Chrome MSAA blend control resolves fractional sample color
    // only after coverage is combined. Preserve it without paying for float
    // sample storage on the common unblended path.
    if (command.blend && s.samplePixels && !(s.samplePixels[0] instanceof Float32Array))
      s.samplePixels = s.samplePixels.map((sample) => Float32Array.from(sample));
    const samplePixels = command.colorAttachment ? null : s.samplePixels;
    const drawTargets = samplePixels || [pixels];
    if (
      samplePixels &&
      command.depthEnabled &&
      command.depthSurface &&
      !command.depthSurface.sampleDepthPixels
    )
      command.depthSurface.sampleDepthPixels = samples.map(() =>
        command.depthSurface.depthPixels.slice(),
      );
    const [vx, vy, vw, vh] = command.viewport;
    const vertices = command.vertices.map(({ position: p, varyings }) => {
        const transformed = [
          ((p[0] / p[3] + 1) * vw) / 2 + vx,
          (((command.clipOrigin === 0x8ca2 ? -p[1] : p[1]) / p[3] + 1) * vh) / 2 + vy,
          p[2] / p[3],
          1 / p[3],
        ];
        transformed.varyings = varyings;
        return transformed;
      }),
      triangles = [];
    if (command.mode === 4)
      for (let i = 0; i + 2 < vertices.length; i += 3) triangles.push(vertices.slice(i, i + 3));
    if (command.mode === 5)
      for (let i = 2; i < vertices.length; i++)
        triangles.push(
          i % 2
            ? [vertices[i - 1], vertices[i - 2], vertices[i]]
            : [vertices[i - 2], vertices[i - 1], vertices[i]],
        );
    if (command.mode === 6)
      for (let i = 2; i < vertices.length; i++)
        triangles.push([vertices[0], vertices[i - 1], vertices[i]]);
    if (triangles.length * width * height > 16777216) fail('triangle sample complexity limit');
    for (const [a, b, c] of triangles) {
      if (!command.depthClamp) {
        const minimum = command.clipDepthMode === 0x935f ? 0 : -1;
        if (
          (a[2] < minimum && b[2] < minimum && c[2] < minimum) ||
          (a[2] > 1 && b[2] > 1 && c[2] > 1)
        )
          continue;
        if (a[2] < minimum || a[2] > 1 || b[2] < minimum || b[2] > 1 || c[2] < minimum || c[2] > 1)
          fail('partial clip-space observation boundary');
      }
      const area = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
      if (Math.abs(area) < 1e-12) continue;
      const edges = [
        [b, c],
        [c, a],
        [a, b],
      ];
      const topLeft = edges.map(([u, v]) => {
        if (area < 0) [u, v] = [v, u];
        return v[1] < u[1] || (v[1] === u[1] && v[0] > u[0]);
      });
      const edgeMargins = edges.map(
        ([u, v]) => (Math.abs(v[0] - u[0]) + Math.abs(v[1] - u[1])) / (2 * Math.abs(area)),
      );
      const edgeDX = edges.map(([u, v]) => (v[0] - u[0]) / area);
      const edgeDY = edges.map(([u, v]) => (v[1] - u[1]) / area);
      const weights = [0, 0, 0];
      const fragmentInputs = {
        ...command.uniforms,
        gl_FragCoord: [0, 0, 0, 0],
        gl_FrontFacing: area > 0,
      };
      const colorScratch = [0, 0, 0, 0];
      const resolvedVaryings = command.varyings.map((varying) => {
        const av = a.varyings[varying.name];
        const output = Array.isArray(av) ? new Array(av.length) : 0;
        fragmentInputs[varying.name] = output;
        return {
          name: varying.name,
          av,
          bv: b.varyings[varying.name],
          cv: c.varyings[varying.name],
          output,
        };
      });
      // A direct vec4 output can be evaluated from already-interpolated
      // vertex values. Keep the general GLSL evaluator for every other shader.
      let directColor = null;
      if (command.fragment.directColor) {
        const components = [];
        for (const arg of command.fragment.directColor) {
          if (arg.op === 'literal') {
            components.push({ constant: Math.fround(arg.value) });
            continue;
          }
          const varying = resolvedVaryings.find((entry) => entry.name === arg.name);
          if (varying) {
            if (Array.isArray(varying.av)) {
              for (let index = 0; index < varying.av.length; index++)
                components.push({ varying, index });
            } else components.push({ varying, index: -1 });
          } else if (Object.hasOwn(command.uniforms, arg.name)) {
            const value = command.uniforms[arg.name];
            for (const part of Array.isArray(value) ? value : [value])
              components.push({ constant: Math.fround(part) });
          } else {
            components.length = 0;
            break;
          }
        }
        if (components.length >= 4) directColor = components.slice(0, 4);
      }
      const depthPasses = (depth, prior) => {
        const mode = command.depthFunc;
        return mode === 512
          ? false
          : mode === 513
            ? depth < prior
            : mode === 514
              ? depth === prior
              : mode === 515
                ? depth <= prior
                : mode === 516
                  ? depth > prior
                  : mode === 517
                    ? depth !== prior
                    : mode === 518
                      ? depth >= prior
                      : true;
      };
      const minX = Math.max(0, Math.floor(Math.min(a[0], b[0], c[0]) - 1));
      const maxX = Math.min(width, Math.ceil(Math.max(a[0], b[0], c[0]) + 1));
      const minY = Math.max(0, Math.floor(Math.min(a[1], b[1], c[1]) - 1));
      const maxY = Math.min(height, Math.ceil(Math.max(a[1], b[1], c[1]) + 1));
      for (let y = minY; y < maxY; y++)
        for (let x = minX; x < maxX; x++) {
          if (command.scissor) {
            const [sx, sy, sw, sh] = command.scissor;
            if (x < sx || y < sy || x >= sx + sw || y >= sy + sh) continue;
          }
          const px = x + 0.5,
            py = y + 0.5;
          for (let edgeIndex = 0; edgeIndex < 3; edgeIndex++) {
            const u = edges[edgeIndex][0];
            weights[edgeIndex] = edgeDX[edgeIndex] * (py - u[1]) - edgeDY[edgeIndex] * (px - u[0]);
          }
          let fullyCovered = true,
            fullyOutside = false;
          for (let edgeIndex = 0; edgeIndex < 3; edgeIndex++) {
            if (weights[edgeIndex] <= edgeMargins[edgeIndex]) fullyCovered = false;
            if (weights[edgeIndex] < -edgeMargins[edgeIndex]) fullyOutside = true;
          }
          if (fullyOutside) continue;
          let covered = null;
          if (!fullyCovered) {
            covered = new Array(samples.length);
            let anyCovered = false;
            for (let sample = 0; sample < samples.length; sample++) {
              const [dx, dy] = samples[sample];
              let inside = true;
              for (let edgeIndex = 0; edgeIndex < 3; edgeIndex++) {
                const w =
                  weights[edgeIndex] +
                  edgeDX[edgeIndex] * (dy - 0.5) -
                  edgeDY[edgeIndex] * (dx - 0.5);
                if (w < 0 || (w === 0 && !topLeft[edgeIndex])) {
                  inside = false;
                  break;
                }
              }
              covered[sample] = inside;
              anyCovered ||= inside;
            }
            if (!anyCovered) continue;
          }
          const depthCoordinate = weights[0] * a[2] + weights[1] * b[2] + weights[2] * c[2],
            depth = command.clipDepthMode === 0x935f ? depthCoordinate : (depthCoordinate + 1) / 2,
            reciprocalW = weights[0] * a[3] + weights[1] * b[3] + weights[2] * c[3];
          const fragCoord = fragmentInputs.gl_FragCoord;
          fragCoord[0] = px;
          fragCoord[1] = py;
          fragCoord[2] = depth;
          fragCoord[3] = reciprocalW;
          let color;
          if (directColor) {
            for (let channel = 0; channel < 4; channel++) {
              const component = directColor[channel];
              if (Object.hasOwn(component, 'constant')) {
                colorScratch[channel] = component.constant;
              } else {
                const { av, bv, cv } = component.varying;
                const index = component.index;
                colorScratch[channel] = Math.fround(
                  (weights[0] * (index < 0 ? av : av[index]) * a[3] +
                    weights[1] * (index < 0 ? bv : bv[index]) * b[3] +
                    weights[2] * (index < 0 ? cv : cv[index]) * c[3]) /
                    reciprocalW,
                );
              }
            }
            color = colorScratch;
          } else {
            for (const varying of resolvedVaryings) {
              const { av, bv, cv } = varying;
              if (Array.isArray(av)) {
                const interpolated = varying.output;
                for (let index = 0; index < av.length; index++)
                  interpolated[index] = Math.fround(
                    (weights[0] * av[index] * a[3] +
                      weights[1] * bv[index] * b[3] +
                      weights[2] * cv[index] * c[3]) /
                      reciprocalW,
                  );
              } else
                fragmentInputs[varying.name] = Math.fround(
                  (weights[0] * av * a[3] + weights[1] * bv * b[3] + weights[2] * cv * c[3]) /
                    reciprocalW,
                );
            }
            try {
              color = glslObservations.executeColor(command.fragment, fragmentInputs, colorScratch);
            } catch (e) {
              fail('fragment evaluation: ' + e.message);
            }
          }
          if (color === null) continue;
          if (!Array.isArray(color) || color.length !== 4) fail('fragment output');
          const depthSurface = command.depthEnabled ? command.depthSurface : null;
          const depthIndex = y * width + x;
          let passedSamples = samplePixels && !fullyCovered ? covered : null;
          if (samplePixels && depthSurface) {
            passedSamples = new Array(samples.length);
            let anyPassed = false;
            for (let sample = 0; sample < samples.length; sample++) {
              if (!fullyCovered && !covered[sample]) {
                passedSamples[sample] = false;
                continue;
              }
              const prior = depthSurface.sampleDepthPixels[sample][depthIndex];
              const passed = depthPasses(depth, prior);
              passedSamples[sample] = passed;
              if (!passed) continue;
              anyPassed = true;
              if (command.depthMask) {
                const maximum = 2 ** 24 - 1;
                depthSurface.sampleDepthPixels[sample][depthIndex] =
                  Math.round(Math.max(0, Math.min(1, depth)) * maximum) / maximum;
              }
            }
            if (!anyPassed) continue;
          } else if (depthSurface) {
            if (!depthPasses(depth, depthSurface.depthPixels[depthIndex])) continue;
            if (command.depthMask) {
              const bits = depthSurface.storageFormat === 33189 ? 16 : 24;
              const maximum = 2 ** bits - 1;
              depthSurface.depthPixels[depthIndex] =
                Math.round(Math.max(0, Math.min(1, depth)) * maximum) / maximum;
            }
          }
          for (const query of command.queries) query.samplesPassed = true;
          const i = (y * width + x) * 4;
          const floatAttachment = !!command.colorAttachment?.floatPixels;
          // D3D11's RGBA8 blend unit uses quantized shader output and its
          // complement (e.g. alpha 0.5 becomes factors 127/255 and 128/255).
          // Keep floating attachments in their own unquantized domain.
          const source =
            command.blend && !floatAttachment && !samplePixels
              ? color.map(
                  (value) =>
                    Math.max(
                      0,
                      Math.ceil(Math.fround(Math.max(0, Math.min(1, value)) * 255) - 0.5),
                    ) / 255,
                )
              : color;
          if (samplePixels && !command.blend) {
            for (let k = 0; k < 4; k++)
              if (command.mask[k] && (k !== 3 || s.attributes.alpha)) {
                const value = Math.max(
                  0,
                  Math.ceil(Math.fround(Math.max(0, Math.min(1, source[k])) * 255) - 0.5),
                );
                for (let sample = 0; sample < samplePixels.length; sample++)
                  if (!passedSamples || passedSamples[sample]) samplePixels[sample][i + k] = value;
              }
            continue;
          }
          for (let sample = 0; sample < drawTargets.length; sample++) {
            if (passedSamples && !passedSamples[sample]) continue;
            const target = drawTargets[sample];
            const destination = command.blend
              ? Array.from(target.subarray(i, i + 4), (value) =>
                  floatAttachment ? value : value / 255,
                )
              : null;
            for (let k = 0; k < 4; k++)
              if (command.mask[k] && (command.colorAttachment || k !== 3 || s.attributes.alpha)) {
                let output = source[k];
                if (command.blend) {
                  const [srcRGB, dstRGB, srcAlpha, dstAlpha] = command.blendFactors;
                  output =
                    source[k] *
                      blendFactorValue(k === 3 ? srcAlpha : srcRGB, source, destination, k) +
                    destination[k] *
                      blendFactorValue(k === 3 ? dstAlpha : dstRGB, source, destination, k);
                }
                target[i + k] = floatAttachment
                  ? Math.fround(output)
                  : samplePixels
                    ? Math.fround(Math.max(0, Math.min(1, output)) * 255)
                    : Math.max(
                        0,
                        Math.ceil(Math.fround(Math.max(0, Math.min(1, output)) * 255) - 0.5),
                      );
              }
          }
        }
    }
    if (samplePixels) {
      const sampleCount = samplePixels.length;
      for (let i = 0; i < pixels.length; i++) {
        let sum = 0;
        for (let sample = 0; sample < sampleCount; sample++) sum += samplePixels[sample][i];
        pixels[i] = Math.ceil(sum / sampleCount - 0.5);
      }
    }
  }
};
