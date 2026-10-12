// Extensions register behavior and state transitions together with their names.
// Merely adding a name to the native inventory is not extension support.
const extensionDefinitions = new Map(),
  extensionSlots = new WeakMap();
const extension = (name, tag, constants = {}, methods = {}, enable = () => {}) => {
  const prototype = Object.create(Object.prototype),
    constructor = {
      [tag]() {
        throw new TypeError('Illegal constructor');
      },
    }[tag];
  if (typeof markNative === 'function') markNative(constructor, tag);
  Object.defineProperty(prototype, 'constructor', {
    value: constructor,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(prototype, Symbol.toStringTag, { value: tag, configurable: true });
  for (const [key, value] of Object.entries(constants))
    Object.defineProperty(prototype, key, { value, enumerable: true });
  for (const [key, record] of Object.entries(methods)) {
    const fn = {
      [key](...args) {
        const slot = extensionSlots.get(this),
          owner = slot?.owner;
        if (!owner || slot.name !== name) throw new TypeError('Illegal invocation');
        if (args.length < record.length) throw new TypeError('Not enough arguments');
        return record.call(owner, ...args);
      },
    }[key];
    Object.defineProperty(fn, 'length', { value: record.length });
    if (typeof markNative === 'function') markNative(fn, key);
    Object.defineProperty(prototype, key, {
      value: fn,
      enumerable: true,
      writable: true,
      configurable: true,
    });
  }
  extensionDefinitions.set(name, { prototype, enable });
};
extension(
  'WEBGL_debug_renderer_info',
  'WebGLDebugRendererInfo',
  { UNMASKED_VENDOR_WEBGL: 37445, UNMASKED_RENDERER_WEBGL: 37446 },
  {},
  (s) => {
    s.debug = true;
  },
);
extension(
  'EXT_clip_control',
  'EXTClipControl',
  {
    LOWER_LEFT_EXT: 0x8ca1,
    UPPER_LEFT_EXT: 0x8ca2,
    NEGATIVE_ONE_TO_ONE_EXT: 0x935e,
    ZERO_TO_ONE_EXT: 0x935f,
    CLIP_ORIGIN_EXT: 0x935c,
    CLIP_DEPTH_MODE_EXT: 0x935d,
  },
  {
    clipControlEXT: {
      length: 2,
      call: (s, origin, depth) => {
        origin = Number(origin) >>> 0;
        depth = Number(depth) >>> 0;
        if (![0x8ca1, 0x8ca2].includes(origin) || ![0x935e, 0x935f].includes(depth)) {
          error(s, 1280);
          return;
        }
        s.clipOrigin = origin;
        s.clipDepthMode = depth;
      },
    },
  },
  (s) => {
    s.clipOrigin = 0x8ca1;
    s.clipDepthMode = 0x935e;
  },
);
extension('EXT_depth_clamp', 'EXTDepthClamp', { DEPTH_CLAMP_EXT: 0x864f }, {}, (s) => {
  s.enabled.set(0x864f, false);
});
extension(
  'EXT_polygon_offset_clamp',
  'EXTPolygonOffsetClamp',
  { POLYGON_OFFSET_CLAMP_EXT: 0x8e1b },
  {
    polygonOffsetClampEXT: {
      length: 3,
      call: (s, factor, units, clamp) => {
        s.polygonOffsetFactor = Math.fround(factor);
        s.polygonOffsetUnits = Math.fround(units);
        s.polygonOffsetClamp = Math.fround(clamp);
      },
    },
  },
);
extension('EXT_texture_filter_anisotropic', 'EXTTextureFilterAnisotropic', {
  TEXTURE_MAX_ANISOTROPY_EXT: 34046,
  MAX_TEXTURE_MAX_ANISOTROPY_EXT: 34047,
});
extension('EXT_texture_mirror_clamp_to_edge', 'EXTTextureMirrorClampToEdge', {
  MIRROR_CLAMP_TO_EDGE_EXT: 0x8743,
});
if (kind === 'webgl2')
  extension('WEBGL_stencil_texturing', 'WebGLStencilTexturing', {
    DEPTH_STENCIL_TEXTURE_MODE_WEBGL: 0x90ea,
    STENCIL_INDEX_WEBGL: 0x1901,
  });
if (kind === 'webgl2')
  extension('WEBGL_blend_func_extended', 'WebGLBlendFuncExtended', {
    SRC1_COLOR_WEBGL: 0x88f9,
    SRC1_ALPHA_WEBGL: 0x8589,
    ONE_MINUS_SRC1_COLOR_WEBGL: 0x88fa,
    ONE_MINUS_SRC1_ALPHA_WEBGL: 0x88fb,
    MAX_DUAL_SOURCE_DRAW_BUFFERS_WEBGL: 0x88fc,
  });
if (kind === 'webgl2')
  extension(
    'OVR_multiview2',
    'OVRMultiview2',
    {
      FRAMEBUFFER_ATTACHMENT_TEXTURE_NUM_VIEWS_OVR: 0x9630,
      FRAMEBUFFER_ATTACHMENT_TEXTURE_BASE_VIEW_INDEX_OVR: 0x9632,
      MAX_VIEWS_OVR: 0x9631,
      FRAMEBUFFER_INCOMPLETE_VIEW_TARGETS_OVR: 0x9633,
    },
    {
      framebufferTextureMultiviewOVR: {
        length: 6,
        call: (s, target, attachment, texture, level, baseViewIndex, numViews) => {
          target = framebufferTarget(s, target);
          if (!target) return;
          attachment = attachmentValid(s, attachment);
          if (!attachment) return;
          const framebuffer = resources.get(framebufferBinding(s, target));
          if (!framebuffer) {
            error(s, 1282);
            return;
          }
          let resourceTexture = null;
          if (texture !== null) {
            resourceTexture = resource(s, texture, 'WebGLTexture');
            if (!resourceTexture) return;
            if (resourceTexture.target !== 35866) {
              error(s, 1282);
              return;
            }
          }
          level = Number(level) | 0;
          baseViewIndex = Number(baseViewIndex) | 0;
          numViews = Number(numViews) | 0;
          if (
            level < 0 ||
            baseViewIndex < 0 ||
            numViews < 1 ||
            numViews > (profile.parameters[0x9631]?.value ?? 4)
          ) {
            error(s, 1281);
            return;
          }
          if (!framebuffer.multiview) framebuffer.multiview = new Map();
          if (texture === null) {
            framebuffer.multiview.delete(attachment);
            framebuffer.attachments.delete(attachment);
          } else {
            framebuffer.multiview.set(attachment, { texture, level, baseViewIndex, numViews });
            framebuffer.attachments.set(attachment, texture);
          }
        },
      },
    },
  );
if (kind === 'webgl2')
  extension(
    'WEBGL_clip_cull_distance',
    'WebGLClipCullDistance',
    {
      MAX_CLIP_DISTANCES_WEBGL: 0x0d32,
      MAX_CULL_DISTANCES_WEBGL: 0x82f9,
      MAX_COMBINED_CLIP_AND_CULL_DISTANCES_WEBGL: 0x82fa,
      CLIP_DISTANCE0_WEBGL: 0x3000,
      CLIP_DISTANCE1_WEBGL: 0x3001,
      CLIP_DISTANCE2_WEBGL: 0x3002,
      CLIP_DISTANCE3_WEBGL: 0x3003,
      CLIP_DISTANCE4_WEBGL: 0x3004,
      CLIP_DISTANCE5_WEBGL: 0x3005,
      CLIP_DISTANCE6_WEBGL: 0x3006,
      CLIP_DISTANCE7_WEBGL: 0x3007,
    },
    {},
    (s) => {
      for (let i = 0; i < 8; i++) s.enabled.set(0x3000 + i, false);
    },
  );
if (kind === 'webgl2') {
  const indexedBuffer = (s, index) => {
    index = Number(index) >>> 0;
    if (index >= (profile.parameters[36063]?.value || 8)) {
      error(s, 1281);
      return null;
    }
    if (!s.indexedBuffers) s.indexedBuffers = new Map();
    if (!s.indexedBuffers.has(index))
      s.indexedBuffers.set(index, {
        mask: index === 0 ? s.mask : [true, true, true, true],
        blend: !!s.enabled.get(3042),
        equationRGB: 0x8006,
        equationAlpha: 0x8006,
        srcRGB: 1,
        dstRGB: 0,
        srcAlpha: 1,
        dstAlpha: 0,
      });
    return s.indexedBuffers.get(index);
  };
  const indexedFactors = new Set([
    0, 1, 0x0300, 0x0301, 0x0302, 0x0303, 0x0304, 0x0305, 0x0306, 0x0307, 0x0308, 0x8001, 0x8002,
    0x8003, 0x8004,
  ]);
  const indexedEquation = (s, index, rgb, alpha) => {
    const state = indexedBuffer(s, index);
    if (!state) return;
    rgb = Number(rgb) >>> 0;
    alpha = Number(alpha) >>> 0;
    if (
      ![0x8006, 0x800a, 0x800b, 0x8007, 0x8008].includes(rgb) ||
      ![0x8006, 0x800a, 0x800b, 0x8007, 0x8008].includes(alpha)
    ) {
      error(s, 1280);
      return;
    }
    state.equationRGB = rgb;
    state.equationAlpha = alpha;
  };
  const indexedFactorsSet = (s, index, srcRGB, dstRGB, srcAlpha, dstAlpha) => {
    const state = indexedBuffer(s, index);
    if (!state) return;
    const values = [srcRGB, dstRGB, srcAlpha, dstAlpha].map((x) => Number(x) >>> 0);
    if (values.some((x) => !indexedFactors.has(x))) {
      error(s, 1280);
      return;
    }
    [state.srcRGB, state.dstRGB, state.srcAlpha, state.dstAlpha] = values;
  };
  extension(
    'OES_draw_buffers_indexed',
    'OESDrawBuffersIndexed',
    {},
    {
      enableiOES: {
        length: 2,
        call: (s, cap, index) => {
          if (Number(cap) !== 0x0be2) {
            error(s, 1280);
            return;
          }
          const state = indexedBuffer(s, index);
          if (state) state.blend = true;
        },
      },
      disableiOES: {
        length: 2,
        call: (s, cap, index) => {
          if (Number(cap) !== 0x0be2) {
            error(s, 1280);
            return;
          }
          const state = indexedBuffer(s, index);
          if (state) state.blend = false;
        },
      },
      blendEquationiOES: {
        length: 2,
        call: (s, index, mode) => indexedEquation(s, index, mode, mode),
      },
      blendEquationSeparateiOES: { length: 3, call: indexedEquation },
      blendFunciOES: {
        length: 3,
        call: (s, index, src, dst) => indexedFactorsSet(s, index, src, dst, src, dst),
      },
      blendFuncSeparateiOES: { length: 5, call: indexedFactorsSet },
      colorMaskiOES: {
        length: 5,
        call: (s, index, r, g, b, a) => {
          const state = indexedBuffer(s, index);
          if (state) {
            state.mask = [!!r, !!g, !!b, !!a];
            if (Number(index) === 0) s.mask = state.mask;
          }
        },
      },
    },
  );
  method('getIndexedParameter', (s, pname, index) => {
    pname = Number(pname) >>> 0;
    if (!s.extensions.has('OES_draw_buffers_indexed'))
      return fail('getIndexedParameter(' + pname + ')');
    const state = indexedBuffer(s, index);
    if (!state) return null;
    if (pname === 0x0c23) return state.mask.slice();
    if (pname === 0x0be2) return state.blend;
    if (pname === 0x8009) return state.equationRGB;
    if (pname === 0x883d) return state.equationAlpha;
    if (pname === 0x80c9) return state.srcRGB;
    if (pname === 0x80c8) return state.dstRGB;
    if (pname === 0x80cb) return state.srcAlpha;
    if (pname === 0x80ca) return state.dstAlpha;
    error(s, 1280);
    return null;
  });
}
if (kind === 'webgl2')
  extension(
    'WEBGL_multi_draw',
    'WebGLMultiDraw',
    {},
    {
      multiDrawArraysWEBGL: {
        length: 6,
        call: (s, mode, firsts, firstsOffset, counts, countsOffset, drawcount) => {
          firstsOffset = Number(firstsOffset) >>> 0;
          countsOffset = Number(countsOffset) >>> 0;
          drawcount = Number(drawcount) | 0;
          if (
            !ArrayBuffer.isView(firsts) ||
            !ArrayBuffer.isView(counts) ||
            drawcount < 0 ||
            firstsOffset + drawcount > firsts.length ||
            countsOffset + drawcount > counts.length
          ) {
            error(s, 1281);
            return;
          }
          for (let i = 0; i < drawcount; i++)
            drawVertices(s, mode, firsts[firstsOffset + i], counts[countsOffset + i]);
        },
      },
      multiDrawElementsWEBGL: {
        length: 7,
        call: (s, mode, counts, countsOffset, type, offsets, offsetsOffset, drawcount) => {
          countsOffset = Number(countsOffset) >>> 0;
          offsetsOffset = Number(offsetsOffset) >>> 0;
          drawcount = Number(drawcount) | 0;
          if (
            !ArrayBuffer.isView(counts) ||
            !ArrayBuffer.isView(offsets) ||
            drawcount < 0 ||
            countsOffset + drawcount > counts.length ||
            offsetsOffset + drawcount > offsets.length
          ) {
            error(s, 1281);
            return;
          }
          for (let i = 0; i < drawcount; i++)
            drawElements(s, mode, counts[countsOffset + i], type, offsets[offsetsOffset + i]);
        },
      },
      multiDrawArraysInstancedWEBGL: {
        length: 8,
        call: (
          s,
          mode,
          firsts,
          firstsOffset,
          counts,
          countsOffset,
          instances,
          instancesOffset,
          drawcount,
        ) => {
          firstsOffset = Number(firstsOffset) >>> 0;
          countsOffset = Number(countsOffset) >>> 0;
          instancesOffset = Number(instancesOffset) >>> 0;
          drawcount = Number(drawcount) | 0;
          if (
            !ArrayBuffer.isView(firsts) ||
            !ArrayBuffer.isView(counts) ||
            !ArrayBuffer.isView(instances) ||
            drawcount < 0 ||
            firstsOffset + drawcount > firsts.length ||
            countsOffset + drawcount > counts.length ||
            instancesOffset + drawcount > instances.length
          ) {
            error(s, 1281);
            return;
          }
          for (let i = 0; i < drawcount; i++)
            drawArraysInstanced(
              s,
              mode,
              firsts[firstsOffset + i],
              counts[countsOffset + i],
              instances[instancesOffset + i],
            );
        },
      },
      multiDrawElementsInstancedWEBGL: {
        length: 9,
        call: (
          s,
          mode,
          counts,
          countsOffset,
          type,
          offsets,
          offsetsOffset,
          instances,
          instancesOffset,
          drawcount,
        ) => {
          countsOffset = Number(countsOffset) >>> 0;
          offsetsOffset = Number(offsetsOffset) >>> 0;
          instancesOffset = Number(instancesOffset) >>> 0;
          drawcount = Number(drawcount) | 0;
          if (
            !ArrayBuffer.isView(counts) ||
            !ArrayBuffer.isView(offsets) ||
            !ArrayBuffer.isView(instances) ||
            drawcount < 0 ||
            countsOffset + drawcount > counts.length ||
            offsetsOffset + drawcount > offsets.length ||
            instancesOffset + drawcount > instances.length
          ) {
            error(s, 1281);
            return;
          }
          for (let i = 0; i < drawcount; i++)
            drawElementsInstanced(
              s,
              mode,
              counts[countsOffset + i],
              type,
              offsets[offsetsOffset + i],
              instances[instancesOffset + i],
            );
        },
      },
    },
  );
if (kind === 'webgl2')
  extension(
    'WEBGL_polygon_mode',
    'WebGLPolygonMode',
    {
      POLYGON_MODE_WEBGL: 0x0b40,
      POLYGON_OFFSET_LINE_WEBGL: 0x2a02,
      LINE_WEBGL: 0x1b01,
      FILL_WEBGL: 0x1b02,
    },
    {
      polygonModeWEBGL: {
        length: 2,
        call: (s, face, mode) => {
          if (Number(face) !== 0x0408 || ![0x1b01, 0x1b02].includes(Number(mode))) {
            error(s, 1280);
            return;
          }
          s.polygonMode = Number(mode);
        },
      },
    },
    (s) => {
      s.polygonMode = 0x1b02;
      s.enabled.set(0x2a02, false);
    },
  );
if (kind === 'webgl2')
  extension(
    'WEBGL_provoking_vertex',
    'WebGLProvokingVertex',
    {
      FIRST_VERTEX_CONVENTION_WEBGL: 0x8e4d,
      LAST_VERTEX_CONVENTION_WEBGL: 0x8e4e,
      PROVOKING_VERTEX_WEBGL: 0x8e4f,
    },
    {
      provokingVertexWEBGL: {
        length: 1,
        call: (s, value) => {
          value = Number(value) >>> 0;
          if (value !== 0x8e4d && value !== 0x8e4e) {
            error(s, 1280);
            return;
          }
          s.provokingVertex = value;
        },
      },
    },
    (s) => {
      s.provokingVertex = 0x8e4e;
    },
  );
if (kind === 'webgl')
  extension('WEBGL_color_buffer_float', 'WebGLColorBufferFloat', {
    RGBA32F_EXT: 34836,
    FRAMEBUFFER_ATTACHMENT_COMPONENT_TYPE_EXT: 33297,
    UNSIGNED_NORMALIZED_EXT: 35863,
  });
extension('EXT_color_buffer_half_float', 'EXTColorBufferHalfFloat', {
  RGBA16F_EXT: 34842,
  RGB16F_EXT: 34843,
  FRAMEBUFFER_ATTACHMENT_COMPONENT_TYPE_EXT: 33297,
  UNSIGNED_NORMALIZED_EXT: 35863,
});
if (kind === 'webgl2') extension('EXT_color_buffer_float', 'EXTColorBufferFloat');
else
  extension('OES_standard_derivatives', 'OESStandardDerivatives', {
    FRAGMENT_SHADER_DERIVATIVE_HINT_OES: 35723,
  });
if (kind === 'webgl2') extension('EXT_float_blend', 'EXTFloatBlend');
if (kind === 'webgl2') {
  extension('EXT_render_snorm', 'EXTRenderSnorm');
  extension('EXT_texture_norm16', 'EXTTextureNorm16', {
    R16_EXT: 0x822a,
    RG16_EXT: 0x822c,
    RGB16_EXT: 0x8054,
    RGBA16_EXT: 0x805b,
    R16_SNORM_EXT: 0x8f98,
    RG16_SNORM_EXT: 0x8f99,
    RGB16_SNORM_EXT: 0x8f9a,
    RGBA16_SNORM_EXT: 0x8f9b,
  });
  extension('EXT_conservative_depth', 'EXTConservativeDepth');
  extension('NV_shader_noperspective_interpolation', 'NVShaderNoperspectiveInterpolation');
  extension('OES_sample_variables', 'OESSampleVariables');
  extension('OES_shader_multisample_interpolation', 'OESShaderMultisampleInterpolation', {
    MIN_FRAGMENT_INTERPOLATION_OFFSET_OES: 0x8e5b,
    MAX_FRAGMENT_INTERPOLATION_OFFSET_OES: 0x8e5c,
    FRAGMENT_INTERPOLATION_OFFSET_BITS_OES: 0x8e5d,
  });
}
extension('KHR_parallel_shader_compile', 'KHRParallelShaderCompile', {
  COMPLETION_STATUS_KHR: 37297,
});
const timerName =
  kind === 'webgl2' ? 'EXT_disjoint_timer_query_webgl2' : 'EXT_disjoint_timer_query';
const queryClock = performance.now.bind(performance),
  timerQueryPrototype = Object.create(Object.prototype);
Object.defineProperty(timerQueryPrototype, Symbol.toStringTag, {
  value: 'WebGLTimerQueryEXT',
  configurable: true,
});
const createQuery = (s) => {
  const value = Object.create(kind === 'webgl2' ? WebGLQuery.prototype : timerQueryPrototype);
  resources.set(value, {
    owner: s,
    type: kind === 'webgl2' ? 'WebGLQuery' : 'WebGLTimerQueryEXT',
    deleted: false,
    bound: false,
    target: 0,
    available: false,
    result: 0,
    pending: false,
  });
  return value;
};
const query = (s, value) =>
  resource(s, value, kind === 'webgl2' ? 'WebGLQuery' : 'WebGLTimerQueryEXT');
const validQueryTarget = (s, target) =>
  (target === 35007 && s.extensions.has(timerName)) ||
  (kind === 'webgl2' && [35887, 36202, 35975].includes(target));
const activeQueries = (s) => s.activeQueries || (s.activeQueries = new Map());
const beginQuery = (s, target, value) => {
  target = Number(target) >>> 0;
  if (!validQueryTarget(s, target)) {
    error(s, 1280);
    return;
  }
  const q = query(s, value);
  if (!q) return;
  if (activeQueries(s).has(target) || q.active || (q.target && q.target !== target)) {
    error(s, 1282);
    return;
  }
  q.target = target;
  q.bound = true;
  q.active = true;
  q.pending = false;
  q.available = false;
  q.samplesPassed = false;
  q.started = queryClock();
  activeQueries(s).set(target, value);
};
const endQuery = (s, target) => {
  target = Number(target) >>> 0;
  if (!validQueryTarget(s, target)) {
    error(s, 1280);
    return;
  }
  const value = activeQueries(s).get(target);
  if (!value) {
    error(s, 1282);
    return;
  }
  const q = resources.get(value);
  activeQueries(s).delete(target);
  q.active = false;
  q.pending = true;
  q.elapsed = Math.max(0, Math.floor((queryClock() - q.started) * 1e6));
  (s.pendingQueries || (s.pendingQueries = new Set())).add(q);
};
const getQuery = (s, target, pname) => {
  target = Number(target) >>> 0;
  pname = Number(pname) >>> 0;
  if (pname === 34916 && s.extensions.has(timerName) && [35007, 36392].includes(target))
    return target === 35007 ? 64 : 0;
  if (!validQueryTarget(s, target) || pname !== 34917) {
    error(s, 1280);
    return null;
  }
  return activeQueries(s).get(target) || null;
};
const getQueryResult = (s, value, pname) => {
  const q = query(s, value);
  if (!q) return null;
  pname = Number(pname) >>> 0;
  if ((!q.bound && kind === 'webgl2') || q.active) {
    error(s, 1282);
    return null;
  }
  if (pname === 34919) return q.available;
  if (pname === 34918) return q.result;
  error(s, 1280);
  return null;
};
const deleteQuery = (s, value) => {
  if (value === null) return;
  const prior = resources.get(value);
  if (prior?.owner === s && prior.deleted) return;
  const q = query(s, value);
  if (!q) return;
  q.deleted = true;
  q.pending = false;
  s.pendingQueries?.delete(q);
  if (q.active) {
    activeQueries(s).delete(q.target);
    q.active = false;
  }
};
const isQuery = (s, value) => {
  const q = resources.get(value);
  return (
    !!q &&
    q.owner === s &&
    q.type === (kind === 'webgl2' ? 'WebGLQuery' : 'WebGLTimerQueryEXT') &&
    q.bound &&
    !q.deleted &&
    q.generation === (s.generation || 0)
  );
};
const queryCounter = (s, value, target) => {
  const q = query(s, value);
  if (!q) return;
  if (Number(target) !== 36392) {
    error(s, 1280);
    return;
  }
  if (q.active || (q.target && q.target !== 36392)) {
    error(s, 1282);
    return;
  }
  q.target = 36392;
  q.bound = true;
  q.available = false;
  q.pending = true;
  q.elapsed = 0;
  (s.pendingQueries || (s.pendingQueries = new Set())).add(q);
};
const queryMethods = {
  createQueryEXT: { length: 0, call: createQuery },
  deleteQueryEXT: { length: 1, call: deleteQuery },
  isQueryEXT: { length: 1, call: isQuery },
  beginQueryEXT: { length: 2, call: beginQuery },
  endQueryEXT: { length: 1, call: endQuery },
  getQueryEXT: { length: 2, call: getQuery },
  getQueryObjectEXT: { length: 2, call: getQueryResult },
  queryCounterEXT: { length: 2, call: queryCounter },
};
const queryConstants = {
  QUERY_COUNTER_BITS_EXT: 34916,
  TIME_ELAPSED_EXT: 35007,
  TIMESTAMP_EXT: 36392,
  GPU_DISJOINT_EXT: 36795,
};
if (kind === 'webgl')
  Object.assign(queryConstants, {
    CURRENT_QUERY_EXT: 34917,
    QUERY_RESULT_EXT: 34918,
    QUERY_RESULT_AVAILABLE_EXT: 34919,
  });
extension(
  timerName,
  kind === 'webgl2' ? 'EXTDisjointTimerQueryWebGL2' : 'EXTDisjointTimerQuery',
  queryConstants,
  kind === 'webgl2' ? { queryCounterEXT: queryMethods.queryCounterEXT } : queryMethods,
);
if (kind === 'webgl2')
  for (const [name, fn] of Object.entries({
    createQuery,
    deleteQuery,
    isQuery,
    beginQuery,
    endQuery,
    getQuery,
    getQueryParameter: getQueryResult,
  }))
    method(name, fn);
const flushQueries = (s) => {
  storage(s);
  const pending = [...(s.pendingQueries || [])];
  setTimeout(() => {
    for (const q of pending)
      if (q.pending && !q.deleted) {
        q.pending = false;
        q.available = true;
        q.result = [35007, 36392].includes(q.target)
          ? q.elapsed
          : q.target === 35975
            ? 0
            : Number(!!q.samplesPassed);
        s.pendingQueries.delete(q);
      }
  }, 0);
};
method('flush', flushQueries);
method('finish', flushQueries);
const restoreState = (s) => {
  s.generation = (s.generation || 0) + 1;
  s.lost = false;
  s.error = 0;
  s.restoreAllowed = false;
  s.extensions.clear();
  s.debug = false;
  s.bindings.clear();
  s.program = null;
  s.vertexArray = null;
  s.defaultVertexState = null;
  s.genericAttribs = null;
  s.drawFramebuffer = null;
  s.readFramebuffer = null;
  s.renderbuffer = null;
  s.color = [0, 0, 0, 0];
  s.mask = [true, true, true, true];
  s.viewport = [0, 0, s.dim.width, s.dim.height];
  s.scissor = s.viewport.slice();
  s.pack = 4;
  s.depthClear = 1;
  s.stencilClear = 0;
  s.depthMask = true;
  s.depthValue = 1;
  s.stencilValue = 0;
  s.stencilMaskFront = s.stencilMaskBack = 4294967295;
  s.clipOrigin = 0x8ca1;
  s.clipDepthMode = 0x935e;
  s.polygonOffsetFactor = 0;
  s.polygonOffsetUnits = 0;
  s.polygonOffsetClamp = 0;
  s.polygonMode = 0x1b02;
  s.provokingVertex = 0x8e4e;
  s.indexedBuffers?.clear();
  s.blendSrcRGB = s.blendSrcAlpha = 1;
  s.blendDstRGB = s.blendDstAlpha = 0;
  s.derivativeHint = s.mipmapHint = 4352;
  for (const key of s.enabled.keys()) s.enabled.set(key, key === 3024);
  s.enabled.delete(0x864f);
  for (let i = 0; i < 8; i++) s.enabled.delete(0x3000 + i);
  s.textureUnits?.clear();
  s.activeTexture = 0;
  s.unpackAlignment = 4;
  s.pixels = null;
  s.samplePixels = null;
  s.drawCommands = [];
  s.activeQueries?.clear();
  s.pendingQueries?.clear();
  s.drawingBufferColorSpace = s.unpackColorSpace = 'srgb';
};
extension(
  'WEBGL_lose_context',
  'WebGLLoseContext',
  {},
  {
    loseContext: {
      length: 0,
      call: (s) => {
        if (s.lost) {
          s.error = 1282;
          return;
        }
        s.lost = true;
        s.error = 37442;
        s.generation = (s.generation || 0) + 1;
        s.restoreAllowed = false;
        for (const q of s.pendingQueries || []) q.pending = false;
        setTimeout(() => {
          const event = new Event('webglcontextlost', { cancelable: true });
          Object.defineProperty(event, 'statusMessage', { value: '', enumerable: true });
          s.canvas.dispatchEvent(event);
          s.restoreAllowed = event.defaultPrevented;
        }, 0);
      },
    },
    restoreContext: {
      length: 0,
      call: (s) => {
        if (!s.lost || !s.restoreAllowed) {
          s.error = 1282;
          return;
        }
        setTimeout(() => {
          if (!s.lost) return;
          restoreState(s);
          const event = new Event('webglcontextrestored', { cancelable: true });
          Object.defineProperty(event, 'statusMessage', { value: '', enumerable: true });
          s.canvas.dispatchEvent(event);
        }, 0);
      },
    },
  },
);

extension(
  'WEBGL_debug_shaders',
  'WebGLDebugShaders',
  {},
  {
    getTranslatedShaderSource: {
      length: 1,
      call: (s, value) => {
        const shader = resource(s, value, 'WebGLShader');
        if (!shader) return '';
        return shader.compiled && shader.evaluation
          ? JSON.stringify(shader.evaluation, (key, value) =>
              value instanceof Set ? [...value] : value,
            )
          : '';
      },
    },
  },
);
