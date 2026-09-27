// Texture storage is authoritative for render passes, queue copies and mapped
// buffer reads. This bounded software path has no native graphics dependency.
const textureFormats = new Set(['rgba8unorm', 'bgra8unorm', 'rgba8unorm-srgb', 'bgra8unorm-srgb']);
const extent = (value) => {
  const a =
    Array.isArray(value) || ArrayBuffer.isView(value)
      ? value
      : [value.width, value.height, value.depthOrArrayLayers];
  return [
    number(a[0]),
    a[1] === undefined ? 1 : number(a[1]),
    a[2] === undefined ? 1 : number(a[2]),
  ];
};
const origin = (value) =>
  value === undefined
    ? [0, 0, 0]
    : Array.isArray(value)
      ? [number(value[0] || 0), number(value[1] || 0), number(value[2] || 0)]
      : [number(value.x || 0), number(value.y || 0), number(value.z || 0)];
for (const type of [
  'GPUTexture',
  'GPUTextureView',
  'GPUShaderModule',
  'GPURenderPipeline',
  'GPURenderPassEncoder',
  'GPUCanvasContext',
]) {
  const proto = globalThis[type]?.prototype;
  if (!proto) continue;
  for (const name of Object.getOwnPropertyNames(proto))
    if (
      name !== 'constructor' &&
      typeof Object.getOwnPropertyDescriptor(proto, name).value === 'function'
    )
      method(type, name, () => unsupported(type + '.' + name));
  if (type !== 'GPUCanvasContext')
    getter(
      type,
      'label',
      (s) => s.label,
      (s, v) => {
        s.label = String(v);
      },
    );
}
const makeTexture = (device, descriptor) => {
  const size = extent(descriptor.size),
    format = String(descriptor.format),
    usage = Number(descriptor.usage) >>> 0,
    mipLevelCount = number(descriptor.mipLevelCount ?? 1),
    sampleCount = number(descriptor.sampleCount ?? 1),
    dimension = String(descriptor.dimension ?? '2d');
  if (!textureFormats.has(format)) unsupported('texture format ' + format);
  if (dimension !== '2d' || size[2] !== 1 || mipLevelCount !== 1 || sampleCount !== 1)
    unsupported('texture dimensions/mipmaps/multisampling');
  const valid =
    !device.destroyed &&
    size[0] > 0 &&
    size[1] > 0 &&
    size[0] <= 8192 &&
    size[1] <= 8192 &&
    usage > 0 &&
    !(usage & ~31);
  if (!valid) validation(device, 'Invalid texture descriptor');
  if (size[0] * size[1] > 4194304) unsupported('texture observation allocation limit');
  return make('GPUTexture', {
    device,
    label: String(descriptor.label || ''),
    width: size[0],
    height: size[1],
    depthOrArrayLayers: 1,
    format,
    usage,
    mipLevelCount,
    sampleCount,
    dimension,
    valid,
    destroyed: false,
    bytes: new Uint8Array(valid ? size[0] * size[1] * 4 : 0),
  });
};
method('GPUDevice', 'createTexture', (d, descriptor) => makeTexture(d, descriptor));
for (const name of [
  'width',
  'height',
  'depthOrArrayLayers',
  'format',
  'usage',
  'mipLevelCount',
  'sampleCount',
  'dimension',
])
  getter('GPUTexture', name, (t) => t[name]);
method('GPUTexture', 'destroy', (t) => {
  t.destroyed = true;
  t.bytes = new Uint8Array(0);
});
method('GPUTexture', 'createView', (t, d = {}) => {
  if (
    (d.format !== undefined && d.format !== t.format) ||
    (d.dimension !== undefined && d.dimension !== '2d') ||
    d.baseMipLevel ||
    d.baseArrayLayer ||
    (d.mipLevelCount !== undefined && d.mipLevelCount !== 1) ||
    (d.arrayLayerCount !== undefined && d.arrayLayerCount !== 1)
  )
    unsupported('texture view reinterpretation');
  return make('GPUTextureView', { texture: t, label: String(d.label || '') });
});
method('GPUDevice', 'createShaderModule', (d, descriptor) => {
  const source = String(descriptor.code);
  let functions, error, opaqueReason;
  try {
    functions = wgslObservations.compile(source);
  } catch (e) {
    if (e.unsupported) opaqueReason = e.message;
    else {
      error = e.message;
      validation(d, error);
    }
  }
  return make('GPUShaderModule', {
    device: d,
    label: String(descriptor.label || ''),
    functions,
    error,
    opaqueReason,
  });
});
method('GPUShaderModule', 'getCompilationInfo', (s) =>
  platformPromiseResolve(
    make('GPUCompilationInfo', {
      messages: s.error
        ? [
            {
              message: s.error,
              type: 'error',
              lineNum: 1,
              linePos: 1,
              offset: 0,
              length: 0,
            },
          ]
        : [],
    }),
  ),
);
const compilationMessage = (data) => {
  const message = make('GPUCompilationMessage', data);
  // Promise delivery can project nested platform objects through an engine
  // wrapper which is not a stable WeakMap key. Keep the immutable compilation
  // record on that wrapper as WebIDL readonly data as well.
  for (const name of ['message', 'type', 'lineNum', 'linePos', 'offset', 'length'])
    Object.defineProperty(message, name, {
      value: data[name],
      enumerable: true,
      configurable: true,
    });
  return message;
};
getter('GPUCompilationInfo', 'messages', (s) => s.messages.map(compilationMessage));
for (const name of ['message', 'type', 'lineNum', 'linePos', 'offset', 'length'])
  getter('GPUCompilationMessage', name, (s) => s[name]);
const stage = (device, descriptor, kind) => {
  const module = check(descriptor.module, 'GPUShaderModule');
  if (module.device !== device || module.error) return null;
  if (module.opaqueReason) unsupported('WGSL: ' + module.opaqueReason);
  const fn = descriptor.entryPoint
    ? module.functions.get(String(descriptor.entryPoint))
    : [...module.functions.values()].find((f) => f.attrs[kind]);
  if (!fn?.attrs[kind]) return null;
  if (descriptor.constants && Object.keys(descriptor.constants).length)
    unsupported('pipeline constants');
  return fn;
};
method('GPUDevice', 'createRenderPipeline', (d, descriptor) => {
  const vertex = stage(d, descriptor.vertex, 'vertex'),
    fragment = descriptor.fragment && stage(d, descriptor.fragment, 'fragment'),
    targets = descriptor.fragment?.targets || [],
    primitive = descriptor.primitive || {};
  if (
    descriptor.vertex.buffers?.length ||
    descriptor.depthStencil ||
    descriptor.multisample?.count > 1 ||
    (primitive.topology && primitive.topology !== 'triangle-list') ||
    (primitive.cullMode && primitive.cullMode !== 'none')
  )
    unsupported('render pipeline state');
  if (targets.length !== 1 || targets[0].blend || !textureFormats.has(targets[0].format))
    unsupported('render color targets');
  const valid = !!vertex && !!fragment && !d.destroyed;
  if (!valid) validation(d, 'Invalid render pipeline');
  return make('GPURenderPipeline', {
    device: d,
    label: String(descriptor.label || ''),
    vertex,
    fragment,
    format: targets[0].format,
    writeMask: targets[0].writeMask ?? 15,
    valid,
  });
});
method('GPUDevice', 'createRenderPipelineAsync', (d, descriptor) => {
  try {
    return platformPromiseResolve(
      Reflect.apply(GPUDevice.prototype.createRenderPipeline, d.object, [descriptor]),
    );
  } catch (e) {
    return platformPromiseReject(e);
  }
});
const colorComponents = (value) =>
  Array.isArray(value) ? value : [value?.r ?? 0, value?.g ?? 0, value?.b ?? 0, value?.a ?? 0];
const encodeColor = (value, format) => {
  let c = value.map((x) => Math.max(0, Math.min(1, Number(x))));
  if (format.endsWith('-srgb'))
    c = c.map((v, i) =>
      i === 3 ? v : v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055,
    );
  const b = c.map((v) => Math.max(0, Math.ceil(Math.fround(v * 255) - 0.5)));
  return format.startsWith('bgra') ? [b[2], b[1], b[0], b[3]] : b;
};
method('GPUCommandEncoder', 'beginRenderPass', (e, descriptor) => {
  const attachments = Array.from(descriptor.colorAttachments || []);
  if (
    attachments.length !== 1 ||
    !attachments[0] ||
    descriptor.depthStencilAttachment ||
    descriptor.occlusionQuerySet
  )
    unsupported('render pass attachments');
  const a = attachments[0],
    t = check(a.view, 'GPUTextureView').texture;
  if (a.resolveTarget) unsupported('render resolve target');
  const loadOp = String(a.loadOp),
    storeOp = String(a.storeOp);
  if (!['load', 'clear'].includes(loadOp) || !['store', 'discard'].includes(storeOp))
    throw new TypeError('Invalid render pass operation');
  if (e.finished || e.passActive || t.device !== e.device || !(t.usage & 16)) {
    validation(e.device, 'Invalid render pass');
    e.valid = false;
  }
  e.passActive = true;
  return make('GPURenderPassEncoder', {
    encoder: e,
    texture: t,
    loadOp,
    storeOp,
    clear: encodeColor(colorComponents(a.clearValue), t.format),
    draws: [],
    pipeline: null,
    viewport: [0, 0, t.width, t.height, 0, 1],
    scissor: [0, 0, t.width, t.height],
    ended: false,
    label: String(descriptor.label || ''),
  });
});
const passAvailable = (p) => {
  if (p.ended || p.encoder.finished) {
    validation(p.encoder.device, 'Render pass already ended');
    p.encoder.valid = false;
    return false;
  }
  return true;
};
method('GPURenderPassEncoder', 'setPipeline', (p, value) => {
  if (!passAvailable(p)) return;
  const pipeline = check(value, 'GPURenderPipeline');
  if (
    pipeline.device !== p.encoder.device ||
    !pipeline.valid ||
    pipeline.format !== p.texture.format
  ) {
    validation(p.encoder.device, 'Invalid render pipeline binding');
    p.encoder.valid = false;
    return;
  }
  p.pipeline = pipeline;
});
method('GPURenderPassEncoder', 'setViewport', (p, x, y, w, h, min = 0, max = 1) => {
  if (!passAvailable(p)) return;
  const v = [x, y, w, h, min, max].map(Number);
  if (v.some((x) => !Number.isFinite(x)) || w < 0 || h < 0 || min < 0 || max > 1 || min > max) {
    validation(p.encoder.device, 'Invalid viewport');
    p.encoder.valid = false;
    return;
  }
  p.viewport = v;
});
method('GPURenderPassEncoder', 'setScissorRect', (p, x, y, w, h) => {
  if (!passAvailable(p)) return;
  const v = [x, y, w, h].map(number);
  if (v[0] + v[2] > p.texture.width || v[1] + v[3] > p.texture.height) {
    validation(p.encoder.device, 'Invalid scissor');
    p.encoder.valid = false;
    return;
  }
  p.scissor = v;
});
method('GPURenderPassEncoder', 'draw', (p, count, instances = 1, first = 0, firstInstance = 0) => {
  if (!passAvailable(p)) return;
  [count, instances, first, firstInstance] = [count, instances, first, firstInstance].map(number);
  if (!p.pipeline) {
    validation(p.encoder.device, 'No render pipeline');
    p.encoder.valid = false;
    return;
  }
  if (count * instances > 65536) unsupported('render vertex observation limit');
  p.draws.push({
    pipeline: p.pipeline,
    count,
    instances,
    first,
    firstInstance,
    viewport: p.viewport.slice(),
    scissor: p.scissor.slice(),
  });
});
method('GPURenderPassEncoder', 'end', (p) => {
  if (!passAvailable(p)) return;
  p.ended = true;
  p.encoder.passActive = false;
  p.encoder.commands.push({
    textureCommand: true,
    kind: 'render',
    texture: p.texture,
    loadOp: p.loadOp,
    storeOp: p.storeOp,
    clear: p.clear,
    draws: p.draws.slice(),
  });
});
const copyRegion = (texture, descriptor, size) => {
  if (descriptor.mipLevel || (descriptor.aspect && descriptor.aspect !== 'all'))
    unsupported('texture copy subresource');
  const start = origin(descriptor.origin),
    shape = extent(size);
  return {
    start,
    shape,
    valid: start.every((v, i) => v + shape[i] <= [texture.width, texture.height, 1][i]),
  };
};
method('GPUCommandEncoder', 'copyTextureToBuffer', (e, source, destination, size) => {
  const texture = check(source.texture, 'GPUTexture'),
    b = check(destination.buffer, 'GPUBuffer'),
    region = copyRegion(texture, source, size),
    offset = number(destination.offset ?? 0),
    bytesPerRow = number(destination.bytesPerRow ?? region.shape[0] * 4),
    rowsPerImage = number(destination.rowsPerImage ?? region.shape[1]);
  const valid =
    !e.finished &&
    !e.passActive &&
    texture.device === e.device &&
    b.device === e.device &&
    !!(texture.usage & 1) &&
    !!(b.usage & 8) &&
    region.valid &&
    offset % 4 === 0 &&
    bytesPerRow >= region.shape[0] * 4 &&
    bytesPerRow % 256 === 0 &&
    rowsPerImage >= region.shape[1] &&
    offset + (region.shape[1] - 1) * bytesPerRow + region.shape[0] * 4 <= b.size;
  if (!valid) {
    validation(e.device, 'Invalid texture to buffer copy');
    e.valid = false;
    return;
  }
  e.commands.push({
    textureCommand: true,
    kind: 'textureToBuffer',
    texture,
    b,
    offset,
    bytesPerRow,
    ...region,
  });
});
method('GPUCommandEncoder', 'copyTextureToTexture', (e, source, destination, size) => {
  const texture = check(source.texture, 'GPUTexture'),
    target = check(destination.texture, 'GPUTexture'),
    a = copyRegion(texture, source, size),
    b = copyRegion(target, destination, size);
  if (
    e.finished ||
    e.passActive ||
    texture.device !== e.device ||
    target.device !== e.device ||
    texture.format !== target.format ||
    !(texture.usage & 1) ||
    !(target.usage & 2) ||
    !a.valid ||
    !b.valid ||
    texture === target
  ) {
    validation(e.device, 'Invalid texture copy');
    e.valid = false;
    return;
  }
  e.commands.push({
    textureCommand: true,
    kind: 'textureToTexture',
    texture,
    target,
    start: a.start,
    destination: b.start,
    shape: a.shape,
  });
});
method('GPUQueue', 'writeTexture', (q, destination, data, layout, size) => {
  const t = check(destination.texture, 'GPUTexture'),
    r = copyRegion(t, destination, size),
    view = ArrayBuffer.isView(data);
  if (!view && !(data instanceof ArrayBuffer)) throw new TypeError('Expected buffer source');
  const bytes = view
      ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
      : new Uint8Array(data),
    offset = number(layout.offset ?? 0),
    row = number(layout.bytesPerRow ?? r.shape[0] * 4);
  if (
    t.device !== q.device ||
    !t.valid ||
    t.destroyed ||
    !(t.usage & 2) ||
    !r.valid ||
    row < r.shape[0] * 4 ||
    offset + (r.shape[1] - 1) * row + r.shape[0] * 4 > bytes.length
  ) {
    validation(q.device, 'Invalid texture write');
    return;
  }
  for (let y = 0; y < r.shape[1]; y++)
    t.bytes.set(
      bytes.subarray(offset + y * row, offset + y * row + r.shape[0] * 4),
      ((r.start[1] + y) * t.width + r.start[0]) * 4,
    );
});
const validateTextureCommand = (op, device) =>
  op.texture.device === device &&
  op.texture.valid &&
  !op.texture.destroyed &&
  (op.kind !== 'textureToBuffer' || available(op.b, device, 8)) &&
  (op.kind !== 'textureToTexture' ||
    (op.target.device === device && op.target.valid && !op.target.destroyed));
const renderTexture = (t, draw) => {
  const p = draw.pipeline,
    [vx, vy, vw, vh] = draw.viewport,
    [sx, sy, sw, sh] = draw.scissor;
  if (t.width * t.height * draw.count * draw.instances > 16777216)
    unsupported('render sample complexity limit');
  for (let instance = 0; instance < draw.instances; instance++) {
    const vertices = [];
    for (let i = 0; i < draw.count; i++) {
      const v = wgslObservations.execute(p.vertex, {
        vertex_index: draw.first + i,
        instance_index: draw.firstInstance + instance,
      });
      if (
        !Array.isArray(v) ||
        v.length !== 4 ||
        !v.every(Number.isFinite) ||
        v[3] <= 0 ||
        Math.abs(v[0]) > v[3] ||
        Math.abs(v[1]) > v[3] ||
        v[2] < 0 ||
        v[2] > v[3]
      )
        unsupported('WGSL clip-space observation');
      vertices.push([
        vx + ((v[0] / v[3] + 1) * vw) / 2,
        vy + ((1 - v[1] / v[3]) * vh) / 2,
        v[2] / v[3],
      ]);
    }
    for (let i = 0; i + 2 < vertices.length; i += 3) {
      const [a, b, c] = vertices.slice(i, i + 3),
        area = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
      if (!area) continue;
      for (let y = Math.max(0, sy); y < Math.min(t.height, sy + sh); y++)
        for (let x = Math.max(0, sx); x < Math.min(t.width, sx + sw); x++) {
          const px = x + 0.5,
            py = y + 0.5,
            edge = (u, v) => (v[0] - u[0]) * (py - u[1]) - (v[1] - u[1]) * (px - u[0]),
            weights = [edge(b, c) / area, edge(c, a) / area, edge(a, b) / area];
          const edges = [
            [b, c],
            [c, a],
            [a, b],
          ];
          if (
            weights.some((w, i) => {
              if (w < 0) return true;
              if (w !== 0) return false;
              let [u, v] = edges[i];
              if (area < 0) [u, v] = [v, u];
              const dx = v[0] - u[0],
                dy = v[1] - u[1];
              return !(dy < 0 || (dy === 0 && dx > 0));
            })
          )
            continue;
          const value = wgslObservations.execute(p.fragment, {
            position: [px, py, weights[0] * a[2] + weights[1] * b[2] + weights[2] * c[2], 1],
            front_facing: area < 0,
          });
          if (value === null) continue;
          if (!Array.isArray(value) || value.length !== 4) unsupported('WGSL fragment output');
          const bytes = encodeColor(value, t.format),
            offset = (y * t.width + x) * 4;
          for (let k = 0; k < 4; k++)
            if (p.writeMask & (1 << (t.format.startsWith('bgra') && k !== 3 ? 2 - k : k)))
              t.bytes[offset + k] = bytes[k];
        }
    }
  }
};
const executeTextureCommand = (op) => {
  const t = op.texture;
  if (op.kind === 'textureToBuffer' || op.kind === 'textureToTexture') {
    for (let y = 0; y < op.shape[1]; y++) {
      const start = ((op.start[1] + y) * t.width + op.start[0]) * 4;
      if (op.kind === 'textureToBuffer')
        op.b.bytes.set(
          t.bytes.subarray(start, start + op.shape[0] * 4),
          op.offset + y * op.bytesPerRow,
        );
      else
        op.target.bytes.set(
          t.bytes.subarray(start, start + op.shape[0] * 4),
          ((op.destination[1] + y) * op.target.width + op.destination[0]) * 4,
        );
    }
    return;
  }
  if (op.loadOp === 'clear') for (let i = 0; i < t.bytes.length; i += 4) t.bytes.set(op.clear, i);
  for (const draw of op.draws) renderTexture(t, draw);
  if (op.storeOp === 'discard') t.bytes.fill(0);
};
// The canvas binding owns a configuration and current texture; unrelated
// offscreen textures use the same storage and queue operations.
const discardCurrent = (s) => {
  if (s.current) {
    const t = slots.get(s.current);
    t.destroyed = true;
    t.bytes = new Uint8Array(0);
  }
  s.current = null;
};
// Chrome retains the last image consumed by a canvas snapshot after unconfigure;
// unread GPU commands do not create that historical image. Keep the snapshot at
// this projection boundary, separately from the invalidated live GPU resource.
if (typeof canvasCompatibilityState !== 'undefined')
  canvasCompatibilityState.registerContext('webgpu', (canvas, attributes, dim) => {
    const object = make('GPUCanvasContext', { canvas, dim, configuration: null, current: null }),
      s = slots.get(object);
    dim.onResize = () => {
      discardCurrent(s);
      s.retainedImage = null;
    };
    dim.onTransfer = () => {
      discardCurrent(s);
      s.retainedImage = null;
    };
    dim.readPixels = () => {
      if (s.retainedImage && !s.current) return s.retainedImage.slice();
      const out = new Uint8ClampedArray(dim.width * dim.height * 4),
        t = s.current && slots.get(s.current);
      if (t && !t.destroyed) {
        for (let i = 0; i < out.length; i += 4) {
          const b = t.bytes,
            a = s.configuration.alphaMode === 'opaque' ? 255 : b[i + 3];
          out[i] = b[i + (t.format.startsWith('bgra') ? 2 : 0)];
          out[i + 1] = b[i + 1];
          out[i + 2] = b[i + (t.format.startsWith('bgra') ? 0 : 2)];
          out[i + 3] = a;
        }
      }
      s.retainedImage = out;
      return out;
    };
    return object;
  });
getter('GPUCanvasContext', 'canvas', (s) => s.canvas);
method('GPUCanvasContext', 'configure', (s, d) => {
  const device = check(d.device, 'GPUDevice'),
    format = String(d.format);
  if (!textureFormats.has(format)) throw new TypeError('Invalid canvas format');
  discardCurrent(s);
  s.retainedImage = null;
  s.configuration = {
    device: d.device,
    format,
    usage: d.usage === undefined ? 16 : Number(d.usage) >>> 0,
    viewFormats: Array.from(d.viewFormats || []),
    colorSpace: d.colorSpace ?? 'srgb',
    toneMapping: { mode: d.toneMapping?.mode ?? 'standard' },
    alphaMode: d.alphaMode ?? 'opaque',
  };
  s.device = device;
});
method('GPUCanvasContext', 'getConfiguration', (s) =>
  s.configuration
    ? {
        ...s.configuration,
        viewFormats: s.configuration.viewFormats.slice(),
        toneMapping: { ...s.configuration.toneMapping },
      }
    : null,
);
method('GPUCanvasContext', 'unconfigure', (s) => {
  discardCurrent(s);
  s.configuration = null;
});
method('GPUCanvasContext', 'getCurrentTexture', (s) => {
  if (!s.configuration)
    throw platformDOMException('Context is not configured', 'InvalidStateError');
  if (
    !s.current ||
    slots.get(s.current).width !== s.dim.width ||
    slots.get(s.current).height !== s.dim.height
  )
    s.current = makeTexture(s.device, {
      size: [s.dim.width, s.dim.height],
      format: s.configuration.format,
      usage: s.configuration.usage,
    });
  return s.current;
});
