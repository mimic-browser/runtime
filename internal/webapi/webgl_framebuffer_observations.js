// Renderbuffer attachment state is independent of default-buffer observations.
// Texture attachments, multisampling and attachment pixel execution stay explicit.
const renderFormats = {
  32854: [4, 4, 4, 4, 0, 0],
  36194: [5, 6, 5, 0, 0, 0],
  32855: [5, 5, 5, 1, 0, 0],
  33189: [0, 0, 0, 0, 16, 0],
  36168: [0, 0, 0, 0, 0, 8],
  34041: [0, 0, 0, 0, 24, 8],
  32856: [8, 8, 8, 8, 0, 0],
  6408: [8, 8, 8, 8, 0, 0],
  32849: [8, 8, 8, 0, 0, 0],
  33321: [8, 0, 0, 0, 0, 0],
  33323: [8, 8, 0, 0, 0, 0],
  33325: [16, 0, 0, 0, 0, 0],
  33327: [16, 16, 0, 0, 0, 0],
  34842: [16, 16, 16, 16, 0, 0],
  34843: [16, 16, 16, 0, 0, 0],
  33326: [32, 0, 0, 0, 0, 0],
  33328: [32, 32, 0, 0, 0, 0],
  34836: [32, 32, 32, 32, 0, 0],
  35898: [11, 11, 10, 0, 0, 0],
  36759: [8, 8, 8, 8, 0, 0],
  32859: [16, 16, 16, 16, 0, 0],
};
const framebufferTarget = (s, t) => {
  t = Number(t) >>> 0;
  if (t === 36160 || (kind === 'webgl2' && (t === 36008 || t === 36009))) return t;
  error(s, 1280);
  return 0;
};
const framebufferBinding = (s, t) => (t === 36008 ? s.readFramebuffer : s.drawFramebuffer);
const attachmentValid = (s, a) => {
  a = Number(a) >>> 0;
  if (
    [36064, 36096, 36128, 33306].includes(a) ||
    (kind === 'webgl2' && a >= 36064 && a < 36064 + (profile.parameters[36063]?.value || 8))
  )
    return a;
  error(s, 1280);
  return 0;
};
for (const [name, klass] of [
  ['Framebuffer', WebGLFramebuffer],
  ['Renderbuffer', WebGLRenderbuffer],
]) {
  method('create' + name, (s) => {
    const value = Object.create(klass.prototype);
    resources.set(value, {
      owner: s,
      type: 'WebGL' + name,
      deleted: false,
      bound: false,
      attachments: new Map(),
      attachmentImages: name === 'Framebuffer' ? new Map() : undefined,
      width: 0,
      height: 0,
      format: 32854,
      storageFormat: 0,
    });
    return value;
  });
  method('is' + name, (s, value) => {
    const r = resources.get(value);
    return !!r && r.owner === s && r.type === 'WebGL' + name && !r.deleted && r.bound;
  });
  method('delete' + name, (s, value) => {
    if (value === null) return;
    const r = resources.get(value);
    if (r?.owner === s && r.type === 'WebGL' + name && r.deleted) return;
    const own = resource(s, value, 'WebGL' + name);
    if (!own) return;
    own.deleted = true;
    if (name === 'Framebuffer') {
      if (s.drawFramebuffer === value) s.drawFramebuffer = null;
      if (s.readFramebuffer === value) s.readFramebuffer = null;
      const attached = [...own.attachments.values()];
      own.attachments.clear();
      for (const texture of attached) releaseTextureAttachment(value, texture);
      own.attachmentImages.clear();
      own.multiview?.clear();
    } else {
      if (s.renderbuffer === value) s.renderbuffer = null;
      for (const f of new Set([s.drawFramebuffer, s.readFramebuffer])) {
        const data = resources.get(f);
        if (data)
          for (const [a, b] of data.attachments) if (b === value) data.attachments.delete(a);
      }
    }
  });
}
method('bindFramebuffer', (s, t, value) => {
  t = framebufferTarget(s, t);
  if (!t) return;
  if (value !== null) {
    const r = resource(s, value, 'WebGLFramebuffer');
    if (!r) return;
    r.bound = true;
  }
  if (t !== 36008) s.drawFramebuffer = value;
  if (t !== 36009) s.readFramebuffer = value;
});
method('bindRenderbuffer', (s, t, value) => {
  if (Number(t) !== 36161) {
    error(s, 1280);
    return;
  }
  if (value !== null) {
    const r = resource(s, value, 'WebGLRenderbuffer');
    if (!r) return;
    r.bound = true;
  }
  s.renderbuffer = value;
});
method('renderbufferStorage', (s, t, format, width, height) => {
  if (Number(t) !== 36161) {
    error(s, 1280);
    return;
  }
  const r = resources.get(s.renderbuffer);
  if (!r) {
    error(s, 1282);
    return;
  }
  format = Number(format) >>> 0;
  width = Number(width) | 0;
  height = Number(height) | 0;
  const limit = profile.parameters[34024]?.value || 16384;
  if (width < 0 || height < 0 || width > limit || height > limit) {
    error(s, 1281);
    return;
  }
  const base = [32854, 36194, 32855, 33189, 36168, 34041].includes(format),
    half = [33325, 33327, 34842].includes(format),
    floating = [33326, 33328, 34836, 35898].includes(format);
  const colorFloat =
    s.extensions.has('WEBGL_color_buffer_float') || s.extensions.has('OES_texture_float');
  if (kind === 'webgl' && !base) {
    if ([34842, 34843].includes(format) && s.extensions.has('EXT_color_buffer_half_float')) {
      // The WebGL extension accepts the format before the underlying half-float
      // storage capability validates it. Failed allocation retains prior storage.
      if (format === 34843 || !s.extensions.has('OES_texture_half_float')) {
        r.format = format;
        error(s, 1280);
        return;
      }
    } else if (format !== 34836 || !colorFloat) {
      error(s, 1280);
      return;
    }
  } else if (
    !renderFormats[format] ||
    (format === 36759 && !s.extensions.has('EXT_render_snorm')) ||
    (format === 32859 && !s.extensions.has('EXT_texture_norm16')) ||
    format === 34843 ||
    (half &&
      !s.extensions.has('EXT_color_buffer_half_float') &&
      !s.extensions.has('EXT_color_buffer_float')) ||
    (floating && !s.extensions.has('EXT_color_buffer_float'))
  ) {
    error(s, 1280);
    return;
  }
  materializePrograms(s);
  Object.assign(r, {
    format,
    storageFormat: format,
    width,
    height,
    pixels:
      format === 34836
        ? new Float32Array(width * height * 4)
        : format === 32856
          ? new Uint8Array(width * height * 4)
          : null,
    floatPixels: format === 34836,
    depthPixels: [33189, 34041].includes(format) ? new Float32Array(width * height).fill(1) : null,
  });
});
method('getRenderbufferParameter', (s, t, p) => {
  if (Number(t) !== 36161) {
    error(s, 1280);
    return null;
  }
  const r = resources.get(s.renderbuffer);
  if (!r) {
    error(s, 1282);
    return null;
  }
  p = Number(p) >>> 0;
  if (p === 36162) return r.width;
  if (p === 36163) return r.height;
  if (p === 36164) return r.format;
  if (p === 36011 && kind === 'webgl2') return 0;
  const i = [36176, 36177, 36178, 36179, 36180, 36181].indexOf(p);
  if (i >= 0) return r.storageFormat ? renderFormats[r.storageFormat][i] : 0;
  error(s, 1280);
  return null;
});
method('framebufferRenderbuffer', (s, t, a, rt, value) => {
  t = framebufferTarget(s, t);
  if (!t) return;
  a = attachmentValid(s, a);
  if (!a) return;
  if (Number(rt) !== 36161) {
    error(s, 1280);
    return;
  }
  const f = resources.get(framebufferBinding(s, t));
  if (!f) {
    error(s, 1282);
    return;
  }
  if (value !== null && !resource(s, value, 'WebGLRenderbuffer')) return;
  const keys = a === 33306 ? [36096, 36128] : [a],
    old = keys.map((key) => f.attachments.get(key));
  for (const key of keys)
    if (value === null) f.attachments.delete(key);
    else f.attachments.set(key, value);
  for (const key of keys) {
    f.attachmentImages.delete(key);
    f.multiview?.delete(key);
  }
  for (const previous of old) releaseTextureAttachment(framebufferBinding(s, t), previous);
});
method('framebufferTexture2D', (s, t, a, imageTarget, value, level) => {
  t = framebufferTarget(s, t);
  if (!t) return;
  a = attachmentValid(s, a);
  if (!a) return;
  imageTarget = textureImageTarget(s, imageTarget);
  if (!imageTarget) return;
  level = Number(level) | 0;
  if (level < 0 || (kind === 'webgl' && level !== 0)) {
    error(s, 1281);
    return;
  }
  const f = resources.get(framebufferBinding(s, t));
  if (!f) {
    error(s, 1282);
    return;
  }
  if (value !== null) {
    const texture = resource(s, value, 'WebGLTexture');
    if (!texture) return;
    const binding = imageTarget === 3553 ? 3553 : 34067;
    if (texture.target !== binding) {
      error(s, 1282);
      return;
    }
  }
  const old = f.attachments.get(a);
  if (value === null) {
    f.attachments.delete(a);
    f.attachmentImages.delete(a);
  } else {
    f.attachments.set(a, value);
    f.attachmentImages.set(a, { target: imageTarget, level });
    resources.get(value).framebuffers.add(framebufferBinding(s, t));
  }
  releaseTextureAttachment(framebufferBinding(s, t), old);
  f.multiview?.delete(a);
});
const framebufferColor = (s, binding) => {
  const framebuffer = resources.get(binding),
    value = framebuffer?.attachments.get(36064),
    resource = resources.get(value),
    image = framebuffer?.attachmentImages.get(36064);
  return image ? resource?.levels.get(image.target + ':' + image.level) : resource;
};
const framebufferDepth = (s, binding) => {
  const framebuffer = resources.get(binding),
    value = framebuffer?.attachments.get(36096),
    resource = resources.get(value),
    image = framebuffer?.attachmentImages.get(36096);
  return image ? resource?.levels.get(image.target + ':' + image.level) : resource;
};
const depthTarget = (s, binding, width, height) => {
  if (binding) {
    const depth = framebufferDepth(s, binding);
    return depth?.depthPixels?.length === width * height ? depth : null;
  }
  if (!s.attributes.depth) return null;
  if (!s.depthPixels || s.depthWidth !== width || s.depthHeight !== height) {
    s.depthPixels = new Float32Array(width * height).fill(1);
    s.depthWidth = width;
    s.depthHeight = height;
  }
  return s;
};
method('checkFramebufferStatus', (s, t) => {
  t = framebufferTarget(s, t);
  if (!t) return 0;
  const f = resources.get(framebufferBinding(s, t));
  if (!f) return 36053;
  if (!f.attachments.size) return 36055;
  let width, height;
  for (const [a, value] of f.attachments) {
    const multiview = f.multiview?.get(a);
    if (multiview) {
      const texture = resources.get(value);
      const image = texture?.levels.get('35866:' + multiview.level);
      if (!image || image.depth < multiview.baseViewIndex + multiview.numViews) return 36054;
      if (width !== undefined && (width !== image.width || height !== image.height)) return 36057;
      width = image.width;
      height = image.height;
      continue;
    }
    const r = resources.get(value),
      image = f.attachmentImages.get(a),
      level = image ? r.levels.get(image.target + ':' + image.level) : r,
      bits = renderFormats[level?.internalformat || level?.storageFormat] || [0, 0, 0, 0, 0, 0];
    if (
      !level?.width ||
      !level?.height ||
      (a >= 36064 && a < 36096 && !bits[0]) ||
      (a === 36096 && !bits[4]) ||
      (a === 36128 && !bits[5])
    )
      return 36054;
    if (
      kind === 'webgl' &&
      width !== undefined &&
      (width !== level.width || height !== level.height)
    )
      return 36057;
    width = level.width;
    height = level.height;
  }
  return 36053;
});
method('getFramebufferAttachmentParameter', (s, t, a, p) => {
  t = framebufferTarget(s, t);
  if (!t) return null;
  a = attachmentValid(s, a);
  if (!a) return null;
  const f = resources.get(framebufferBinding(s, t));
  if (!f) return fail('default framebuffer attachment query');
  const value = f.attachments.get(a === 33306 ? 36096 : a),
    r = resources.get(value);
  p = Number(p) >>> 0;
  const multiview = f.multiview?.get(a);
  if (multiview) {
    if (p === 0x9630) return multiview.numViews;
    if (p === 0x9632) return multiview.baseViewIndex;
    if (p === 36048) return 5890;
    if (p === 36049) return value;
    if (p === 36050) return multiview.level;
  }
  const componentEnabled =
    kind === 'webgl2' ||
    s.extensions.has('EXT_color_buffer_half_float') ||
    s.extensions.has('WEBGL_color_buffer_float') ||
    s.extensions.has('OES_texture_float');
  if (
    (p === 33297 && !componentEnabled) ||
    (p === 33296 && kind !== 'webgl2' && !s.extensions.has('EXT_sRGB'))
  ) {
    error(s, 1280);
    return null;
  }
  if (p === 36048) return value ? (f.attachmentImages.has(a) ? 5890 : 36161) : 0;
  if (p === 36049) return value || null;
  if (!value) {
    error(s, 1282);
    return null;
  }
  if (f.attachmentImages.has(a)) {
    const image = f.attachmentImages.get(a),
      level = r.levels.get(image.target + ':' + image.level);
    if (p === 36050) return image.level;
    if (p === 36051) return image.target === 3553 ? 0 : image.target;
    if (p === 33296) return 9729;
    if (p === 33297) return level?.floatPixels ? 5126 : 35863;
    const i = [33298, 33299, 33300, 33301, 33302, 33303].indexOf(p);
    if (i >= 0 && kind === 'webgl2')
      return level ? (renderFormats[level.internalformat] || [0, 0, 0, 0, 0, 0])[i] : 0;
  }
  if (p === 33296) return 9729;
  if (p === 33297) {
    if (a === 33306) {
      error(s, 1282);
      return null;
    }
    if (!r.storageFormat) return 0;
    if (r.storageFormat === 36759) return 36764;
    return [33325, 33327, 34842, 34843, 33326, 33328, 34836, 35898].includes(r.storageFormat)
      ? 5126
      : 35863;
  }
  const i = [33298, 33299, 33300, 33301, 33302, 33303].indexOf(p);
  if (i >= 0 && kind === 'webgl2') return r.storageFormat ? renderFormats[r.storageFormat][i] : 0;
  error(s, 1280);
  return null;
});
