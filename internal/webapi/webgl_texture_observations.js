// Texture objects own level bytes. Binding units and parameters reference those
// objects; uploads copy author memory, and later sampling sees ordered writes.
const textureBindingTarget = (s, target) => {
  target = Number(target) >>> 0;
  if ([3553, 34067].includes(target) || (kind === 'webgl2' && [32879, 35866].includes(target)))
    return target;
  error(s, 1280);
  return 0;
};
const textureUnit = (s) => {
  if (!s.textureUnits) s.textureUnits = new Map();
  const index = s.activeTexture || 0;
  if (!s.textureUnits.has(index)) s.textureUnits.set(index, new Map());
  return s.textureUnits.get(index);
};
const textureBound = (s, target, image = false) => {
  target = Number(target) >>> 0;
  const binding =
    image && target >= 34069 && target <= 34074 ? 34067 : textureBindingTarget(s, target);
  if (!binding) return null;
  const value = textureUnit(s).get(binding);
  if (!value) {
    error(s, 1282);
    return null;
  }
  return resources.get(value);
};
method('createTexture', (s) => {
  const value = Object.create(WebGLTexture.prototype);
  resources.set(value, {
    owner: s,
    type: 'WebGLTexture',
    deleted: false,
    bound: false,
    target: 0,
    levels: new Map(),
    parameters: new Map([
      [10240, 9729],
      [10241, 9986],
      [10242, 10497],
      [10243, 10497],
      ...(kind === 'webgl2'
        ? [
            [32882, 10497],
            [33084, 0],
            [33085, 1000],
            [33082, -1000],
            [33083, 1000],
            [34892, 0],
            [34893, 515],
            [37167, false],
            [0x90ea, 0x1902],
          ]
        : []),
    ]),
  });
  return value;
});
method('isTexture', (s, value) => {
  const r = resources.get(value);
  return !!r && r.owner === s && r.type === 'WebGLTexture' && r.bound && !r.deleted;
});
method('activeTexture', (s, value) => {
  const unit = (Number(value) >>> 0) - 33984,
    limit = profile.parameters[35661]?.value || 32;
  if (unit < 0 || unit >= limit) {
    error(s, 1280);
    return;
  }
  s.activeTexture = unit;
});
method('bindTexture', (s, target, value) => {
  target = textureBindingTarget(s, target);
  if (!target) return;
  if (value !== null) {
    const r = resource(s, value, 'WebGLTexture');
    if (!r) return;
    if (r.target && r.target !== target) {
      error(s, 1282);
      return;
    }
    r.target = target;
    r.bound = true;
  }
  textureUnit(s).set(target, value);
});
method('deleteTexture', (s, value) => {
  if (value === null) return;
  const prior = resources.get(value);
  if (prior?.owner === s && prior.type === 'WebGLTexture' && prior.deleted) return;
  const r = resource(s, value, 'WebGLTexture');
  if (!r) return;
  r.deleted = true;
  r.levels.clear();
  for (const unit of s.textureUnits?.values() || [])
    for (const [target, bound] of unit) if (bound === value) unit.set(target, null);
});
method('getTexParameter', (s, target, pname) => {
  const t = textureBound(s, target);
  if (!t) return null;
  pname = Number(pname) >>> 0;
  if (pname === 34046 && s.extensions.has('EXT_texture_filter_anisotropic'))
    return t.parameters.get(pname) || 1;
  if (pname === 0x90ea && !s.extensions.has('WEBGL_stencil_texturing')) {
    error(s, 1280);
    return null;
  }
  if (!t.parameters.has(pname)) {
    error(s, 1280);
    return null;
  }
  return t.parameters.get(pname);
});
const textureParameter = (s, target, pname, value, integer) => {
  const t = textureBound(s, target);
  if (!t) return;
  pname = Number(pname) >>> 0;
  value = integer ? Number(value) | 0 : Math.fround(value);
  if (pname === 34046 && s.extensions.has('EXT_texture_filter_anisotropic')) {
    if (value < 1) {
      error(s, 1281);
      return;
    }
    t.parameters.set(pname, Math.min(value, profile.anisotropy));
    return;
  }
  if (
    !t.parameters.has(pname) ||
    pname === 37167 ||
    (pname === 0x90ea && !s.extensions.has('WEBGL_stencil_texturing'))
  ) {
    error(s, 1280);
    return;
  }
  const allowed =
    pname === 10240
      ? [9728, 9729]
      : pname === 10241
        ? [9728, 9729, 9984, 9985, 9986, 9987]
        : [10242, 10243, 32882].includes(pname)
          ? [
              10497,
              33071,
              33648,
              ...(s.extensions.has('EXT_texture_mirror_clamp_to_edge') ? [0x8743] : []),
            ]
          : pname === 0x90ea
            ? [0x1902, 0x1901]
            : pname === 34892
              ? [0, 34894]
              : pname === 34893
                ? [512, 513, 514, 515, 516, 517, 518, 519]
                : null;
  if (allowed && !allowed.includes(value)) {
    error(s, 1280);
    return;
  }
  if ([33084, 33085].includes(pname) && value < 0) {
    error(s, 1281);
    return;
  }
  t.parameters.set(pname, value);
};
method('texParameteri', (s, t, p, v) => textureParameter(s, t, p, v, true));
method('texParameterf', (s, t, p, v) => textureParameter(s, t, p, v, false));
const textureImageTarget = (s, target) => {
  target = Number(target) >>> 0;
  if (target === 3553 || (target >= 34069 && target <= 34074)) return target;
  error(s, 1280);
  return 0;
};
const textureDimensions = (s, target, level, width, height, border) => {
  const limit =
    target === 3553 ? graphics.maxTextureSize : profile.parameters[34076]?.value || 16384;
  if (
    level < 0 ||
    level > Math.floor(Math.log2(limit)) ||
    width < 0 ||
    height < 0 ||
    width > Math.max(1, limit >> level) ||
    height > Math.max(1, limit >> level) ||
    border !== 0 ||
    (target !== 3553 && width !== height)
  ) {
    error(s, 1281);
    return false;
  }
  if (width * height > 16777216) fail('texture allocation limit');
  return true;
};
const halfFloat = (value) => {
  const sign = value & 32768 ? -1 : 1,
    exponent = (value >> 10) & 31,
    mantissa = value & 1023;
  return (
    sign *
    (exponent === 31
      ? mantissa
        ? NaN
        : Infinity
      : exponent === 0
        ? mantissa * 2 ** -24
        : (1 + mantissa / 1024) * 2 ** (exponent - 15))
  );
};
const halfScratch = new DataView(new ArrayBuffer(4));
const toHalfFloat = (value) => {
  halfScratch.setFloat32(0, value);
  const bits = halfScratch.getUint32(0),
    sign = (bits >>> 16) & 32768,
    raw = (bits >>> 23) & 255,
    mantissa = bits & 8388607,
    exp = raw - 112;
  if (raw === 255) return sign | (mantissa ? 32256 : 31744);
  if (exp >= 31) return sign | 31744;
  const round = (v, n) => {
    if (n > 31) return 0;
    const q = v >>> n,
      r = v & ((1 << n) - 1),
      h = 1 << (n - 1);
    return q + (r > h || (r === h && q & 1) ? 1 : 0);
  };
  if (exp <= 0) return sign | round(mantissa | 8388608, 14 - exp);
  return sign + ((exp << 10) + round(mantissa, 13));
};
const textureUnpack = (s, width, height, format, type, value) => {
  const floating = type === 5126 || type === 5131 || type === 36193;
  if (
    (type === 5126 && kind === 'webgl' && !s.extensions.has('OES_texture_float')) ||
    (type === 36193 && (kind !== 'webgl' || !s.extensions.has('OES_texture_half_float'))) ||
    (type === 5131 && kind !== 'webgl2')
  ) {
    error(s, 1280);
    return null;
  }
  if (![5121, 5126, 5131, 36193].includes(type) || ![6406, 6407, 6408, 6409, 6410].includes(format))
    return fail('texture pixel format');
  const components = { 6406: 1, 6407: 3, 6408: 4, 6409: 1, 6410: 2 }[format],
    pixels = floating ? new Float32Array(width * height * 4) : new Uint8Array(width * height * 4),
    unit = type === 5126 ? 4 : type === 5121 ? 1 : 2,
    alpha = floating ? 1 : 255;
  if (value === null) return pixels;
  const Expected = type === 5126 ? Float32Array : type === 5121 ? Uint8Array : Uint16Array;
  if (!(value instanceof Expected)) {
    if (ArrayBuffer.isView(value)) {
      error(s, 1282);
      return null;
    }
    throw new TypeError('Expected ArrayBufferView');
  }
  const alignment = s.unpackAlignment || 4,
    stride = (Math.ceil((width * components * unit) / alignment) * alignment) / unit;
  if (value.length < (height ? stride * (height - 1) + width * components : 0)) {
    error(s, 1282);
    return null;
  }
  const read = (i) => (unit === 2 ? halfFloat(value[i]) : value[i]);
  for (let y = 0; y < height; y++)
    for (let x = 0; x < width; x++) {
      const i = y * stride + x * components,
        o = (y * width + x) * 4;
      if (format === 6406) {
        pixels[o + 3] = read(i);
        continue;
      }
      if (format === 6409 || format === 6410)
        pixels.set([read(i), read(i), read(i), format === 6410 ? read(i + 1) : alpha], o);
      else
        pixels.set([read(i), read(i + 1), read(i + 2), format === 6408 ? read(i + 3) : alpha], o);
    }
  return pixels;
};
method(
  'texImage2D',
  (s, target, level, internalformat, width, height, border, format, type, value, ...extra) => {
    if (value === undefined) return fail('TexImageSource upload');
    target = textureImageTarget(s, target);
    if (!target) return;
    const t = textureBound(s, target, true);
    if (!t) return;
    [level, width, height, border] = [level, width, height, border].map((v) => Number(v) | 0);
    [internalformat, format, type] = [internalformat, format, type].map((v) => Number(v) >>> 0);
    if (!textureDimensions(s, target, level, width, height, border)) return;
    if (
      internalformat !== format &&
      !(kind === 'webgl2' && [32856, 34842, 34836].includes(internalformat) && format === 6408)
    ) {
      error(s, 1282);
      return;
    }
    if (
      kind === 'webgl2' &&
      ((internalformat === 34836 && type !== 5126) ||
        (internalformat === 34842 && ![5126, 5131].includes(type)) ||
        (![34836, 34842].includes(internalformat) && type !== 5121))
    ) {
      error(s, 1282);
      return;
    }
    if (extra.length) return fail('texture source offset');
    const pixels = textureUnpack(s, width, height, format, type, value);
    if (pixels && internalformat === 34842)
      for (let i = 0; i < pixels.length; i++) pixels[i] = halfFloat(toHalfFloat(pixels[i]));
    if (pixels)
      t.levels.set(target + ':' + level, {
        width,
        height,
        format,
        internalformat,
        type,
        pixels,
        floatPixels: type !== 5121,
      });
  },
);
if (kind === 'webgl2')
  method(
    'texImage3D',
    (
      s,
      target,
      level,
      internalformat,
      width,
      height,
      depth,
      border,
      format,
      type,
      value,
      ...extra
    ) => {
      target = Number(target) >>> 0;
      if (target !== 35866) {
        error(s, 1280);
        return;
      }
      const texture = textureBound(s, target);
      if (!texture) return;
      [level, width, height, depth, border] = [level, width, height, depth, border].map(
        (v) => Number(v) | 0,
      );
      if (!textureDimensions(s, 3553, level, width, height, border)) return;
      if (depth < 0 || depth > (profile.parameters[35071]?.value || 2048)) {
        error(s, 1281);
        return;
      }
      if (Number(internalformat) !== 32856 || Number(format) !== 6408 || Number(type) !== 5121) {
        error(s, 1282);
        return;
      }
      if (extra.length) fail('texture 3D source offset');
      if (width * height * depth > 16777216) fail('texture 3D allocation limit');
      const pixels = textureUnpack(s, width, height * depth, Number(format), Number(type), value);
      if (pixels)
        texture.levels.set(target + ':' + level, {
          width,
          height,
          depth,
          format: 6408,
          internalformat: 32856,
          type: 5121,
          pixels,
          floatPixels: false,
        });
    },
  );
method('texSubImage2D', (s, target, level, x, y, width, height, format, type, value, ...extra) => {
  if (value === undefined) return fail('TexImageSource subupload');
  target = textureImageTarget(s, target);
  if (!target) return;
  const t = textureBound(s, target, true);
  if (!t) return;
  [level, x, y, width, height] = [level, x, y, width, height].map((v) => Number(v) | 0);
  const image = t.levels.get(target + ':' + level);
  if (!image) {
    error(s, 1282);
    return;
  }
  if (
    x < 0 ||
    y < 0 ||
    width < 0 ||
    height < 0 ||
    x + width > image.width ||
    y + height > image.height
  ) {
    error(s, 1281);
    return;
  }
  if (
    Number(format) !== image.format ||
    (kind === 'webgl2' && image.internalformat === 34842
      ? ![5126, 5131].includes(Number(type))
      : Number(type) !== image.type)
  ) {
    error(s, 1282);
    return;
  }
  if (extra.length) return fail('texture source offset');
  const pixels = textureUnpack(s, width, height, Number(format), Number(type), value);
  if (pixels && image.internalformat === 34842)
    for (let i = 0; i < pixels.length; i++) pixels[i] = halfFloat(toHalfFloat(pixels[i]));
  if (pixels)
    for (let row = 0; row < height; row++)
      image.pixels.set(
        pixels.subarray(row * width * 4, (row + 1) * width * 4),
        ((row + y) * image.width + x) * 4,
      );
});
// Each draw retains the image/parameters it sampled, so author uploads between
// draws cannot retroactively change a pending observation.
const textureInputs = (s, program) => {
  const units = new Map();
  for (const uniform of program.uniforms) {
    if (uniform.type !== 'sampler2D') continue;
    const index = program.values.get(uniform.name),
      value = s.textureUnits?.get(index)?.get(3553),
      t = resources.get(value),
      base = t?.parameters.get(33084) || 0,
      image = t?.levels.get('3553:' + base);
    const mipComplete =
      !!image &&
      Array.from(
        { length: Math.floor(Math.log2(Math.max(1, image.width, image.height))) + 1 },
        (_, i) => {
          const level = t.levels.get('3553:' + (base + i));
          return (
            level &&
            level.width === Math.max(1, image.width >> i) &&
            level.height === Math.max(1, image.height >> i) &&
            level.format === image.format
          );
        },
      ).every(Boolean);
    if (image?.compressed && !image.pixels && image.width && image.height)
      image.pixels = new Float32Array(
        host.decodeTextureBlocks(image.format, image.width, image.height, Array.from(image.blocks)),
      );
    units.set(
      index,
      image
        ? {
            ...image,
            mipComplete,
            pixels: image.pixels?.slice() || new Uint8Array(0),
            parameters: new Map(t.parameters),
          }
        : null,
    );
  }
  return (unit, uv) => {
    const image = units.get(unit);
    if (!image || !image.width || !image.height) return [0, 0, 0, 1];
    const p = image.parameters,
      wrapS = p.get(10242),
      wrapT = p.get(10243),
      filter = p.get(10240),
      min = p.get(10241),
      power = (v) => (v & (v - 1)) === 0;
    if (
      (image.type === 5126 &&
        image.internalformat !== 34842 &&
        !s.extensions.has('OES_texture_float_linear') &&
        ![9728, 9984, 9986].includes(min)) ||
      (image.type === 5126 &&
        image.internalformat !== 34842 &&
        !s.extensions.has('OES_texture_float_linear') &&
        filter === 9729) ||
      (kind === 'webgl' &&
        image.type === 36193 &&
        !s.extensions.has('OES_texture_half_float_linear') &&
        (filter === 9729 || ![9728, 9984, 9986].includes(min)))
    )
      return [0, 0, 0, 1];
    if (
      kind === 'webgl' &&
      (!power(image.width) || !power(image.height)) &&
      (wrapS !== 33071 || wrapT !== 33071 || ![9728, 9729].includes(min))
    )
      return [0, 0, 0, 1];
    if (![9728, 9729].includes(min) && Math.max(image.width, image.height) > 1) {
      if (image.mipComplete) fail('mipmap texture filtering');
      return [0, 0, 0, 1];
    }
    if ([9728, 9729].includes(min) && min !== filter)
      fail('distinct texture minification/magnification filters');
    const wrap = (i, n, mode) =>
      mode === 33071
        ? Math.max(0, Math.min(n - 1, i))
        : mode === 0x8743
          ? Math.max(0, Math.min(n - 1, i < 0 ? -i - 1 : i))
          : mode === 33648
            ? (() => {
                i = ((i % (2 * n)) + 2 * n) % (2 * n);
                return i < n ? i : 2 * n - 1 - i;
              })()
            : ((i % n) + n) % n;
    const read = (x, y) => {
      const at = (wrap(y, image.height, wrapT) * image.width + wrap(x, image.width, wrapS)) * 4;
      return Array.from(image.pixels.subarray(at, at + 4), (v) =>
        image.compressed || image.floatPixels ? v : v / 255,
      );
    };
    const x = uv[0] * image.width,
      y = uv[1] * image.height;
    if (filter === 9728) return read(Math.floor(x), Math.floor(y));
    const x0 = Math.floor(x - 0.5),
      y0 = Math.floor(y - 0.5),
      fx = x - 0.5 - x0,
      fy = y - 0.5 - y0,
      a = read(x0, y0),
      b = read(x0 + 1, y0),
      c = read(x0, y0 + 1),
      d = read(x0 + 1, y0 + 1);
    return a.map(
      (v, i) => (v * (1 - fx) + b[i] * fx) * (1 - fy) + (c[i] * (1 - fx) + d[i] * fx) * fy,
    );
  };
};
const compressedFormats = new Map();
const compressedExtension = (name, tag, constants) => {
  extension(name, tag, constants);
  for (const format of Object.values(constants))
    compressedFormats.set(format, {
      extension: name,
      block: [33776, 33777, 35916, 35917, 36283, 36284].includes(format) ? 8 : 16,
      s3tc: (format >= 33776 && format <= 33779) || (format >= 35916 && format <= 35919),
    });
};
compressedExtension('WEBGL_compressed_texture_s3tc', 'WebGLCompressedTextureS3TC', {
  COMPRESSED_RGB_S3TC_DXT1_EXT: 33776,
  COMPRESSED_RGBA_S3TC_DXT1_EXT: 33777,
  COMPRESSED_RGBA_S3TC_DXT3_EXT: 33778,
  COMPRESSED_RGBA_S3TC_DXT5_EXT: 33779,
});
compressedExtension('WEBGL_compressed_texture_s3tc_srgb', 'WebGLCompressedTextureS3TCsRGB', {
  COMPRESSED_SRGB_S3TC_DXT1_EXT: 35916,
  COMPRESSED_SRGB_ALPHA_S3TC_DXT1_EXT: 35917,
  COMPRESSED_SRGB_ALPHA_S3TC_DXT3_EXT: 35918,
  COMPRESSED_SRGB_ALPHA_S3TC_DXT5_EXT: 35919,
});
compressedExtension('EXT_texture_compression_rgtc', 'EXTTextureCompressionRGTC', {
  COMPRESSED_RED_RGTC1_EXT: 36283,
  COMPRESSED_SIGNED_RED_RGTC1_EXT: 36284,
  COMPRESSED_RED_GREEN_RGTC2_EXT: 36285,
  COMPRESSED_SIGNED_RED_GREEN_RGTC2_EXT: 36286,
});
compressedExtension('EXT_texture_compression_bptc', 'EXTTextureCompressionBPTC', {
  COMPRESSED_RGBA_BPTC_UNORM_EXT: 36492,
  COMPRESSED_SRGB_ALPHA_BPTC_UNORM_EXT: 36493,
  COMPRESSED_RGB_BPTC_SIGNED_FLOAT_EXT: 36494,
  COMPRESSED_RGB_BPTC_UNSIGNED_FLOAT_EXT: 36495,
});
const compressedBytes = (s, format, width, height, value, offset = 0, length = 0) => {
  const definition = compressedFormats.get(format);
  if (!definition || !s.extensions.has(definition.extension)) {
    error(s, 1280);
    return null;
  }
  if (!ArrayBuffer.isView(value)) throw new TypeError('Expected ArrayBufferView');
  offset = Number(offset);
  length = Number(length);
  if (
    offset < 0 ||
    length < 0 ||
    offset > value.byteLength ||
    (length && offset + length > value.byteLength)
  ) {
    error(s, 1281);
    return null;
  }
  const source = new Uint8Array(
      value.buffer,
      value.byteOffset + offset,
      length || value.byteLength - offset,
    ),
    size = Math.ceil(width / 4) * Math.ceil(height / 4) * definition.block;
  if (source.length !== size) {
    error(s, 1281);
    return null;
  }
  return source.slice();
};
method(
  'compressedTexImage2D',
  (s, target, level, format, width, height, border, value, ...extra) => {
    target = textureImageTarget(s, target);
    if (!target) return;
    const t = textureBound(s, target, true);
    if (!t) return;
    [level, width, height, border] = [level, width, height, border].map((v) => Number(v) | 0);
    format = Number(format) >>> 0;
    if (!textureDimensions(s, target, level, width, height, border)) return;
    const definition = compressedFormats.get(format);
    if (kind === 'webgl' && level > 0 && (width & (width - 1) || height & (height - 1))) {
      error(s, 1281);
      return;
    }
    const alignment = Math.max(1, 4 >> Math.min(level, 2));
    if (width % alignment || height % alignment) {
      error(s, 1282);
      return;
    }
    const blocks = compressedBytes(s, format, width, height, value, ...extra);
    if (blocks)
      t.levels.set(target + ':' + level, {
        width,
        height,
        format,
        internalformat: format,
        compressed: true,
        blocks,
        pixels: null,
      });
  },
);
method(
  'compressedTexSubImage2D',
  (s, target, level, x, y, width, height, format, value, ...extra) => {
    target = textureImageTarget(s, target);
    if (!target) return;
    const t = textureBound(s, target, true);
    if (!t) return;
    [level, x, y, width, height] = [level, x, y, width, height].map((v) => Number(v) | 0);
    format = Number(format) >>> 0;
    const image = t.levels.get(target + ':' + level),
      definition = compressedFormats.get(format);
    if (!image || image.format !== format || !definition) {
      error(s, 1282);
      return;
    }
    if (
      x < 0 ||
      y < 0 ||
      width < 0 ||
      height < 0 ||
      x + width > image.width ||
      y + height > image.height
    ) {
      error(s, 1281);
      return;
    }
    if (
      x % 4 ||
      y % 4 ||
      (width % 4 && x + width !== image.width) ||
      (height % 4 && y + height !== image.height)
    ) {
      error(s, 1282);
      return;
    }
    const source = compressedBytes(s, format, width, height, value, ...extra);
    if (!source) return;
    const stride = Math.ceil(image.width / 4) * definition.block,
      rowBytes = Math.ceil(width / 4) * definition.block;
    for (let row = 0; row < Math.ceil(height / 4); row++)
      image.blocks.set(
        source.subarray(row * rowBytes, (row + 1) * rowBytes),
        (row + y / 4) * stride + (x / 4) * definition.block,
      );
    image.pixels = null;
  },
);
if (kind === 'webgl') {
  extension('OES_texture_float', 'OESTextureFloat');
  extension('OES_texture_half_float', 'OESTextureHalfFloat', { HALF_FLOAT_OES: 36193 });
  extension('OES_texture_half_float_linear', 'OESTextureHalfFloatLinear');
}
extension('OES_texture_float_linear', 'OESTextureFloatLinear');
