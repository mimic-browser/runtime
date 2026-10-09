(function (host) {
  'use strict';
  /* shared_promise_intrinsics */
  /* dev_preview_state */
  let hostToken = host.token();
  // Snapshot restoration rebinds host state without replacing canonical objects.
  const bootstrapRestoreHooks = [];
  const bootstrapCallbacks = [];
  const registerBootstrapCallback = (name, ...callbacks) => {
    bootstrapCallbacks.push([name, callbacks]);
    host[name](...callbacks);
  };
  // Generated fallback operations have no implementation until they are read.
  // Their descriptor is published at the original position; after all profile
  // and handwritten installers run, V8 replaces the placeholder with a lazy
  // data slot. This leaves authored methods and accessor semantics untouched.
  const generatedLazyInstaller =
    typeof host.createGeneratedLazyInstaller === 'function'
      ? host.createGeneratedLazyInstaller
      : null;
  const generatedLazyOperations = [];
  const generatedLazyOperationIndex = new Map();
  const stageGeneratedLazyOperation = (target, interfaceName, memberName, domain) => {
    if (!generatedLazyInstaller) return false;
    const id = generatedLazyOperations.length;
    const record = {
      target,
      interfaceName,
      memberName,
      functionName: memberName,
      domain,
      length: 0,
    };
    generatedLazyOperations.push(record);
    generatedLazyOperationIndex.set(interfaceName + '\0' + memberName, record);
    Object.defineProperty(target, memberName, {
      value: undefined,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    return true;
  };
  const installGeneratedLazyOperations = () => {
    if (!generatedLazyInstaller || generatedLazyOperations.length === 0) return;
    const materialize = (id) => {
      const record = generatedLazyOperations[id];
      if (!record) throw new RangeError('Unknown generated WebAPI member');
      const { interfaceName, memberName, functionName, domain, length } = record;
      const implementation = domain
        ? {
            [functionName](...args) {
              return invokeLazyBehavior(interfaceName, memberName, this, args, undefined, 'value');
            },
          }[functionName]
        : {
            [functionName]() {
              return host.semanticMissingAt(
                'surface.js/generatedLazyOperation',
                interfaceName + '.' + memberName,
              );
            },
          }[functionName];
      if (implementation.length !== length)
        Object.defineProperty(implementation, 'length', { value: length, configurable: true });
      const value = platformOperation(implementation, functionName, length);
      markNative(value, functionName);
      return value;
    };
    generatedLazyInstaller(materialize);
    // A restored context has its own native registry even though its JS graph
    // was deserialized from the bootstrap snapshot.
    bootstrapRestoreHooks.push(() => host.createGeneratedLazyInstaller(materialize));
    for (let id = 0; id < generatedLazyOperations.length; id++) {
      const record = generatedLazyOperations[id];
      const { target, memberName } = record;
      const descriptor = Object.getOwnPropertyDescriptor(target, memberName);
      // A later semantic installer may have replaced or removed the fallback.
      if (
        !descriptor ||
        !('value' in descriptor) ||
        descriptor.value !== undefined ||
        !descriptor.configurable ||
        !descriptor.writable
      ) {
        generatedLazyOperations[id] = null;
        continue;
      }
      const attributes = descriptor.enumerable ? 0 : 2;
      if (!generatedLazyInstaller(target, memberName, id, attributes))
        throw new Error(
          'Failed to install generated lazy operation ' + record.interfaceName + '.' + memberName,
        );
      record.target = null;
    }
    generatedLazyOperationIndex.clear();
  };
  // Shape is eager; implementation is not. These realm-local cells keep every
  // public function/accessor and its descriptor stable across materialization.
  const lazyDomainInterfaces = new Map();
  const lazyDomainState = new Map();
  const lazyBehaviorCells = new Map();
  const lazySingletons = new Map();
  const nativeEval = globalThis.eval;
  const lazyKey = (interfaceName, member, side = 'value') =>
    interfaceName + '\0' + member + '\0' + side;
  const defineLazyDomain = (name, interfaces) => {
    lazyDomainState.set(name, { status: 'cold', error: undefined });
    for (const interfaceName of interfaces) lazyDomainInterfaces.set(interfaceName, name);
  };
  const ensureLazyDomain = (name) => {
    const state = lazyDomainState.get(name);
    if (!state || state.status === 'ready') return;
    if (state.status === 'failed') throw state.error;
    // Re-entry sees the cells already published and never a partial public
    // shape. Implementations which recurse during installation fail clearly.
    if (state.status === 'loading') return;
    // A page can replace eval. Never hand its replacement a privileged code
    // object; leave the domain cold so restoring the intrinsic can recover.
    if (globalThis.eval !== nativeEval) throw new TypeError('Browser eval was replaced');
    state.status = 'loading';
    try {
      const source = host.lazyDomainSource(name);
      eval(host.internalEvalCodeObjects ? internalEvalCode(source) : source);
      state.status = 'ready';
    } catch (error) {
      state.error = error;
      state.status = 'failed';
      throw error;
    }
  };
  const bindLazyBehavior = (interfaceName, member, implementation, side = 'value') => {
    const cell = lazyBehaviorCells.get(lazyKey(interfaceName, member, side));
    if (!cell) return false;
    cell.implementation = implementation;
    return true;
  };
  const invokeLazyBehavior = (interfaceName, member, receiver, args, newTarget, side) => {
    const domain = lazyDomainInterfaces.get(interfaceName),
      cell = lazyBehaviorCells.get(lazyKey(interfaceName, member, side || 'value'));
    if (!domain || !cell)
      return host.semanticMissingAt('surface.js/lazyBehavior', interfaceName + '.' + member);
    ensureLazyDomain(domain);
    const implementation = cell.implementation;
    if (typeof implementation !== 'function') {
      host.semanticMissingAt('surface.js/lazyBehavior', interfaceName + '.' + member);
      if (member === 'constructor') throw new TypeError('Illegal constructor');
      return;
    }
    return newTarget
      ? Reflect.construct(implementation, args, newTarget)
      : Reflect.apply(implementation, receiver, args);
  };
  const requireLazySingleton = (domain, name) => {
    ensureLazyDomain(domain);
    if (!lazySingletons.has(domain + '\0' + name))
      throw new Error('lazy domain did not publish singleton ' + domain + '.' + name);
    return lazySingletons.get(domain + '\0' + name);
  };
  const publishLazySingleton = (domain, name, value) =>
    lazySingletons.set(domain + '\0' + name, value);
  const installLazyShape = () => {
    for (const interfaceName of ['GPUSupportedFeatures', 'WGSLLanguageFeatures']) {
      const prototype = globalThis[interfaceName]?.prototype;
      if (!prototype || Object.hasOwn(prototype, Symbol.iterator)) continue;
      const iterator = function values() {
        return invokeLazyBehavior(interfaceName, '@@iterator', this, [], undefined, 'value');
      };
      lazyBehaviorCells.set(lazyKey(interfaceName, '@@iterator'), {
        implementation: undefined,
      });
      Object.defineProperty(prototype, Symbol.iterator, {
        value: iterator,
        writable: true,
        configurable: true,
      });
    }
  };
  const installLazySingletonBindings = () => {
    if (!globalThis.Navigator?.prototype || !globalThis.GPU) return;
    const get = function () {
      if (this !== nav) throw new TypeError('Illegal invocation');
      return requireLazySingleton('webgpu', 'gpu');
    };
    markNative(get, 'gpu', 'get ');
    Object.defineProperty(globalThis.Navigator.prototype, 'gpu', {
      get,
      enumerable: true,
      configurable: true,
    });
  };
  defineLazyDomain('audio', [
    'AnalyserNode',
    'AudioBuffer',
    'AudioBufferSourceNode',
    'AudioContext',
    'AudioDestinationNode',
    'AudioListener',
    'AudioNode',
    'AudioParam',
    'AudioScheduledSourceNode',
    'AudioWorkletNode',
    'BaseAudioContext',
    'BiquadFilterNode',
    'ChannelMergerNode',
    'ChannelSplitterNode',
    'ConstantSourceNode',
    'ConvolverNode',
    'DelayNode',
    'DynamicsCompressorNode',
    'GainNode',
    'IIRFilterNode',
    'MediaElementAudioSourceNode',
    'MediaStreamAudioDestinationNode',
    'MediaStreamAudioSourceNode',
    'OfflineAudioCompletionEvent',
    'OfflineAudioContext',
    'OscillatorNode',
    'PannerNode',
    'PeriodicWave',
    'ScriptProcessorNode',
    'StereoPannerNode',
    'WaveShaperNode',
  ]);
  defineLazyDomain('webgl', [
    'WebGLActiveInfo',
    'WebGLBuffer',
    'WebGLContextEvent',
    'WebGLFramebuffer',
    'WebGLProgram',
    'WebGLQuery',
    'WebGLRenderbuffer',
    'WebGLRenderingContext',
    'WebGL2RenderingContext',
    'WebGLSampler',
    'WebGLShader',
    'WebGLShaderPrecisionFormat',
    'WebGLSync',
    'WebGLTexture',
    'WebGLTransformFeedback',
    'WebGLUniformLocation',
    'WebGLVertexArrayObject',
  ]);
  defineLazyDomain('webgpu', [
    'GPU',
    'GPUAdapter',
    'GPUAdapterInfo',
    'GPUBindGroup',
    'GPUBindGroupLayout',
    'GPUBuffer',
    'GPUCanvasContext',
    'GPUCommandBuffer',
    'GPUCommandEncoder',
    'GPUCompilationInfo',
    'GPUCompilationMessage',
    'GPUComputePassEncoder',
    'GPUComputePipeline',
    'GPUDevice',
    'GPUDeviceLostInfo',
    'GPUError',
    'GPUExternalTexture',
    'GPUInternalError',
    'GPUMemoryHeapInfo',
    'GPUOutOfMemoryError',
    'GPUPipelineError',
    'GPUPipelineLayout',
    'GPUQuerySet',
    'GPUQueue',
    'GPURenderBundle',
    'GPURenderBundleEncoder',
    'GPURenderPassEncoder',
    'GPURenderPipeline',
    'GPUResourceTable',
    'GPUSampler',
    'GPUShaderModule',
    'GPUSubgroupMatrixConfig',
    'GPUSupportedFeatures',
    'GPUSupportedLimits',
    'GPUTexture',
    'GPUTextureView',
    'GPUUncapturedErrorEvent',
    'GPUValidationError',
    'WGSLLanguageFeatures',
  ]);
  Object.defineProperty(globalThis, '__mimicLazySurface', {
    value: {
      domainFor: (interfaceName) => lazyDomainInterfaces.get(interfaceName),
      cell(interfaceName, member, side = 'value') {
        const key = lazyKey(interfaceName, member, side);
        let cell = lazyBehaviorCells.get(key);
        if (!cell) lazyBehaviorCells.set(key, (cell = { implementation: undefined }));
        return cell;
      },
      invoke: invokeLazyBehavior,
    },
    configurable: true,
  });
  // Private brands and captured operations travel with owner references. Public
  // methods/prototypes are mutable and cannot validate or dispatch a borrowed
  // WebIDL operation. Conversion still runs in the calling method's realm.
  const realmBindings = new WeakMap();
  const bindingGet = Function.prototype.call.bind(WeakMap.prototype.get, realmBindings);
  const bindingSet = Function.prototype.call.bind(WeakMap.prototype.set, realmBindings);
  const registerRealmBinding = (value, kind, operations, unpreventable = false) => {
    const invoke = (receiver, operation, args) => {
      const binding = bindingGet(receiver);
      if (!binding || binding.kind !== kind) throw new TypeError('Illegal invocation');
      return binding.operations[operation](...args);
    };
    bindingSet(value, { kind, operations, invoke, unpreventable });
  };
  const requireRealmBinding = (value, kind) => {
    const local = bindingGet(value);
    if (local?.kind === kind) return local;
    const reference = referenceGet(value);
    if (reference?.binding?.kind === kind) return reference;
    throw new TypeError('Illegal invocation');
  };
  const callRealmBinding = (receiver, binding, operation, args) =>
    bindingGet(receiver) === binding
      ? binding.kind === 'ElementGeometry'
        ? binding.operations[operation](receiver, ...args)
        : binding.operations[operation](...args)
      : unwrapCrossRealm(binding.frame, binding.binding.invoke)(receiver, operation, args);
  const bindingString = (value) => {
    if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    return String(value);
  };
  // Match the realm trace's once-per-(name,supported) contract before FFI.
  const tracedAccesses = new Map();
  let traceActive = false;
  const setTraceActive = (enabled) => {
    traceActive = !!enabled;
    tracedAccesses.clear();
  };
  const recordAPIAccess = (name, supported) => {
    if (!traceActive) return;
    const bit = supported ? 1 : 2,
      seen = tracedAccesses.get(name) || 0;
    if (seen & bit) return;
    tracedAccesses.set(name, seen | bit);
    host.apiAccess(name, supported);
  };
  Object.defineProperty(globalThis, '__mimicAttributeUnsafeInterfaces', {
    value: new Set([
      'WritableStream',
      'WritableStreamDefaultWriter',
      'TransformStream',
      'TransformStreamDefaultController',
      'RTCDataChannel',
      'RTCPeerConnection',
      'GPUDevice',
      'Plugin',
      'PluginArray',
      'MimeType',
      'MimeTypeArray',
    ]),
    configurable: true,
  });
  const snapshotPublication = globalThis.__mimicSnapshotPublication;
  const engineGlobals = new Set(
    snapshotPublication ? snapshotPublication.engineKeys : Reflect.ownKeys(globalThis),
  );
  const illegal = (n) => {
    throw new TypeError('Illegal constructor: ' + n);
  };
  const def = (o, n, d) =>
    Object.defineProperty(o, n, Object.assign({ enumerable: true, configurable: true }, d));
  /* shared_native_functions */
  /* shared_dom_name_validation */
  // QuickJS intentionally keeps ECMA-402 optional. Chrome does not, so expose
  // the target profile's locale/time-zone through a small deterministic Intl
  // layer while richer CLDR-backed semantics are added behind the same API.
  let intlEnvironment = host.intlEnvironment();
  globalThis[Symbol.for('__mimicRefreshIntlEnvironment')] = () => {
    intlEnvironment = host.intlEnvironment();
  };
  const nativeIntl = typeof globalThis.Intl !== 'undefined';
  if (typeof globalThis.Intl === 'undefined') {
    const canonicalLocale = (value) => String(value || intlEnvironment.locale).replace(/_/g, '-');
    // ECMA-402 locale availability comes from Chrome's bundled ICU data, not
    // from arbitrary syntactically valid BCP-47 tags. Keep the availability
    // decision separate from locale canonicalization so unknown tags such as
    // zz-ZZ are correctly omitted by supportedLocalesOf().
    const intlLanguageBases = new Set(
      'af am ar as az be bg bn bo br bs ca cs cy da de dsb el en es et eu fa fi fil fo fr ga gd gl gu ha haw he hi hr hsb hu hy id ig is it ja ka kk km kn ko ky lb lo lt lv mk ml mn mr ms mt my nb ne nl nn no or pa pl ps pt ro ru si sk sl sq sr sv sw ta te th tk tr uk ur uz vi wo xh yo zh zu'.split(
        ' ',
      ),
    );
    const supportedLocales = (locales) =>
      Intl.getCanonicalLocales(locales).filter((locale) =>
        intlLanguageBases.has(locale.split('-')[0].toLowerCase()),
      );
    class Collator {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      compare(a, b) {
        return String(a).localeCompare(String(b));
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          usage: 'sort',
          sensitivity: this.__options.sensitivity || 'variant',
          ignorePunctuation: false,
          collation: 'default',
          numeric: !!this.__options.numeric,
          caseFirst: 'false',
        };
      }
      static supportedLocalesOf(locales) {
        return supportedLocales(locales);
      }
    }
    class NumberFormat {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      format(value) {
        const n = Number(value);
        return Number.isFinite(n) ? String(n) : String(n);
      }
      formatToParts(value) {
        return [{ type: 'integer', value: this.format(value) }];
      }
      formatRange(a, b) {
        return this.format(a) + 'вЂ“' + this.format(b);
      }
      formatRangeToParts(a, b) {
        return [
          { type: 'shared', source: 'startRange', value: this.format(a) },
          { type: 'literal', source: 'shared', value: 'вЂ“' },
          { type: 'shared', source: 'endRange', value: this.format(b) },
        ];
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          numberingSystem: 'latn',
          style: this.__options.style || 'decimal',
          minimumIntegerDigits: 1,
          minimumFractionDigits: 0,
          maximumFractionDigits: 3,
          useGrouping: 'auto',
          notation: 'standard',
          signDisplay: 'auto',
          roundingIncrement: 1,
          roundingMode: 'halfExpand',
          roundingPriority: 'auto',
          trailingZeroDisplay: 'auto',
        };
      }
      static supportedLocalesOf(locales) {
        return supportedLocales(locales);
      }
    }
    class DateTimeFormat {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      format(value = Date.now()) {
        const d = new Date(value);
        return d.toLocaleString ? d.toLocaleString() : d.toString();
      }
      formatToParts(value = Date.now()) {
        return [{ type: 'literal', value: this.format(value) }];
      }
      formatRange(a, b) {
        return this.format(a) + ' вЂ“ ' + this.format(b);
      }
      formatRangeToParts(a, b) {
        return [
          { type: 'shared', source: 'startRange', value: this.format(a) },
          { type: 'literal', source: 'shared', value: ' вЂ“ ' },
          { type: 'shared', source: 'endRange', value: this.format(b) },
        ];
      }
      resolvedOptions() {
        const dateFields =
          this.__options.dateStyle ||
          this.__options.timeStyle ||
          [
            'weekday',
            'era',
            'year',
            'month',
            'day',
            'dayPeriod',
            'hour',
            'minute',
            'second',
            'fractionalSecondDigits',
            'timeZoneName',
          ].some((key) => this.__options[key] !== undefined)
            ? {}
            : { year: 'numeric', month: '2-digit', day: '2-digit' };
        return {
          locale: this.__locale,
          calendar: 'gregory',
          numberingSystem: 'latn',
          timeZone: this.__options.timeZone || intlEnvironment.timeZone,
          ...dateFields,
          ...(this.__options.hour12 === undefined
            ? {}
            : {
                hourCycle: this.__options.hour12 ? 'h12' : 'h23',
                hour12: !!this.__options.hour12,
              }),
        };
      }
      static supportedLocalesOf(locales) {
        return supportedLocales(locales);
      }
    }
    class PluralRules {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      select(value) {
        return Number(value) === 1 ? 'one' : 'other';
      }
      selectRange() {
        return 'other';
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          type: this.__options.type || 'cardinal',
          minimumIntegerDigits: 1,
          minimumFractionDigits: 0,
          maximumFractionDigits: 3,
          pluralCategories: ['one', 'other'],
        };
      }
      static supportedLocalesOf(locales) {
        return Intl.getCanonicalLocales(locales);
      }
    }
    class RelativeTimeFormat {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      format(value, unit) {
        return String(value) + ' ' + String(unit);
      }
      formatToParts(value, unit) {
        return [{ type: 'integer', value: String(value), unit: String(unit) }];
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          style: this.__options.style || 'long',
          numeric: this.__options.numeric || 'always',
          numberingSystem: 'latn',
        };
      }
      static supportedLocalesOf(locales) {
        return Intl.getCanonicalLocales(locales);
      }
    }
    class ListFormat {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      format(values) {
        return Array.from(values, String).join(', ');
      }
      formatToParts(values) {
        return Array.from(values, String).map((value, index) => ({
          type: index ? 'literal' : 'element',
          value,
        }));
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          type: this.__options.type || 'conjunction',
          style: this.__options.style || 'long',
        };
      }
      static supportedLocalesOf(locales) {
        return Intl.getCanonicalLocales(locales);
      }
    }
    class DisplayNames {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__options = options;
      }
      of(code) {
        return String(code);
      }
      resolvedOptions() {
        return {
          locale: this.__locale,
          style: this.__options.style || 'long',
          type: this.__options.type || 'language',
          fallback: this.__options.fallback || 'code',
          languageDisplay: this.__options.languageDisplay || 'dialect',
        };
      }
      static supportedLocalesOf(locales) {
        return Intl.getCanonicalLocales(locales);
      }
    }
    class Locale {
      constructor(tag, options = {}) {
        this.__tag = canonicalLocale(tag);
        this.language = this.__tag.split('-')[0];
        this.baseName = this.__tag;
        this.calendar = options.calendar;
        this.caseFirst = options.caseFirst;
        this.collation = options.collation;
        this.hourCycle = options.hourCycle;
        this.numberingSystem = options.numberingSystem;
        this.numeric = !!options.numeric;
        this.region = options.region;
        this.script = options.script;
      }
      toString() {
        return this.__tag;
      }
      maximize() {
        return new Locale(this.__tag);
      }
      minimize() {
        return new Locale(this.__tag);
      }
      getCalendars() {
        return ['gregory'];
      }
      getCollations() {
        return ['emoji', 'eor'];
      }
      getHourCycles() {
        return ['h12', 'h23'];
      }
      getNumberingSystems() {
        return ['latn'];
      }
      getTimeZones() {
        return [intlEnvironment.timeZone];
      }
      getTextInfo() {
        return { direction: 'ltr' };
      }
      getWeekInfo() {
        return { firstDay: 7, weekend: [6, 7], minimalDays: 1 };
      }
    }
    class Segmenter {
      constructor(locales, options = {}) {
        this.__locale = canonicalLocale(Array.isArray(locales) ? locales[0] : locales);
        this.__granularity = options.granularity || 'grapheme';
      }
      segment(input) {
        const source = String(input),
          values =
            this.__granularity === 'word' ? source.match(/\S+|\s+/g) || [] : Array.from(source);
        return {
          containing(index) {
            let offset = 0;
            for (const segment of values) {
              const value = {
                segment,
                index: offset,
                input: source,
                isWordLike: /\S/.test(segment),
              };
              if (index >= offset && index < offset + segment.length) return value;
              offset += segment.length;
            }
          },
          *[Symbol.iterator]() {
            let index = 0;
            for (const segment of values) {
              yield { segment, index, input: source, isWordLike: /\S/.test(segment) };
              index += segment.length;
            }
          },
        };
      }
      resolvedOptions() {
        return { locale: this.__locale, granularity: this.__granularity };
      }
      static supportedLocalesOf(locales) {
        return Intl.getCanonicalLocales(locales);
      }
    }
    const callable = (name, Constructor) =>
      new Proxy(Constructor, {
        apply(target, _self, args) {
          recordAPIAccess('Intl.' + name, true);
          return Reflect.construct(target, args);
        },
        construct(target, args, newTarget) {
          recordAPIAccess('Intl.' + name, true);
          return Reflect.construct(target, args, newTarget);
        },
      });
    globalThis.Intl = {
      getCanonicalLocales(locales) {
        recordAPIAccess('Intl.getCanonicalLocales', true);
        if (locales === undefined) return [];
        return (Array.isArray(locales) ? locales : [locales]).map(canonicalLocale);
      },
      supportedValuesOf(key) {
        recordAPIAccess('Intl.supportedValuesOf', true);
        if (String(key) === 'timeZone') return [intlEnvironment.timeZone];
        return [];
      },
      Collator: callable('Collator', Collator),
      NumberFormat: callable('NumberFormat', NumberFormat),
      DateTimeFormat: callable('DateTimeFormat', DateTimeFormat),
      PluralRules: callable('PluralRules', PluralRules),
      RelativeTimeFormat: callable('RelativeTimeFormat', RelativeTimeFormat),
      ListFormat: callable('ListFormat', ListFormat),
      DisplayNames: callable('DisplayNames', DisplayNames),
      Locale: callable('Locale', Locale),
      Segmenter: callable('Segmenter', Segmenter),
    };
    for (const [name, value] of Object.entries(globalThis.Intl)) {
      markNative(value, name);
      if (typeof value === 'function' && value.prototype)
        for (const member of Reflect.ownKeys(value.prototype)) {
          if (member === 'constructor') continue;
          const descriptor = Object.getOwnPropertyDescriptor(value.prototype, member);
          markNative(descriptor && descriptor.value, String(member));
          markNative(descriptor && descriptor.get, String(member), 'get ');
        }
    }
  } else {
    /* native_intl */
  }
  /* profile_locale */
  /* shared_fetch_primitives */
  const eventSlots = new WeakMap(),
    messageEventSlots = new WeakMap(),
    errorEventSlots = new WeakMap();
  class Event {
    constructor(type, init = {}) {
      eventSlots.set(this, {
        type: String(type),
        bubbles: !!init.bubbles,
        cancelable: !!init.cancelable,
        composed: !!init.composed,
        timeStamp: host.performanceNow(),
        defaultPrevented: false,
        trusted: false,
      });
      Object.defineProperty(this, 'isTrusted', {
        get: () => !!eventSlots.get(this).trusted,
        enumerable: true,
        configurable: false,
      });
    }
    get timeStamp() {
      return eventSlots.get(this).timeStamp;
    }
    get type() {
      return eventSlots.get(this).type;
    }
    get bubbles() {
      return eventSlots.get(this).bubbles;
    }
    get cancelable() {
      return eventSlots.get(this).cancelable;
    }
    get defaultPrevented() {
      return eventSlots.get(this).defaultPrevented;
    }
    preventDefault() {
      const state = eventSlots.get(this);
      if (state.cancelable) state.defaultPrevented = true;
    }
  }
  class MessageEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      messageEventSlots.set(this, {
        data: init.data,
        origin: String(init.origin || ''),
        lastEventId: String(init.lastEventId || ''),
        source: init.source || null,
        ports: init.ports || [],
      });
    }
    get data() {
      return messageEventSlots.get(this).data;
    }
    get origin() {
      return messageEventSlots.get(this).origin;
    }
    get lastEventId() {
      return messageEventSlots.get(this).lastEventId;
    }
    get source() {
      return messageEventSlots.get(this).source;
    }
    get ports() {
      return messageEventSlots.get(this).ports;
    }
  }
  class ErrorEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      errorEventSlots.set(this, {
        message: String(init.message || ''),
        filename: String(init.filename || ''),
        lineno: Number(init.lineno || 0),
        colno: Number(init.colno || 0),
        error: init.error,
      });
    }
    get message() {
      return errorEventSlots.get(this).message;
    }
    get filename() {
      return errorEventSlots.get(this).filename;
    }
    get lineno() {
      return errorEventSlots.get(this).lineno;
    }
    get colno() {
      return errorEventSlots.get(this).colno;
    }
    get error() {
      return errorEventSlots.get(this).error;
    }
  }
  let eventListeners = new WeakMap();
  const listenersFor = (value) => {
    let listeners = eventListeners.get(value);
    if (!listeners) {
      listeners = new Map();
      eventListeners.set(value, listeners);
    }
    return listeners;
  };
  let eventHandlerListeners = new WeakMap(),
    eventHandlerWrappers = new WeakMap();
  const eventHandlerRecord = (target, type) => {
    let map = eventHandlerListeners.get(target);
    if (!map) {
      map = new Map();
      eventHandlerListeners.set(target, map);
    }
    let record = map.get(type);
    if (!record) {
      record = { value: null, listener: null };
      map.set(type, record);
    }
    return record;
  };
  const setEventHandlerValue = (target, type, value) => {
    const record = eventHandlerRecord(target, type);
    value =
      value !== null && (typeof value === 'function' || typeof value === 'object') ? value : null;
    record.value = value;
    if (value === null && record.listener) {
      target.removeEventListener(type, record.listener);
      record.listener = null;
    }
    if (value !== null && !record.listener) {
      record.listener = function (event) {
        if (typeof record.value === 'function' && record.value.call(this, event) === false)
          event.preventDefault();
      };
      eventHandlerWrappers.set(record.listener, record);
      target.addEventListener(type, record.listener);
    }
  };
  const internalEventHandler = (target, type) => {
    if (eventHandlerListeners.get(target)?.has(type)) return null;
    if (elementHandlers.has(target)) return elementHandlers.get(target)[type] || null;
    if (xhrEventTargetSlots.has(target) && type !== 'readystatechange')
      return xhrEventTargetSlots.get(target)['on' + type] || null;
    if (xhrSlots.has(target)) return xhrSlots.get(target)['on' + type] || null;
    if (rtcPeerStates.has(target)) return rtcPeerStates.get(target)['on' + type] || null;
    if (rtcDataStates.has(target)) return rtcDataStates.get(target)['on' + type] || null;
    const name = 'on' + type;
    return name in target ? target[name] : null;
  };
  const eventParents = new WeakMap();
  let dispatchEventCore = (target, event, trusted) => {
    const state = eventSlots.get(event);
    if (!state) throw new TypeError('parameter 1 is not of type Event');
    if (trusted) state.trusted = true;
    for (const listener of (listenersFor(target).get(state.type) || []).slice())
      listener.call(target, event);
    const handler = internalEventHandler(target, state.type);
    if (typeof handler === 'function') handler.call(target, event);
    return !state.defaultPrevented;
  };
  class EventTarget {
    constructor() {
      listenersFor(this);
    }
    addEventListener(t, f) {
      const target = this == null ? globalThis : this;
      if (typeof f === 'function') {
        const listeners = listenersFor(target),
          a = listeners.get(String(t)) || [];
        a.push(f);
        listeners.set(String(t), a);
      }
    }
    removeEventListener(t, f) {
      const target = this == null ? globalThis : this,
        a = listenersFor(target).get(String(t)) || [];
      const i = a.indexOf(f);
      if (i >= 0) a.splice(i, 1);
    }
    dispatchEvent(e) {
      return dispatchEventCore(this == null ? globalThis : this, e, false);
    }
  }
  const dispatchTrusted = (target, event) => dispatchEventCore(target, event, true);
  const dispatchNative = (target, event) => dispatchEventCore(target, event, true, true);
  const messagePortSlots = new WeakMap();
  class MessagePort extends EventTarget {
    constructor(token) {
      super();
      if (token !== hostToken) illegal('MessagePort');
      messagePortSlots.set(this, {
        peer: null,
        closed: false,
        started: false,
        onmessage: null,
        onmessageerror: null,
      });
    }
    postMessage(message, transfer = []) {
      const state = messagePortSlots.get(this),
        peer = state.peer,
        peerState = peer && messagePortSlots.get(peer);
      if (state.closed || !peerState || peerState.closed) return;
      host.queuePostedMessage(() => {
        if (peerState.closed) return;
        dispatchTrusted(peer, new MessageEvent('message', { data: message, ports: [] }));
      });
    }
    start() {
      messagePortSlots.get(this).started = true;
    }
    close() {
      messagePortSlots.get(this).closed = true;
    }
    get onmessage() {
      return messagePortSlots.get(this).onmessage;
    }
    set onmessage(value) {
      const state = messagePortSlots.get(this);
      state.onmessage = typeof value === 'function' ? value : null;
      if (state.onmessage) state.started = true;
    }
    get onmessageerror() {
      return messagePortSlots.get(this).onmessageerror;
    }
    set onmessageerror(value) {
      messagePortSlots.get(this).onmessageerror = typeof value === 'function' ? value : null;
    }
  }
  class MessageChannel {
    constructor() {
      const port1 = new MessagePort(hostToken),
        port2 = new MessagePort(hostToken);
      messagePortSlots.get(port1).peer = port2;
      messagePortSlots.get(port2).peer = port1;
      Object.defineProperties(this, {
        port1: { value: port1, enumerable: true },
        port2: { value: port2, enumerable: true },
      });
    }
  }
  const broadcastChannels = new Map(),
    broadcastChannelSlots = new WeakMap();
  class BroadcastChannel extends EventTarget {
    constructor(name) {
      super();
      name = String(name);
      broadcastChannelSlots.set(this, {
        name,
        closed: false,
        onmessage: null,
        onmessageerror: null,
      });
      const channels = broadcastChannels.get(name) || new Set();
      channels.add(this);
      broadcastChannels.set(name, channels);
    }
    get name() {
      return broadcastChannelSlots.get(this).name;
    }
    get onmessage() {
      return broadcastChannelSlots.get(this).onmessage;
    }
    set onmessage(value) {
      broadcastChannelSlots.get(this).onmessage = typeof value === 'function' ? value : null;
    }
    get onmessageerror() {
      return broadcastChannelSlots.get(this).onmessageerror;
    }
    set onmessageerror(value) {
      broadcastChannelSlots.get(this).onmessageerror = typeof value === 'function' ? value : null;
    }
    postMessage(message) {
      const state = broadcastChannelSlots.get(this);
      if (state.closed)
        throw platformDOMException('BroadcastChannel is closed.', 'InvalidStateError');
      const encoded = cloneCodec.encode(message);
      for (const channel of broadcastChannels.get(state.name) || []) {
        if (channel === this || broadcastChannelSlots.get(channel).closed) continue;
        host.queuePostedMessage(() =>
          dispatchTrusted(
            channel,
            new MessageEvent('message', {
              data: cloneCodec.decode(encoded),
              origin: host.locationPart('origin'),
            }),
          ),
        );
      }
    }
    close() {
      const state = broadcastChannelSlots.get(this);
      if (state.closed) return;
      state.closed = true;
      broadcastChannels.get(state.name)?.delete(this);
    }
  }
  const browserMessagePortSlots = new WeakMap(),
    browserMessagePortWrappers = new Map();
  const wrapBrowserMessagePort = (id) => {
    id = String(id);
    const existing = browserMessagePortWrappers.get(id);
    if (existing) return existing;
    const port = new BrowserMessagePort(hostToken, id);
    browserMessagePortWrappers.set(id, port);
    return port;
  };
  const takeMessagePorts = (transfer) =>
    Array.from(transfer || []).flatMap((port) => {
      const binding = bindingGet(port) || referenceGet(port);
      if (binding?.kind !== 'MessagePort' && binding?.binding?.kind !== 'MessagePort') return [];
      return [callRealmBinding(port, binding, 'transfer', [])];
    });
  class BrowserMessagePort extends MessagePort {
    constructor(token, id) {
      super(hostToken);
      if (token !== hostToken) illegal('MessagePort');
      browserMessagePortSlots.set(this, {
        id: String(id),
        closed: false,
        transferred: false,
        started: false,
        onmessage: null,
        onmessageerror: null,
        pending: [],
      });
      registerRealmBinding(this, 'MessagePort', {
        transfer: () => {
          const state = browserMessagePortSlots.get(this);
          if (state.closed || state.transferred)
            throw platformDOMException(
              'MessagePort at index 0 is already neutered.',
              'DataCloneError',
            );
          state.transferred = true;
          browserMessagePortWrappers.delete(state.id);
          return state.id;
        },
      });
    }
    postMessage(message, transfer = []) {
      const state = browserMessagePortSlots.get(this);
      if (state.closed || state.transferred) return;
      host.messagePortPost(state.id, cloneCodec.encode(message), takeMessagePorts(transfer));
    }
    start() {
      const state = browserMessagePortSlots.get(this);
      state.started = true;
      while (state.pending.length) {
        const event = state.pending.shift();
        host.queuePostedMessage(() => dispatchTrusted(this, event));
      }
    }
    close() {
      const state = browserMessagePortSlots.get(this);
      if (state.closed) return;
      state.closed = true;
      host.messagePortClose(state.id);
    }
    get onmessage() {
      return browserMessagePortSlots.get(this).onmessage;
    }
    set onmessage(value) {
      const state = browserMessagePortSlots.get(this);
      state.onmessage = typeof value === 'function' ? value : null;
      if (state.onmessage) this.start();
    }
    get onmessageerror() {
      return browserMessagePortSlots.get(this).onmessageerror;
    }
    set onmessageerror(value) {
      const state = browserMessagePortSlots.get(this);
      state.onmessageerror = typeof value === 'function' ? value : null;
    }
  }
  class BrowserMessageChannel {
    constructor() {
      const ids = host.newMessageChannel(),
        port1 = wrapBrowserMessagePort(ids[0]),
        port2 = wrapBrowserMessagePort(ids[1]);
      Object.defineProperties(this, {
        port1: { value: port1, enumerable: true },
        port2: { value: port2, enumerable: true },
      });
    }
  }
  globalThis.__receiveMessagePort = (id, data, portIds = []) => {
    const port = browserMessagePortWrappers.get(String(id));
    if (!port) return;
    const state = browserMessagePortSlots.get(port),
      event = new MessageEvent('message', {
        data: cloneCodec.decode(data),
        ports: Array.from(portIds, wrapBrowserMessagePort),
      });
    if (state.started) dispatchTrusted(port, event);
    else state.pending.push(event);
  };
  class Node extends EventTarget {
    constructor(token) {
      super();
      if (token !== hostToken) illegal('Node');
    }
    get nodeType() {
      const slot = elementSlot(this);
      if (slot)
        return slot.type === 'element'
          ? 1
          : slot.type === 'text'
            ? 3
            : slot.type === 'comment'
              ? 8
              : slot.type === 'fragment'
                ? 11
                : 0;
      if (this instanceof Document) return 9;
      if (fragmentSlots.has(this)) return 11;
      return 0;
    }
    get nodeName() {
      const slot = elementSlot(this);
      if (slot)
        return slot.type === 'element'
          ? slot.tagName
          : slot.type === 'text'
            ? '#text'
            : slot.type === 'comment'
              ? '#comment'
              : '';
      if (this instanceof Document) return '#document';
      if (fragmentSlots.has(this)) return '#document-fragment';
      return '';
    }
    get textContent() {
      const slot = elementSlot(this);
      return slot ? canonicalTextContent(slot.nodeId) : null;
    }
    set textContent(value) {
      const slot = elementSlot(this);
      if (slot) {
        setCanonicalTextContent(slot.nodeId, value == null ? '' : String(value));
        invalidateDOMCollections();
      }
    }
    get parentNode() {
      const slot = elementSlot(this);
      return slot ? cachedDOMParent(this) : null;
    }
    get firstChild() {
      const slot = elementSlot(this);
      if (slot) return wrap(host.firstChild(slot.nodeId));
      const state = fragmentSlots.get(this);
      return (state && state.children[0]) || null;
    }
    get childNodes() {
      const slot = elementSlot(this);
      if (slot) return nodeList(() => cachedDOMChildren(this).map((child) => elementSlot(child)));
      const state = fragmentSlots.get(this);
      return state ? nodeList(state.children.map((child) => elementSlot(child))) : nodeList([]);
    }
    hasChildNodes() {
      return this.childNodes.length !== 0;
    }
    get isConnected() {
      return isNodeConnected(this);
    }
    contains(other) {
      if (other == null) return false;
      if (other === this) return true;
      const own = elementSlot(this),
        child = elementSlot(other);
      if (own && (own.type === 'text' || own.type === 'comment')) return false;
      if (own && child && !syntheticParents.has(other))
        return host.contains(own.nodeId, child.nodeId);
      for (let node = other; node; node = canonicalDOMParent(node)) if (node === this) return true;
      return false;
    }
  }
  class CharacterData extends Node {
    constructor(token, data) {
      super(token);
      elementData.set(this, data);
    }
    get nodeName() {
      const slot = elementSlot(this);
      return slot && slot.type === 'comment' ? '#comment' : '#text';
    }
    get data() {
      return elementSlot(this).text || '';
    }
    set data(value) {
      this.textContent = String(value);
    }
    get length() {
      return this.data.length;
    }
  }
  class Text extends CharacterData {
    constructor(token, data) {
      super(token, data);
    }
  }
  class Comment extends CharacterData {
    constructor(token, data) {
      super(token, data);
    }
  }
  const domTokenSlots = new WeakMap(),
    domTokenState = (value) => domTokenSlots.get(value),
    domTokens = (value) => {
      const state = domTokenState(value);
      return (intrinsicGetAttribute.call(state.element, state.attribute) || '')
        .trim()
        .split(/\s+/)
        .filter(Boolean);
    },
    writeDOMTokens = (value, tokens) => {
      const state = domTokenState(value);
      intrinsicSetAttribute.call(state.element, state.attribute, [...new Set(tokens)].join(' '));
    };
  class DOMTokenList {
    constructor(element, attribute) {
      domTokenSlots.set(this, { element, attribute });
    }
    get length() {
      return domTokens(this).length;
    }
    get value() {
      const state = domTokenState(this);
      return intrinsicGetAttribute.call(state.element, state.attribute) || '';
    }
    set value(v) {
      const state = domTokenState(this);
      intrinsicSetAttribute.call(state.element, state.attribute, String(v));
    }
    item(i) {
      return domTokens(this)[Number(i)] ?? null;
    }
    contains(token) {
      return domTokens(this).includes(String(token));
    }
    add(...tokens) {
      writeDOMTokens(this, domTokens(this).concat(tokens.map(String)));
    }
    remove(...tokens) {
      const gone = new Set(tokens.map(String));
      writeDOMTokens(
        this,
        domTokens(this).filter((x) => !gone.has(x)),
      );
    }
    toggle(token, force) {
      const state = domTokenState(this);
      return host.toggleToken(
        elementSlot(state.element).nodeId,
        state.attribute,
        String(token),
        force === true ? 1 : force === false ? 0 : -1,
      );
    }
    replace(oldToken, newToken) {
      const a = domTokens(this),
        i = a.indexOf(String(oldToken));
      if (i < 0) return false;
      a[i] = String(newToken);
      writeDOMTokens(this, a);
      return true;
    }
    supports(token) {
      const state = domTokenState(this);
      if (state.attribute === 'controlslist')
        return ['nodownload', 'nofullscreen', 'noremoteplayback', 'noplaybackrate'].includes(
          String(token),
        );
      return false;
    }
    forEach(cb, thisArg) {
      domTokens(this).forEach((v, i) => cb.call(thisArg, v, v, this));
    }
    entries() {
      return Array.from(domTokens(this).entries())[Symbol.iterator]();
    }
    keys() {
      return Array.from(domTokens(this).keys())[Symbol.iterator]();
    }
    values() {
      return domTokens(this)[Symbol.iterator]();
    }
    toString() {
      return this.value;
    }
    [Symbol.iterator]() {
      return this.values();
    }
  }
  /* shared_webkit_css */
  const cssName = (n) => {
    n = String(n);
    if (n.startsWith('--')) return n;
    n = n.toLowerCase();
    return webkitCSSAliases.get(n) || n;
  };
  const cssJSName = (n) =>
    cssNamedProperties[n] ||
    webkitJSNames.get(n) ||
    (n === 'cssFloat'
      ? 'float'
      : n.startsWith('--')
        ? n
        : n.replace(/[A-Z]/g, (m) => '-' + m.toLowerCase()));
  const cssJSInputName = (name) =>
    webkitCSSAliases.has(name)
      ? name
      : /^[wW]ebkit[A-Z]/.test(name)
        ? '-webkit' + name.slice(6).replace(/[A-Z]/g, (c) => '-' + c.toLowerCase())
        : cssJSName(name);
  const parseCSS = (text) => {
    const out = [];
    for (const raw of splitCSSDeclarations(String(text || ''))) {
      const i = raw.indexOf(':');
      if (i < 0) continue;
      const name = cssName(raw.slice(0, i).trim());
      if (!name) continue;
      const extracted = cssExtractPriority(raw.slice(i + 1).trim());
      if (!extracted) continue;
      let { value, priority } = extracted;
      value = normalizeCSSValue(name, value, raw.slice(0, i).trim());
      if (value === null || value === '') continue;
      for (const entry of expandCSSDeclaration(
        cssPrecisionDeclaration({ name, value, priority }, extracted.value),
      )) {
        const old = out.findIndex((x) => x.name === entry.name);
        if (old >= 0) {
          if (out[old].priority === 'important' && priority !== 'important') continue;
          out.splice(old, 1);
          out.push(entry);
        } else out.push(entry);
      }
    }
    return out;
  };
  const serializeCSS = serializeCSSDeclarations;
  const inlineDisplayTags = new Set([
    'A',
    'ABBR',
    'B',
    'CODE',
    'EM',
    'I',
    'IFRAME',
    'IMG',
    'INPUT',
    'LABEL',
    'SMALL',
    'SPAN',
    'STRONG',
  ]);
  const simpleCSSMatch = (element, selector) => {
    if (selector.includes(':root')) {
      if (element.tagName !== 'HTML') return false;
      selector = selector.replace(/:root/g, '');
    }
    if (selector.includes(':')) return false;
    const tag = /^[a-zA-Z][\w-]*|^\*/.exec(selector)?.[0];
    if (tag && tag !== '*' && element.tagName !== tag.toUpperCase()) return false;
    for (const id of selector.matchAll(/#([\w-]+)/g)) if (element.id !== id[1]) return false;
    for (const cls of selector.matchAll(/\.([\w-]+)/g))
      if (!element.classList.contains(cls[1])) return false;
    for (const attr of selector.matchAll(/\[([\w-]+)(?:\s*=\s*(["']?)([^\]"']*)\2)?\]/g)) {
      const actual = element.getAttribute(attr[1]);
      if (actual === null || (attr[3] !== undefined && actual !== attr[3])) return false;
    }
    return true;
  };
  // Stylesheet selectors are a forgiving input boundary: invalid or unsupported
  // rules do not abort computed style. DOM selector APIs retain their SyntaxError.
  const cssSelectorMatch = (element, selector) => {
    try {
      return compatibilitySelectors.matches(element, selector);
    } catch (error) {
      if (error && error.name === 'SyntaxError') return false;
      throw error;
    }
  };
  // Top-world observations share one canonical revision. Child/isolated/foreign
  // realm dependencies retain only synchronous
  // reuse until their complete cross-realm input epoch can be represented.
  let styleReadCache = null,
    styleObservationDynamic = false;
  // Immutable rule programs survive checkpoints; derived element observations
  // share only a matching canonical epoch, including across task checkpoints.
  let checkpointStyleRules = null,
    checkpointStyleVersion = '';
  let checkpointObservations = null,
    checkpointObservationVersion = '';
  let checkpointForeignComputedValues = new Map(),
    checkpointForeignComputedValueVersion = '',
    checkpointForeignDocumentBatchVersion = '',
    checkpointForeignDocumentBatch = false;
  let retainedGeometryVersion = '',
    retainedGeometryRevision = -1,
    retainedGeometryCleanRevision = -1,
    retainedGeometryGeneration = 0,
    retainedGeometryDirty = new WeakMap(),
    retainedStyleAttributeDependencies = new WeakMap(),
    retainedGeometryWidths,
    retainedGeometrySizes,
    retainedGeometryRects,
    retainedGeometryClientRects;
  const retainedGeometryDirtyGeneration = (element) => {
    const cached = styleReadCache.retainedGeometryDirtyVersions;
    if (cached.has(element)) return cached.get(element);
    let generation = 0;
    for (let node = element; node; node = geometryParent(node))
      generation = Math.max(generation, retainedGeometryDirty.get(node) || 0);
    cached.set(element, generation);
    return generation;
  };
  const retainedGeometryCache = () => {
    const values = new WeakMap(),
      cache = {
        has(element) {
          return cache.get(element) !== undefined;
        },
        get(element) {
          const record = values.get(element);
          return record &&
            (retainedGeometryCleanRevision === retainedGeometryRevision ||
              record.generation >= retainedGeometryDirtyGeneration(element))
            ? record.value
            : undefined;
        },
        set(element, value) {
          values.set(element, { generation: retainedGeometryGeneration, value });
          return cache;
        },
      };
    return cache;
  };
  const resetRetainedGeometry = (version, revision) => {
    retainedGeometryVersion = version;
    retainedGeometryRevision = revision;
    retainedGeometryCleanRevision = revision;
    retainedGeometryGeneration = 0;
    retainedGeometryDirty = new WeakMap();
    retainedGeometryWidths = retainedGeometryCache();
    retainedGeometrySizes = retainedGeometryCache();
    retainedGeometryRects = retainedGeometryCache();
    retainedGeometryClientRects = retainedGeometryCache();
  };
  const invalidateRetainedViewportGeometry = () => {
    retainedGeometryClientRects = retainedGeometryCache();
    // Isolated worlds do not own the main world's scroll revision. Discard
    // their whole checkpoint after a bridged scroll so a second read in the
    // same inspector callback cannot reuse viewport coordinates sampled before
    // the owner realm mutation.
    if (styleObservationIsolated) checkpointObservations = null;
    else if (checkpointObservations)
      checkpointObservations.clientRects = retainedGeometryClientRects;
    if (styleReadCache) styleReadCache.clientRects = retainedGeometryClientRects;
  };
  resetRetainedGeometry('', -1);
  const invalidateRetainedGeometry = (target) => {
    // No valid retained epoch exists until an observation has populated it.
    // Repeated construction must not walk ancestry merely to dirty absent data.
    if (retainedGeometryRevision === -1) {
      checkpointObservations = null;
      return;
    }
    if (!target) {
      retainedStyleAttributeDependencies = new WeakMap();
      retainedGeometryCleanRevision = -1;
      retainedGeometryRevision = -1;
      return;
    }
    if (target?.isConnected) {
      // A connected mutation can move following siblings and descendants that
      // are not on the mutated node's ancestor chain. Retain the graph only
      // across observation-only revisions and detached construction.
      retainedStyleAttributeDependencies = new WeakMap();
      retainedGeometryCleanRevision = -1;
      retainedGeometryRevision = -1;
      checkpointObservations = null;
      return;
    }
    retainedGeometryRevision = host.observationRevision();
    retainedGeometryCleanRevision = -1;
    retainedGeometryGeneration++;
    let element = elementSlot(target)?.type === 'element' ? target : target.parentElement;
    for (; element; element = geometryParent(element))
      retainedGeometryDirty.set(element, retainedGeometryGeneration);
  };
  const retainGeometryAcrossAttributeMutation = (element, name) => {
    if (retainedGeometryRevision === -1) {
      checkpointObservations = null;
      styleReadCache = null;
      return;
    }
    // Locating a containing shadow root itself walks canonical ancestry. Keep
    // this fast path document-only; any live shadow tree uses the conservative
    // path until dependencies are indexed per canonical tree root.
    const dependencies = shadowHosts.size ? null : retainedStyleAttributeDependencies.get(document),
      inline = String(elementSlot(element)?.attributes?.style || '').toLowerCase();
    // This first dependency-aware slice is intentionally narrow. A data
    // attribute cannot affect built-in box semantics, but it may participate
    // in an author selector or attr(). Sources containing escapes are treated
    // conservatively because the escaped identifier may name this attribute.
    const irrelevant =
      element.isConnected &&
      name.startsWith('data-') &&
      dependencies &&
      !dependencies.unsafe &&
      !dependencies.sources.some((source) => source.includes(name)) &&
      !inline.includes(name) &&
      !inline.includes('\\');
    if (irrelevant) {
      retainedGeometryRevision = host.observationRevision();
      retainedGeometryCleanRevision = retainedGeometryRevision;
    } else invalidateRetainedGeometry(element);
    // A mutation performed from inside an injected callback ends any active
    // read snapshot even when its retained geometry remains valid.
    styleReadCache = null;
  };
  let styleObservationIsolated = host.isIsolatedInputWorld();
  bootstrapRestoreHooks.push(() => {
    styleObservationIsolated = host.isIsolatedInputWorld();
    checkpointObservations = null;
    checkpointStyleRules = null;
    checkpointForeignComputedValues = new Map();
    checkpointForeignComputedValueVersion = '';
    checkpointForeignDocumentBatchVersion = '';
    checkpointForeignDocumentBatch = false;
    retainedStyleAttributeDependencies = new WeakMap();
    resetRetainedGeometry('', -1);
  });
  const withStyleReadCache = (callback) => {
    // A nested observation already belongs to the outer command's canonical
    // snapshot. Re-reading the host epoch for every computed-style/property
    // helper turns one selector walk into tens of thousands of Go↔V8 calls;
    // the outer finally block still rejects the snapshot if script mutates it.
    if (styleReadCache) return callback();
    const previous = styleReadCache,
      canonicalBundle = host.observationVersion(),
      resourceSeparator = canonicalBundle.lastIndexOf('|'),
      canonicalVersion = canonicalBundle.slice(0, resourceSeparator);
    // The canonical epoch includes viewport and media preferences, so checking
    // it does not require separate host calls for each environment input.
    const version = canonicalVersion + ':' + constructedStyleSheets.revision();
    const retain =
      windowRelations.self === windowRelations.top &&
      !(styleObservationIsolated && canonicalVersion.endsWith(':true'));
    if (!checkpointStyleRules || checkpointStyleVersion !== version) {
      checkpointStyleRules = new WeakMap();
      checkpointStyleVersion = version;
    }
    const elementStateVersion = compatibilityElementState.observationVersion(),
      observationVersion = version + ':' + elementStateVersion;
    let observation =
      previous?.version === observationVersion
        ? previous
        : retain && checkpointObservationVersion === observationVersion
          ? checkpointObservations
          : null;
    if (!observation)
      observation = {
        version: observationVersion,
        environmentVersion: canonicalVersion.slice(canonicalVersion.indexOf('|') + 1),
        retainable: retain,
        rules: retain ? checkpointStyleRules : new WeakMap(),
        declarations: new WeakMap(),
        widths: new WeakMap(),
        rects: new WeakMap(),
        resolvingRects: new Set(),
        provisionalRects: new WeakSet(),
      };
    const canonicalParts = canonicalBundle.split('|'),
      canonicalHead = canonicalParts[0].split(':'),
      geometryVersion =
        canonicalHead[1] +
        '|' +
        canonicalParts.slice(1).join('|') +
        ':' +
        constructedStyleSheets.revision() +
        ':' +
        elementStateVersion,
      documentRevision = Number(canonicalHead[0]);
    if (
      !retain ||
      retainedGeometryVersion !== geometryVersion ||
      (retainedGeometryRevision !== -1 && retainedGeometryRevision !== documentRevision)
    )
      resetRetainedGeometry(geometryVersion, documentRevision);
    else if (retainedGeometryRevision === -1)
      resetRetainedGeometry(geometryVersion, documentRevision);
    if (retain) {
      observation.retainedGeometryDirtyVersions = new WeakMap();
      observation.widths = retainedGeometryWidths;
      observation.boxSizes = retainedGeometrySizes;
      observation.rects = retainedGeometryRects;
      if (!styleObservationIsolated) observation.clientRects = retainedGeometryClientRects;
    }
    if (observation.geometryVersion !== canonicalBundle) {
      observation.geometryVersion = canonicalBundle;
      observation.computedValues = new WeakMap();
      observation.tableColumns = new WeakMap();
      if (!retain) {
        observation.widths = new WeakMap();
        observation.rects = new WeakMap();
      }
      observation.resolvingRects = new Set();
      observation.provisionalRects = new WeakSet();
    }
    if (retain) {
      checkpointObservations = observation;
      checkpointObservationVersion = observationVersion;
    }
    const blitzEpochKey =
      canonicalBundle + ':' + elementStateVersion + ':' + constructedStyleSheets.revision();
    if (observation.blitzEpochKey !== blitzEpochKey) {
      observation.blitzPacked = undefined;
      observation.blitzEpochKey = blitzEpochKey;
    }
    styleReadCache = observation;
    try {
      return callback();
    } catch (error) {
      // A failed observation may have published provisional recursive boxes.
      // Do not retain that partial graph for a later command.
      if (checkpointObservations === observation) checkpointObservations = null;
      if (retain) resetRetainedGeometry('', -1);
      throw error;
    } finally {
      styleReadCache = previous;
      // Style/layout evaluation itself cannot run author code. Page mutations
      // are serialized on the realm actor and the next outer observation
      // validates the canonical host epoch before reusing this snapshot.
      if (!observation.retainable && checkpointObservations === observation)
        checkpointObservations = null;
    }
  };
  // Parentage, ownership and attributes are projections of the same canonical
  // node. The private atomic word publishes every canonical arena write before
  // unlocking it, including writes from another realm. Demand-loaded readbacks
  // survive only that exact epoch; engines without the word use host reads.
  const readCanonicalRevision = typeof Atomics === 'undefined' ? null : Atomics.load,
    CanonicalRevisionArray = typeof BigUint64Array === 'undefined' ? null : BigUint64Array;
  let canonicalRevisionWord = null,
    canonicalReadVersion = -1n,
    canonicalReadRecords = new WeakMap(),
    canonicalReadAdmissions = 0;
  registerBootstrapCallback('installCanonicalDOMReads', (buffer) => {
    canonicalRevisionWord = new CanonicalRevisionArray(buffer);
    canonicalReadVersion = -1n;
    canonicalReadRecords = new WeakMap();
    canonicalReadAdmissions = 0;
  });
  const canonicalDOMRevision = () =>
    canonicalRevisionWord ? readCanonicalRevision(canonicalRevisionWord, 0) : host.domRevision();
  bootstrapRestoreHooks.push(() => {
    canonicalRevisionWord = null;
    canonicalReadRecords = new WeakMap();
    canonicalReadAdmissions = 0;
  });
  const domReadRecord = (node) => {
    let records;
    if (styleReadCache)
      records = styleReadCache.domReadRecords || (styleReadCache.domReadRecords = new WeakMap());
    else {
      if (!canonicalRevisionWord || referenceGet(node)) return null;
      const version = readCanonicalRevision(canonicalRevisionWord, 0);
      if (version !== canonicalReadVersion) {
        canonicalReadVersion = version;
        canonicalReadRecords = new WeakMap();
        canonicalReadAdmissions = 0;
      }
      records = canonicalReadRecords;
      if (!records.has(node) && canonicalReadAdmissions >= 32768) return null;
    }
    let record = records.get(node);
    if (!record) {
      records.set(node, (record = {}));
      if (!styleReadCache) canonicalReadAdmissions++;
    }
    return record;
  };
  const cachedDOMParent = (node) => {
    const slot = elementSlot(node),
      record = domReadRecord(node);
    if (!record) return wrap(host.parentNode(slot.nodeId));
    if (!Object.hasOwn(record, 'parent')) record.parent = wrap(host.parentNode(slot.nodeId));
    return record.parent;
  };
  const cachedDOMOwnerDocument = (node) => {
    const slot = elementSlot(node),
      record = domReadRecord(node);
    if (!record) return wrap(host.nodeOwnerDocument(slot.nodeId));
    if (!Object.hasOwn(record, 'ownerDocument'))
      record.ownerDocument = wrap(host.nodeOwnerDocument(slot.nodeId));
    return record.ownerDocument;
  };
  const cachedDOMChildren = (node) => {
    const nodeID = node === document ? realmDocumentRootID : elementSlot(node).nodeId;
    if (!styleReadCache) {
      const record = domReadRecord(node);
      if (!record) return host.childIDs(nodeID).map(wrap);
      return record.children || (record.children = host.childIDs(nodeID).map(wrap));
    }
    const cache = styleReadCache.domChildren || (styleReadCache.domChildren = new WeakMap());
    if (!cache.has(node)) cache.set(node, host.childIDs(nodeID).map(wrap));
    return cache.get(node);
  };
  const cachedDOMAttribute = (node, name) => {
    const data = elementSlot(node);
    if (
      data.namespaceURI === 'http://www.w3.org/1999/xhtml' ||
      (!data.namespaceURI && !data.qualifiedName)
    )
      name = name.toLowerCase();
    const record = domReadRecord(node);
    if (!record) return host.getAttribute(elementSlot(node).nodeId, name);
    let cache = record.attributes;
    if (!cache) record.attributes = cache = new Map();
    if (!cache.has(name)) {
      // Repeated attribute consumers benefit from one canonical row. Keep
      // single-attribute observations cheap and demand-loaded.
      if (!styleReadCache && cache.size >= 3) {
        const attributes = cssObservationNodeState(node).attributes;
        return Object.hasOwn(attributes, name) ? attributes[name] : null;
      }
      cache.set(name, host.getAttribute(elementSlot(node).nodeId, name));
    }
    return cache.get(name);
  };
  const cachedDOMAttributeNames = (node) => {
    const record = domReadRecord(node);
    if (!record) return host.attributeNames(elementSlot(node).nodeId);
    if (!Object.hasOwn(record, 'attributeNames'))
      record.attributeNames = host.attributeNames(elementSlot(node).nodeId);
    return record.attributeNames;
  };
  // All geometry projections consult the same canonical parent/child snapshot.
  // Keep native membership separate from flat-tree projection (slot/host links)
  // and invalidate both through the same canonical observation epoch.
  const cssObservationNodeState = (node) => {
    const record = domReadRecord(node);
    if (record?.observationState) return record.observationState;
    const encoded = host.styleObservationState(
        node === document ? realmDocumentRootID : elementSlot(node).nodeId,
      ),
      value = JSON.parse(encoded);
    value.signature = encoded;
    const data = elementSlot(node);
    value.selectorSignature =
      (data?.type || 'document') +
      ':' +
      (data?.namespaceURI || '') +
      ':' +
      (data?.tagName || '') +
      ':' +
      JSON.stringify(value.attributes);
    if (record) {
      record.observationState = value;
      record.parent = wrap(value.parent);
      record.attributeNames = Object.keys(value.attributes);
      record.attributes = new Map(Object.entries(value.attributes));
    }
    return value;
  };
  const cssObservationAttribute = (node, name) => {
    const data = elementSlot(node);
    if (
      data.namespaceURI === 'http://www.w3.org/1999/xhtml' ||
      (!data.namespaceURI && !data.qualifiedName)
    )
      name = name.toLowerCase();
    const attrs = cssObservationNodeState(node).attributes;
    return Object.hasOwn(attrs, name) ? attrs[name] : null;
  };
  const cssObservationParent = (node) => {
    const cache =
      styleReadCache &&
      (styleReadCache.nodeParents || (styleReadCache.nodeParents = new WeakMap()));
    if (cache?.has(node)) return cache.get(node);
    const data = elementSlot(node),
      parent =
        syntheticParents.get(node) ||
        (data
          ? wrap(
              styleReadCache ? cssObservationNodeState(node).parent : host.parentNode(data.nodeId),
            )
          : null);
    cache?.set(node, parent);
    return parent;
  };
  const cssObservationChildren = (node) => {
    const cache =
      styleReadCache &&
      (styleReadCache.nodeChildren || (styleReadCache.nodeChildren = new WeakMap()));
    if (cache?.has(node)) return cache.get(node);
    const data = elementSlot(node),
      children =
        node === document || data
          ? cssObservationNodeState(node).children.map(wrap)
          : fragmentState(node)?.children.slice() || [];
    cache?.set(node, children);
    return children;
  };
  const containingShadowRoot = (element) => {
    if (shadowHosts.size === 0 && !styleObservationIsolated) return null;
    const cache =
      styleReadCache &&
      (styleReadCache.shadowRoots || (styleReadCache.shadowRoots = new WeakMap()));
    if (cache?.has(element)) return cache.get(element);
    const visited = [];
    let root = null;
    for (let node = element; node; node = cssObservationParent(node)) {
      if (cache?.has(node)) {
        root = cache.get(node);
        break;
      }
      if (typeof ShadowRoot === 'function' && node instanceof ShadowRoot) {
        root = node;
        break;
      }
      visited.push(node);
    }
    if (cache) for (const node of visited) cache.set(node, root);
    return root;
  };
  // Cache only parsed immutable source text, never computed element state.
  // A bounded cache cannot retain unbounded revisions of author stylesheets.
  const parsedStyleSources = new Map();
  let parsedStyleSourceBytes = 0;
  const parsedStyleRules = (text) => {
    text = String(text);
    if (parsedStyleSources.has(text)) return parsedStyleSources.get(text);
    const rules = [],
      generate = mimicSelectorLibrary.generateCSS;
    const walk = (nodes, conditions = []) => {
      for (const node of nodes) {
        if (node.type === 'Atrule') {
          if (['media', 'supports', 'layer'].includes(node.name) && node.block)
            walk(
              node.block.children,
              node.name === 'layer'
                ? conditions
                : [...conditions, { type: node.name, value: generate(node.prelude) }],
            );
          continue;
        }
        if (node.type !== 'Rule' || !node.block) continue;
        const selectors =
          node.prelude?.type === 'SelectorList'
            ? Array.from(node.prelude.children, generate)
            : [generate(node.prelude)];
        const body = Array.from(node.block.children)
          .filter((n) => n.type === 'Declaration')
          .map(
            (n) => n.property + ':' + generate(n.value) + (n.important ? ' !important' : '') + ';',
          )
          .join('');
        let entries;
        const declarations = () => entries || (entries = parseCSS(body));
        for (const selector of selectors) {
          const specificity =
            (selector.match(/#[\w-]+/g) || []).length * 100 +
            (selector.match(/\.[\w-]+|\[[^\]]+\]|::?[\w-]+/g) || []).length * 10 +
            (selector.match(/(^|[\s>+~])[a-zA-Z][\w-]*/g) || []).length;
          const pseudo = /::?(before|after)$/.exec(selector)?.[1] || '';
          let matcher;
          const scopedMatchers = new WeakMap();
          rules.push({
            selector,
            declarations,
            specificity,
            pseudo,
            matches: (node, shadowRoot = null) =>
              conditions.every((condition) =>
                condition.type === 'media'
                  ? cssMediaMatches(condition.value)
                  : compatibilityCSSSupports.matches?.(condition.value) === true,
              ) &&
              (
                (shadowRoot
                  ? scopedMatchers.get(shadowRoot) ||
                    (() => {
                      const compiled = compatibilitySelectors.compileStyle(
                        pseudo ? selector.replace(/::?(before|after)$/, '') : selector,
                        shadowRoot,
                      );
                      scopedMatchers.set(shadowRoot, compiled);
                      return compiled;
                    })()
                  : matcher) ||
                (matcher = compatibilitySelectors.compileStyle(
                  pseudo ? selector.replace(/::?(before|after)$/, '') : selector,
                ))
              )(node),
          });
        }
      }
    };
    try {
      walk(mimicSelectorLibrary.parseStylesheet(text).children);
    } catch (error) {
      if (error?.name !== 'SyntaxError') throw error;
    }
    const size = text.length * 2;
    if (size <= 8 * 1024 * 1024) {
      while (
        parsedStyleSources.size &&
        (parsedStyleSources.size >= 64 || parsedStyleSourceBytes + size > 8 * 1024 * 1024)
      ) {
        const oldest = parsedStyleSources.keys().next().value;
        parsedStyleSources.delete(oldest);
        parsedStyleSourceBytes -= oldest.length * 2;
      }
      parsedStyleSources.set(text, rules);
      parsedStyleSourceBytes += size;
    }
    return rules;
  };
  // Cache only the ordered rule program. Revalidate canonical owner sheets on
  // every read; computed matches and element state never survive the read.
  const orderedStyleRules = new WeakMap();
  const styleSheetRules = (element, selectedRoot = null) => {
    const root =
        selectedRoot ||
        containingShadowRoot(element) ||
        wrap(cssObservationNodeState(element).ownerDocument) ||
        document,
      cache = styleReadCache?.rules;
    if (cache?.has(root)) return cache.get(root);
    const sources = constructedStyleSheets.sources(root),
      prior = orderedStyleRules.get(root);
    if (root === document) {
      const dependencyBytes = sources.reduce((size, source) => size + source.length * 2, 0),
        unsafe =
          dependencyBytes > 8 * 1024 * 1024 || sources.some((source) => source.includes('\\'));
      retainedStyleAttributeDependencies.set(root, {
        unsafe,
        // Bound the auxiliary lowercase projection independently of the rule
        // cache. Oversized programs simply retain the old invalidation path.
        sources: unsafe ? [] : sources.map((source) => source.toLowerCase()),
      });
    }
    let rules = prior?.rules;
    if (
      !prior ||
      sources.length !== prior.sources.length ||
      sources.some((text, index) => text !== prior.sources[index])
    ) {
      rules = [];
      let order = 0;
      for (const text of sources)
        for (const rule of parsedStyleRules(text))
          rules.push({
            ...rule,
            order: order++,
            matches: (node) =>
              rule.matches(
                node,
                root instanceof ShadowRoot && /:host(?:\b|[-(])/.test(rule.selector) ? root : null,
              ),
          });
      orderedStyleRules.set(root, { sources, rules });
    }
    cache?.set(root, rules);
    return rules;
  };
  let styleCascades = new WeakMap();
  bootstrapRestoreHooks.push(() => {
    styleCascades = new WeakMap();
  });
  let blitzLegacyTraces = 0;
  const uncachedCSSDeclarations = (element, pseudo = '') => {
    if (blitzTraceEnabled && blitzLegacyTraces++ < 4) host.blitzLegacyTrace(new Error().stack);
    const matched = compatibilitySelectors.matchingStyles(
      element,
      styleSheetRules(element),
      pseudo,
    );
    const ownShadow = elementShadows.get(element);
    if (ownShadow)
      matched.push(
        ...compatibilitySelectors
          .matchingStyles(element, styleSheetRules(element, ownShadow), pseudo)
          .filter((rule) => /:host(?:\b|[-(])/.test(rule.selector)),
      );
    const inline = pseudo ? '' : cssObservationNodeState(element).inline;
    const dialog =
      !pseudo && elementSlot(element)?.tagName === 'DIALOG'
        ? host.getAttribute(elementSlot(element).nodeId, 'open') === null
          ? 'none'
          : 'block'
        : null;
    let records = styleReadCache?.retainable ? styleCascades.get(element) : null;
    const previous = records?.get(pseudo);
    // Specified declarations depend on the ordered matching rules and precise
    // inline state, not on unrelated DOM writes or inherited computed values.
    // Reuse only after revalidating all inputs; geometry still gets a new graph.
    if (
      previous &&
      previous.inline === inline &&
      previous.dialog === dialog &&
      previous.matched.length === matched.length &&
      matched.every((rule, i) => rule === previous.matched[i])
    )
      return previous.entries;
    const winners = new Map();
    const accept = (entry, specificity, order) => {
      if (entry.name === 'all') {
        for (const name of cssComputedNames)
          if (name !== 'direction' && name !== 'unicode-bidi')
            accept(
              {
                name,
                value:
                  entry.value === 'initial' ? cssInitialValues.get(name) || 'initial' : entry.value,
                priority: entry.priority,
                allReset: true,
              },
              specificity,
              order,
            );
        return;
      }
      const old = winners.get(entry.name),
        important = entry.priority === 'important';
      if (
        !old ||
        Number(important) > Number(old.important) ||
        (important === old.important &&
          (specificity > old.specificity ||
            (specificity === old.specificity && order >= old.order)))
      )
        winners.set(entry.name, { entry, specificity, order, important });
    };
    if (!pseudo && elementSlot(element)?.tagName === 'IFRAME') {
      for (const side of ['top', 'right', 'bottom', 'left']) {
        accept({ name: `border-${side}-style`, value: 'inset', priority: '' }, -1, -1);
        accept({ name: `border-${side}-width`, value: '2px', priority: '' }, -1, -1);
      }
    }
    if (dialog) accept({ name: 'display', value: dialog, priority: '' }, -1, -1);
    for (const rule of matched)
      for (const entry of rule.declarations()) accept(entry, rule.specificity, rule.order);
    if (!pseudo)
      for (const entry of inline[0] === 'j'
        ? JSON.parse(inline.slice(1))
        : parseCSS(inline.slice(1)))
        accept(entry, 1000, Number.MAX_SAFE_INTEGER);
    const entries = Array.from(winners.values(), (value) => {
      const entry = { ...value.entry };
      if (entry.pending?.systemFont) {
        const resolved = parseCSSFont(host.systemFonts()[entry.pending.value]);
        entry.value = resolved?.[cssShorthandComponents.font.indexOf(entry.name)] || 'initial';
        delete entry.pending;
      }
      return entry;
    });
    if (styleReadCache?.retainable) {
      if (!records) styleCascades.set(element, (records = new Map()));
      records.set(pseudo, { matched, inline, dialog, entries });
    }
    return entries;
  };
  // This private declaration array is read-only to all consumers and never
  // escapes to author code. Recopying every property on each ancestor lookup
  // made a single geometry observation allocate millions of short-lived entries.
  const computedCSSDeclarations = (element) => {
    const cache = styleReadCache?.declarations;
    if (cache?.has(element)) return cache.get(element);
    const entries = uncachedCSSDeclarations(element);
    cache?.set(element, entries);
    return entries;
  };
  const blockifiedDisplay = (value) =>
    ({
      inline: 'block',
      'inline-block': 'block',
      'inline-table': 'table',
      'inline-flex': 'flex',
      'inline-grid': 'grid',
    })[String(value).toLowerCase()] || value;
  // Resolve fallback geometry only when its value is read. Unrelated style reads
  // and property enumeration must not run layout or invoke author-defined getters.
  const inlineCSSDeclarations = (element) => {
    const value = host.inlineStyleState(elementSlot(element).nodeId);
    return value[0] === 'j' ? JSON.parse(value.slice(1)) : parseCSS(value.slice(1));
  };
  const cssUsedColorScheme = (element) => {
    let scheme = 'light';
    for (let n = element; n && elementSlot(n); n = cssFontParent(n)) {
      const value = computedCSSDeclarations(n).find((e) => e.name === 'color-scheme')?.value;
      if (!value || ['inherit', 'unset'].includes(value)) continue;
      const words = value.toLowerCase().split(/\s+/);
      if (
        words.includes('dark') &&
        (!words.includes('light') || host.media('(prefers-color-scheme: dark)'))
      )
        scheme = 'dark';
      break;
    }
    return scheme;
  };
  const cssResolvedColor = (element) => {
    const scheme = cssUsedColorScheme(element);
    for (let n = element; n && elementSlot(n); n = cssFontParent(n)) {
      const value = geometryValue(
        n,
        computedCSSDeclarations(n).find((e) => e.name === 'color')?.value,
      );
      if (!value || ['inherit', 'unset', 'currentcolor'].includes(value.toLowerCase())) continue;
      if (value === 'initial') break;
      const rgba = cssColorRGBA(value, scheme);
      if (rgba) return cssSerializeColor(rgba);
      host.semanticMissingAt(
        'surface.js/cssResolvedColor',
        'CSS.colorResolution',
        JSON.stringify({ reason: 'unsupported computed color', value }),
      );
      return value;
    }
    return 'rgb(0, 0, 0)';
  };
  // Resolved style requires flat-tree participation as well as connectivity.
  // Derive named distribution from the existing shadow/DOM ownership; a light
  // child of a shadow host without a matching slot is connected but excluded.
  const computedStyleSlot = (root, node) => {
    if (shadowSlots.get(root)?.slotAssignment === 'manual') return null;
    const data = elementSlot(node),
      name = data?.type === 'element' ? host.getAttribute(data.nodeId, 'slot') || '' : '';
    return (
      compatibilitySelectors
        .query(root, 'slot')
        .find((slot) => (host.getAttribute(elementSlot(slot).nodeId, 'name') || '') === name) ||
      null
    );
  };
  // Foreign projections execute in the canonical owner realm: synthetic shadow
  // connectivity and viewport state are not duplicated into borrowed wrappers.
  const foreignCSSObservation = (element, kind = '', name = '') => {
    if (
      !styleObservationIsolated &&
      cssObservationNodeState(element).ownerDocument === realmDocumentRootID
    )
      return null;
    const owners = styleReadCache
      ? styleReadCache.foreignCSSOwners || (styleReadCache.foreignCSSOwners = new WeakMap())
      : null;
    if (owners?.get(element) === false) return null;
    const value = host.foreignComputedStyleFlatTree(elementSlot(element).nodeId, kind, name);
    if (
      value !== null &&
      styleReadCache &&
      kind !== 'values' &&
      kind !== 'documentValues' &&
      kind !== 'pseudoContent' &&
      kind !== '' &&
      kind !== 'document'
    )
      styleReadCache.retainable = false;
    owners?.set(element, value !== null);
    return value;
  };
  let blitzProducerEnabled = host.blitzEnabled();
  let blitzShadowAdmissionDepth = 0;
  let blitzTraceEnabled = host.blitzTracing();
  let checkpointBlitzPacked = null,
    checkpointBlitzEpoch = '';
  bootstrapRestoreHooks.push(() => {
    blitzProducerEnabled = host.blitzEnabled();
    blitzTraceEnabled = host.blitzTracing();
    blitzLegacyTraces = 0;
    checkpointBlitzPacked = null;
    checkpointBlitzEpoch = '';
  });
  const blitzProperties = [
    'display',
    'visibility',
    'cursor',
    'content-visibility',
    'opacity',
    'pointer-events',
    'position',
    'z-index',
    'overflow-x',
    'overflow-y',
    'direction',
    'font-size',
    'transform',
    'transform-origin',
    'isolation',
    'perspective',
    'content',
    // Resource discovery reads this for every displayed element. Project it
    // with the other columns instead of one host/native call per element.
    'background-image',
  ];
  const decodeBlitzPacket = (buffer, decoder = new TextDecoder()) => {
    const view = new DataView(buffer);
    if (view.getUint32(0, true) !== 2 || view.getUint32(12, true) !== blitzProperties.length)
      throw new Error('Blitz snapshot schema mismatch');
    return {
      view,
      buffer,
      count: view.getUint32(4, true),
      stride: view.getUint32(8, true),
      strings: new Map(),
      decoder,
      nodeStyles: new Map(),
      scalarValues: new Map(),
      batchedStyles: new Set(),
      transforms: new Map(),
      retainedBytes: 0,
    };
  };
  const blitzPackedRecord = (element) => {
    if (!blitzProducerEnabled) return null;
    if (blitzShadowAdmissionDepth) return null;
    if (!styleReadCache) return withStyleReadCache(() => blitzPackedRecord(element));
    // The native producer consumes the light tree. A shadow host and its
    // descendants need the composed-tree observer; other branches can keep
    // their native layout instead of falling back for the whole document.
    if (shadowHosts.size) {
      const affected =
        styleReadCache.blitzShadowAffected || (styleReadCache.blitzShadowAffected = new WeakMap());
      if (!affected.has(element)) {
        const visited = [];
        let node = element;
        let inShadow = false;
        while (node) {
          if (affected.has(node)) {
            inShadow = affected.get(node);
            break;
          }
          visited.push(node);
          if (elementShadows.has(node) || shadowSlots.has(node)) {
            inShadow = true;
            break;
          }
          node = cssObservationParent(node);
        }
        for (const visitedNode of visited) affected.set(visitedNode, inShadow);
      }
      if (affected.get(element)) return null;
    }
    const elementData = elementSlot(element);
    if (elementData?.tagName === 'INPUT') {
      const type = (host.getAttribute(elementData.nodeId, 'type') || 'text').toLowerCase();
      // The native producer does not project the value label into intrinsic
      // sizing for button-like inputs, including changes made during coercion.
      if (type === 'button' || type === 'submit' || type === 'reset') return null;
    }
    // Foreign membranes can refer to another arena with colliding numeric IDs.
    // Local wrappers (including isolated-world wrappers) share the canonical
    // arena; membership in the native snapshot already proves document owner.
    // Do not reconstruct attributes/inline style just to repeat that check.
    if (referenceGet(element)) return null;
    if (styleReadCache.blitzPacked === undefined) {
      const epoch = styleReadCache.blitzEpochKey;
      if (checkpointBlitzEpoch === epoch) styleReadCache.blitzPacked = checkpointBlitzPacked;
      else {
        const buffer = host.blitzObserve(0, 'snapshot', '');
        if (buffer === null) styleReadCache.blitzPacked = null;
        else styleReadCache.blitzPacked = decodeBlitzPacket(buffer);
        checkpointBlitzPacked = styleReadCache.blitzPacked;
        checkpointBlitzEpoch = epoch;
      }
    }
    const packed = styleReadCache.blitzPacked;
    if (!packed) return null;
    const id = Number(elementSlot(element).nodeId);
    let low = 0,
      high = packed.count - 1;
    while (low <= high) {
      const mid = (low + high) >>> 1,
        at = 16 + mid * packed.stride;
      const current =
        packed.view.getUint32(at, true) + packed.view.getUint32(at + 4, true) * 4294967296;
      if (current === id) return { packed, at };
      if (current < id) low = mid + 1;
      else high = mid - 1;
    }
    return null;
  };
  const blitzStyleValue = (element, name) => {
    // Pinned Stylo treats user-select as non-inherited; the Chrome profile's
    // used inheritance is still owned by the migration correctness oracle.
    if (name === 'user-select' || name === '-webkit-user-select') return null;
    // SVG reference boxes/presentation attributes belong to the existing SVG
    // observation engine until that domain is natively represented.
    if (
      elementSlot(element)?.namespaceURI === 'http://www.w3.org/2000/svg' &&
      (name === 'transform' || name === 'font-size')
    )
      return null;
    const record = blitzPackedRecord(element);
    if (!record) return null;
    const column = blitzProperties.indexOf(name);
    if (column < 0) {
      const { packed, at } = record,
        key = at + ':' + name;
      if (packed.scalarValues.has(key)) return packed.scalarValues.get(key);
      const value = host.blitzObserve(elementSlot(element).nodeId, 'style', name);
      const bytes = typeof value === 'string' ? value.length * 2 : 0;
      if (
        name.length <= 128 &&
        packed.scalarValues.size < 4096 &&
        packed.retainedBytes + bytes < 64 * 1024 * 1024
      ) {
        packed.scalarValues.set(key, value);
        packed.retainedBytes += bytes;
      }
      return value;
    }
    let { packed, at } = record;
    if (packed.view.getUint32(at + 80 + column * 8, true) === 4294967295) {
      let hidden = packed.nodeStyles.get(at);
      if (!hidden) {
        const buffer = host.blitzObserve(elementSlot(element).nodeId, 'node', '');
        if (buffer === null) return null;
        hidden = decodeBlitzPacket(buffer, packed.decoder);
        if (packed.retainedBytes + buffer.byteLength < 64 * 1024 * 1024) {
          packed.nodeStyles.set(at, hidden);
          packed.retainedBytes += buffer.byteLength;
        }
      }
      packed = hidden;
      at = 16;
    }
    const offset = packed.view.getUint32(at + 80 + column * 8, true),
      length = packed.view.getUint32(at + 84 + column * 8, true);
    if (!packed.strings.has(offset))
      packed.strings.set(
        offset,
        packed.decoder.decode(new Uint8Array(packed.buffer, offset, length)),
      );
    return packed.strings.get(offset);
  };
  // Box existence and observation eligibility differ for skipped contents:
  // Chrome exposes real descendant rects without allowing hit testing or IO.
  const blitzSkippedContent = (element) => {
    const record = blitzPackedRecord(element);
    return record ? !!(record.packed.view.getUint32(record.at + 72, true) & 1) : null;
  };
  const blitzLayoutRect = (element) => {
    const record = blitzPackedRecord(element);
    if (!record) return null;
    const { packed, at } = record,
      read = (i) => packed.view.getFloat64(at + 8 + i * 8, true),
      x = read(0),
      y = read(1),
      width = read(2),
      height = read(3);
    return {
      x,
      y,
      left: x,
      top: y,
      width,
      height,
      right: x + width,
      bottom: y + height,
      clientWidth: read(4),
      clientHeight: read(5),
      contentWidth: read(6),
      contentHeight: read(7),
      overflowWidth: read(6),
    };
  };
  const computedStyleDocumentAvailable = (element) => {
    if (blitzPackedRecord(element))
      return styleReadCache
        ? (styleReadCache.documentActive ??= host.documentActive())
        : host.documentActive();
    const foreign = foreignCSSObservation(element, 'document');
    if (foreign !== null) return foreign;
    const cache =
      styleReadCache &&
      (styleReadCache.documentAvailability ||
        (styleReadCache.documentAvailability = new WeakMap()));
    if (cache?.has(element)) return cache.get(element);
    const active = styleReadCache
      ? (styleReadCache.documentActive ??= host.documentActive())
      : host.documentActive();
    if (!active) {
      cache?.set(element, false);
      return false;
    }
    let node = element;
    while (node) {
      const data = elementSlot(node);
      const state = data && cssObservationNodeState(node);
      if (state?.connected && state.ownerDocument === realmDocumentRootID) {
        cache?.set(element, true);
        return true;
      }
      if (shadowSlots.has(node)) {
        node = shadowSlots.get(node).host;
        continue;
      }
      node = syntheticParents.get(node) || (data ? wrap(host.parentNode(data.nodeId)) : null);
    }
    cache?.set(element, false);
    return false;
  };
  const computedStyleAvailable = (element) => {
    // A native record exists only for connected nodes in a supported flat
    // tree. Unsupported shadow/slot documents use the original owner path.
    if (blitzPackedRecord(element)) return true;
    const foreign = foreignCSSObservation(element);
    if (foreign !== null) return foreign;
    const cache =
        styleReadCache &&
        (styleReadCache.flatTreeAvailability ||
          (styleReadCache.flatTreeAvailability = new WeakMap())),
      visited = [];
    const finish = (value) => {
      if (cache) for (const item of visited) cache.set(item, value);
      return value;
    };
    let node = element;
    while (node) {
      if (cache?.has(node)) return finish(cache.get(node));
      visited.push(node);
      if (shadowSlots.has(node)) {
        node = shadowSlots.get(node).host;
        continue;
      }
      const parent = cssObservationParent(node);
      const root = elementShadows.get(parent);
      if (root) {
        node = computedStyleSlot(root, node);
        if (!node) return finish(false);
        continue;
      }
      // Fallback content is absent when its slot has assigned light children.
      const parentData = elementSlot(parent);
      if (parentData?.tagName === 'SLOT') {
        const root = containingShadowRoot(parent),
          shadow = shadowSlots.get(root);
        if (
          shadow &&
          cssObservationChildren(shadow.host).some(
            (child) =>
              ['element', 'text'].includes(elementSlot(child)?.type) &&
              computedStyleSlot(root, child) === parent,
          )
        )
          return finish(false);
      }
      node = parent;
    }
    return finish(true);
  };
  const cssSlots = new WeakMap(),
    cssState = (value) => cssSlots.get(value),
    cssComputedEnumerationAvailable = (state) => {
      // Enumeration reads hundreds of numeric keys. Revalidate the epoch
      // without repeating the style availability walk for every key.
      const version =
        host.observationVersion() +
        ':' +
        compatibilityElementState.observationVersion() +
        ':' +
        host.documentActive();
      if (state.availabilityVersion !== version) {
        state.available = computedStyleDocumentAvailable(state.element);
        state.availabilityVersion = version;
      }
      return state.available;
    },
    cssEntries = (value) => {
      const state = cssState(value);
      if (!state.computed) return inlineCSSDeclarations(state.element);
      if (!computedStyleDocumentAvailable(state.element)) return [];
      return cssComputedNames.map((name) => ({
        name,
        get value() {
          return cssComputedValue(state.element, name, state.pseudo);
        },
        priority: '',
      }));
    },
    writeCSSEntries = (value, entries) => {
      const state = cssState(value);
      if (state.computed)
        throw platformDOMException(
          'These styles are computed, and therefore the CSSStyleDeclaration is read-only.',
          'NoModificationAllowedError',
        );
      for (const iframe of elementWrappers.values())
        if (iframe instanceof HTMLIFrameElement && frameViewportSizes.has(iframe))
          readFrameViewport(elementSlot(iframe).nodeId);
      const text = serializeCSS(entries),
        old = host.setInlineStyle(elementSlot(state.element).nodeId, text, JSON.stringify(entries));
      elementSlot(state.element).attributes.style = text;
      // Native shadow descendants are stored detached and therefore cannot
      // advance the active document's connected revision on their own.
      if (containingShadowRoot(state.element)) host.invalidateStyleObservations();
      compatibilityElementState.inlineStyleChanged(state.element, old);
    };
  class CSSStyleDeclaration {
    constructor(token, element, computed = false, pseudo = '') {
      if (token !== hostToken) illegal('CSSStyleDeclaration');
      cssSlots.set(this, { element, computed, pseudo });
    }
    get length() {
      const state = cssState(this);
      if (state.computed)
        return cssComputedEnumerationAvailable(state) ? cssComputedNames.length : 0;
      return cssEntries(this).length;
    }
    get cssText() {
      return cssState(this).computed ? '' : serializeCSS(cssEntries(this));
    }
    set cssText(v) {
      writeCSSEntries(this, parseCSS(String(v)));
    }
    item(i) {
      const state = cssState(this),
        index = Number(i);
      if (state.computed)
        return cssComputedEnumerationAvailable(state) ? cssComputedNames[index] || '' : '';
      return cssEntries(this)[index]?.name || '';
    }
    getPropertyValue(name) {
      name = cssName(name);
      const state = cssState(this);
      return state.computed
        ? cssComputedValue(state.element, name, state.pseudo)
        : readCSSDeclaration(cssEntries(this), name);
    }
    getPropertyPriority(name) {
      if (cssState(this).computed) return '';
      name = cssName(name);
      const entries = cssEntries(this),
        components = cssShorthandComponents[name];
      if (components) {
        const selected = components.map((n) => entries.find((e) => e.name === n));
        return selected.every((e) => e?.priority === 'important') ? 'important' : '';
      }
      return entries.find((e) => e.name === name)?.priority || '';
    }
    setProperty(name, value, priority = '') {
      const inputName = name;
      name = cssName(name);
      if (/^webkit/i.test(name)) return;
      const inputValue = String(value),
        normalized = normalizeCSSValue(name, inputValue, inputName);
      if (normalized === null) return;
      value = normalized;
      priority = String(priority).toLowerCase();
      if (!name || (priority && priority !== 'important')) return;
      const components = cssShorthandComponents[name] || [name],
        entries = cssEntries(this);
      if (String(value) === '') {
        for (let i = entries.length - 1; i >= 0; i--)
          if (components.includes(entries[i].name) || entries[i].name === name)
            entries.splice(i, 1);
      } else
        for (const entry of expandCSSDeclaration(
          cssPrecisionDeclaration({ name, value: String(value), priority }, inputValue),
        )) {
          const index = entries.findIndex((e) => e.name === entry.name);
          if (index < 0) entries.push(entry);
          else entries[index] = entry;
        }
      writeCSSEntries(this, entries);
    }
    removeProperty(name) {
      const shorthand = webkitCSSLegacyBreakShorthands.has(String(name).toLowerCase());
      name = cssName(name);
      const entries = cssEntries(this),
        old = readCSSDeclaration(entries, name),
        components = cssShorthandComponents[name] || [name];
      writeCSSEntries(
        this,
        entries.filter((x) => !components.includes(x.name) && x.name !== name),
      );
      return shorthand || cssShorthandComponents[name] ? '' : old;
    }
  }
  // Blink exposes these legacy names during CSSStyleDeclaration key
  // enumeration although they have no property descriptor or value.
  const cssEnumeratedNames = Object.keys(cssNamedProperties)
    .concat([
      'epubCaptionSide',
      'epubTextCombine',
      'epubTextEmphasis',
      'epubTextEmphasisColor',
      'epubTextEmphasisStyle',
      'epubTextOrientation',
      'epubTextTransform',
      'epubWordBreak',
      'epubWritingMode',
    ])
    .sort();
  const cssScalarBatchNames = Array.from(new Set(Object.values(cssNamedProperties))).filter(
    (name) => !blitzProperties.includes(name),
  );
  const cssDeclaration = (element, computed = false, pseudo = '') => {
    const prefetchComputedNames = () => {
      if (!computed || pseudo) return;
      withStyleReadCache(() => {
        const record = blitzPackedRecord(element);
        if (!record || record.packed.batchedStyles.has(record.at)) return;
        // Empty native serializers retain the ordinary JS fallback per name.
        const values = host.blitzObserve(
          elementSlot(element).nodeId,
          'styles',
          JSON.stringify(cssScalarBatchNames),
        );
        if (!Array.isArray(values) || values.length !== cssScalarBatchNames.length) return;
        const { packed, at } = record;
        for (let i = 0; i < cssScalarBatchNames.length; i++) {
          const value = values[i];
          const bytes = typeof value === 'string' ? value.length * 2 : 0;
          if (packed.retainedBytes + bytes >= 64 * 1024 * 1024) break;
          packed.scalarValues.set(at + ':' + cssScalarBatchNames[i], value);
          packed.retainedBytes += bytes;
        }
        packed.batchedStyles.add(at);
      });
    };
    const target = new CSSStyleDeclaration(hostToken, element, computed, pseudo),
      proxy = new Proxy(target, {
        get(t, p, r) {
          if (typeof p === 'string') {
            if (/^(0|[1-9][0-9]*)$/.test(p))
              return Number(p) < t.length ? t.item(Number(p)) : undefined;
            if (!Reflect.has(t, p)) {
              if (!Object.hasOwn(cssNamedProperties, p) && !webkitJSNames.has(p)) return undefined;
              return t.getPropertyValue(cssJSName(p));
            }
          }
          return Reflect.get(t, p, r);
        },
        set(t, p, v, r) {
          if (typeof p === 'string' && !Reflect.has(t, p)) {
            if (/^-?webkit/i.test(p) && !webkitJSNames.has(p)) return Reflect.set(t, p, v, r);
            t.setProperty(cssJSInputName(p), String(v));
            return true;
          }
          return Reflect.set(t, p, v, r);
        },
        has(t, p) {
          return (
            (typeof p === 'string' && /^(0|[1-9][0-9]*)$/.test(p) && Number(p) < t.length) ||
            Object.hasOwn(cssNamedProperties, p) ||
            webkitJSNames.has(p) ||
            Reflect.has(t, p)
          );
        },
        ownKeys(t) {
          prefetchComputedNames();
          return Array.from(
            new Set([
              ...Array.from({ length: t.length }, (_, i) => String(i)),
              ...Reflect.ownKeys(t),
              ...cssEnumeratedNames,
            ]),
          );
        },
        getOwnPropertyDescriptor(t, p) {
          if (typeof p === 'string' && /^(0|[1-9][0-9]*)$/.test(p) && Number(p) < t.length)
            return {
              value: t.item(Number(p)),
              writable: false,
              enumerable: true,
              configurable: true,
            };
          if (Object.hasOwn(cssNamedProperties, p) || webkitJSNames.has(p))
            return {
              value: t.getPropertyValue(cssJSName(p)),
              writable: true,
              enumerable: Object.hasOwn(cssNamedProperties, p),
              configurable: true,
            };
          return Reflect.getOwnPropertyDescriptor(t, p);
        },
      });
    cssSlots.set(proxy, cssSlots.get(target));
    return proxy;
  };
  const elementData = new WeakMap(),
    elementClassLists = new WeakMap(),
    elementShadows = new WeakMap(),
    shadowHosts = new Set(),
    syntheticParents = new WeakMap(),
    elementSlot = (value) => elementData.get(value);
  let shadowSnapshotRevision = 0;
  const setSyntheticParent = (node, parent) => {
    syntheticParents.set(node, parent);
    if (typeof ShadowRoot === 'function' && parent instanceof ShadowRoot) shadowSnapshotRevision++;
    host.invalidateStyleObservations();
  };
  const deleteSyntheticParent = (node) => {
    const parent = syntheticParents.get(node),
      removed = syntheticParents.delete(node);
    if (removed) {
      if (typeof ShadowRoot === 'function' && parent instanceof ShadowRoot)
        shadowSnapshotRevision++;
      host.invalidateStyleObservations();
    }
    return removed;
  };
  const setElementShadow = (node, root) => {
    elementShadows.set(node, root);
    shadowSnapshotRevision++;
    host.invalidateStyleObservations();
  };
  // WebIDL Node branding is independent of the caller realm's instanceof.
  const nonHostNodeBrands = new WeakSet();
  const isDOMNode = (value) =>
    elementData.has(value) ||
    fragmentSlots.has(value) ||
    nonHostNodeBrands.has(value) ||
    value === document;
  const isDOMFragment = (value) =>
    elementSlot(value)?.type === 'fragment' || fragmentSlots.has(value);
  const fragmentSlots = new WeakMap(),
    shadowSlots = new WeakMap(),
    fragmentState = (value) => fragmentSlots.get(value);
  class DocumentFragment extends Node {
    constructor(token) {
      super(hostToken);
      if (new.target === ShadowRoot) {
        fragmentSlots.set(this, { children: [], html: '' });
      } else {
        const data = host.createDocumentFragment();
        elementData.set(this, data);
        elementWrappers.set(String(data.nodeId), this);
      }
    }
    get nodeName() {
      return '#document-fragment';
    }
    get textContent() {
      const state = fragmentState(this);
      return state.children.length
        ? state.children
            .filter((child) => child.nodeType !== 8)
            .map((child) => child.textContent || '')
            .join('')
        : state.html.replace(/<[^>]*>/g, '');
    }
    set textContent(value) {
      const state = fragmentState(this);
      for (const child of state.children) deleteSyntheticParent(child);
      state.children = [];
      state.html = value == null ? '' : String(value);
      host.invalidateStyleObservations();
    }
    get children() {
      return cachedHTMLCollection(this, 'children', '', () =>
        fragmentState(this)
          .children.map((child) => elementSlot(child))
          .filter((data) => data?.type === 'element'),
      );
    }
    get firstElementChild() {
      return fragmentState(this).children.find((child) => child instanceof Element) || null;
    }
    get lastElementChild() {
      return fragmentState(this).children.findLast
        ? fragmentState(this).children.findLast((child) => child instanceof Element)
        : fragmentState(this)
            .children.slice()
            .reverse()
            .find((child) => child instanceof Element) || null;
    }
    get childElementCount() {
      return fragmentState(this).children.filter((child) => child instanceof Element).length;
    }
    appendChild(node) {
      const state = fragmentState(this);
      if (!isDOMNode(node))
        throw new TypeError(
          "Failed to execute 'appendChild' on 'Node': parameter 1 is not of type 'Node'.",
        );
      const old = syntheticParents.get(node);
      if (old && fragmentSlots.has(old)) old.removeChild(node);
      state.children.push(node);
      state.html = '';
      setSyntheticParent(node, this);
      return node;
    }
    removeChild(node) {
      const state = fragmentState(this),
        index = state.children.indexOf(node);
      if (index < 0)
        throw platformDOMException(
          'The node to be removed is not a child of this node.',
          'NotFoundError',
        );
      state.children.splice(index, 1);
      deleteSyntheticParent(node);
      return node;
    }
    append(...nodes) {
      for (const node of nodes)
        this.appendChild(isDOMNode(node) ? node : document.createElement('span'));
    }
    prepend(...nodes) {
      for (let i = nodes.length - 1; i >= 0; i--) {
        const node = isDOMNode(nodes[i]) ? nodes[i] : document.createElement('span');
        fragmentState(this).children.unshift(node);
        setSyntheticParent(node, this);
      }
    }
    replaceChildren(...nodes) {
      const state = fragmentState(this);
      for (const child of state.children) deleteSyntheticParent(child);
      state.children = [];
      state.html = '';
      this.append(...nodes);
    }
    querySelector(selector) {
      return this.querySelectorAll(selector)[0] || null;
    }
    querySelectorAll(selector) {
      const query = String(selector).trim(),
        result = [];
      const matches = (node) =>
        node instanceof Element &&
        (query.startsWith('#')
          ? node.id === query.slice(1)
          : query.startsWith('.')
            ? node.classList.contains(query.slice(1))
            : node.localName === query.toLowerCase());
      const visit = (node) => {
        if (matches(node)) result.push(node);
        if (node instanceof Element) for (const child of Array.from(node.children)) visit(child);
      };
      for (const child of fragmentState(this).children) visit(child);
      return nodeListView(
        () => result.length,
        (index) => result[index],
      );
    }
    getElementById(id) {
      return this.querySelector('#' + String(id));
    }
  }
  class ShadowRoot extends DocumentFragment {
    constructor(token, hostElement, mode, init) {
      if (token !== hostToken) illegal('ShadowRoot');
      super(token);
      shadowSlots.set(this, {
        host: hostElement,
        mode,
        delegatesFocus: !!init.delegatesFocus,
        slotAssignment: String(init.slotAssignment || 'named'),
        serializable: !!init.serializable,
        clonable: !!init.clonable,
        onslotchange: null,
      });
      shadowHosts.add(hostElement);
    }
    get mode() {
      return shadowSlots.get(this).mode;
    }
    get host() {
      return shadowSlots.get(this).host;
    }
    get delegatesFocus() {
      return shadowSlots.get(this).delegatesFocus;
    }
    get slotAssignment() {
      return shadowSlots.get(this).slotAssignment;
    }
    get serializable() {
      return shadowSlots.get(this).serializable;
    }
    get clonable() {
      return shadowSlots.get(this).clonable;
    }
    get onslotchange() {
      return shadowSlots.get(this).onslotchange;
    }
    set onslotchange(value) {
      shadowSlots.get(this).onslotchange = typeof value === 'function' ? value : null;
    }
    get innerHTML() {
      return fragmentState(this).html;
    }
    set innerHTML(value) {
      const state = fragmentState(this);
      for (const child of state.children) deleteSyntheticParent(child);
      state.children = [];
      state.html = value == null ? '' : String(value);
      host.invalidateStyleObservations();
    }
    getHTML() {
      return this.innerHTML;
    }
    setHTMLUnsafe(value) {
      this.innerHTML = value;
    }
  }
  const registerPolyfilledShadowRoot = (root) => {
    if (!(root instanceof ShadowRoot) || shadowSlots.has(root)) return;
    let hostElement, mode;
    try {
      hostElement = root.host;
      mode = String(root.mode || 'open');
    } catch {
      return;
    }
    if (!(hostElement instanceof Element) || elementShadows.has(hostElement)) return;
    if (mode !== 'open' && mode !== 'closed') mode = 'open';
    const read = (name, fallback) => {
      try {
        const value = root[name];
        return value === undefined ? fallback : value;
      } catch {
        return fallback;
      }
    };
    shadowSlots.set(root, {
      host: hostElement,
      mode,
      delegatesFocus: !!read('delegatesFocus', false),
      slotAssignment: String(read('slotAssignment', 'named')),
      serializable: !!read('serializable', false),
      clonable: !!read('clonable', false),
      onslotchange: null,
    });
    setElementShadow(hostElement, root);
    shadowHosts.add(hostElement);
  };
  const fireFor = (n) => (type) => dispatchTrusted(n, new Event(type));
  const plainInsertion = (child) => !['SCRIPT', 'IMG', 'LINK', 'IFRAME'].includes(child.tagName);
  const insertHostNode = (parent, child, before, node) => {
    if (plainInsertion(child)) {
      const result = host.insertPlain(parent, child.nodeId, before ? before.nodeId : 0);
      if (result === -1)
        throw platformDOMException('Reference node is not a child.', 'NotFoundError');
      if (result === -2)
        throw platformDOMException('Insertion would create a cycle.', 'HierarchyRequestError');
      if (result >= 0) invalidateDOMCollections();
      return result;
    }
    const fire = fireFor(node);
    if (before)
      host.insert(
        parent,
        child,
        before,
        () => fire('load'),
        () => fire('error'),
      );
    else
      host.append(
        parent,
        child,
        () => fire('load'),
        () => fire('error'),
      );
    invalidateDOMCollections();
    return true;
  };
  // Geometry result objects retain the node creation realm after adoption;
  // the numeric projection follows its current browsing document instead.
  let makeElementClientRects, makeDOMRectList;
  const geometryReadHints = [];
  bootstrapRestoreHooks.push(() => {
    geometryReadHints.length = 0;
  });
  const geometryReceiver = (element) =>
    elementWrappers.get(String(elementSlot(element).nodeId)) || element;
  const geometryOperations = {
    rect: (element) => {
      const target = geometryReceiver(element);
      const prior = geometryReadHints.indexOf(target);
      if (prior >= 0) geometryReadHints.splice(prior, 1);
      geometryReadHints.push(target);
      if (geometryReadHints.length > 16) geometryReadHints.shift();
      const value = clientRectFor(target);
      return new DOMRect(value.x, value.y, value.width, value.height);
    },
    scroll: (element, params) => compatibilityScrolling.dispatch(geometryReceiver(element), params),
    rects: (element) => makeElementClientRects(geometryReceiver(element)),
    mediaTimeRanges: (element, kind) => {
      const slot = elementSlot(element);
      const tag = slot.tagName;
      if (tag !== 'VIDEO' && tag !== 'AUDIO') throw new TypeError('Illegal invocation');
      const source = kind === 'seekable' ? host.mediaSourceState(slot.nodeId) : null;
      const ranges =
        source && Number.isFinite(source.duration) && source.duration >= 0
          ? [[0, source.duration]]
          : [];
      return createMediaTimeRanges(ranges);
    },
  };
  const geometryBinding = {
    kind: 'ElementGeometry',
    operations: geometryOperations,
    invoke(receiver, operation, args) {
      const binding = bindingGet(receiver);
      if (!binding || binding.kind !== 'ElementGeometry') throw new TypeError('Illegal invocation');
      return binding.operations[operation](receiver, ...args);
    },
    unpreventable: false,
  };
  const registerElementGeometry = (element) => {
    bindingSet(element, geometryBinding);
  };
  class Element extends Node {
    constructor(token, data) {
      super(token);
      elementData.set(this, data);
      registerElementGeometry(this);
    }
    get tagName() {
      return elementSlot(this).tagName;
    }
    get nodeName() {
      return this.tagName;
    }
    get localName() {
      return this.tagName.toLowerCase();
    }
    get namespaceURI() {
      return elementSlot(this).namespaceURI || null;
    }
    get id() {
      return intrinsicGetAttribute.call(this, 'id') || '';
    }
    set id(v) {
      intrinsicSetAttribute.call(this, 'id', String(v));
    }
    get className() {
      return intrinsicGetAttribute.call(this, 'class') || '';
    }
    set className(v) {
      intrinsicSetAttribute.call(this, 'class', String(v));
    }
    get role() {
      return intrinsicGetAttribute.call(this, 'role');
    }
    set role(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'role');
      else intrinsicSetAttribute.call(this, 'role', String(v));
    }
    get ariaLabel() {
      return intrinsicGetAttribute.call(this, 'aria-label');
    }
    set ariaLabel(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'aria-label');
      else intrinsicSetAttribute.call(this, 'aria-label', String(v));
    }
    get classList() {
      let list = elementClassLists.get(this);
      if (!list) {
        list = new DOMTokenList(this, 'class');
        elementClassLists.set(this, list);
      }
      return list;
    }
    get shadowRoot() {
      const root = elementShadows.get(this);
      return root && root.mode === 'open' ? root : null;
    }
    attachShadow(init) {
      if (!init || !['open', 'closed'].includes(String(init.mode)))
        throw new TypeError(
          "Failed to execute 'attachShadow' on 'Element': Failed to read the 'mode' property from 'ShadowRootInit'",
        );
      if (elementShadows.has(this))
        throw platformDOMException(
          'Shadow root cannot be created on a host which already hosts a shadow tree.',
          'NotSupportedError',
        );
      const root = new ShadowRoot(hostToken, this, String(init.mode), init);
      setElementShadow(this, root);
      return root;
    }
    get textContent() {
      return canonicalTextContent(elementSlot(this).nodeId);
    }
    set textContent(v) {
      setCanonicalTextContent(elementSlot(this).nodeId, v == null ? '' : String(v));
      invalidateDOMCollections();
    }
    get innerHTML() {
      return host.innerHTML(elementSlot(this).nodeId);
    }
    set innerHTML(v) {
      const slot = elementSlot(this);
      if (!slot) throw new TypeError('Illegal invocation');
      host.setInnerHTML(
        slot.nodeId,
        trustedConvert(
          v === null ? '' : v,
          'TrustedHTML',
          'Element innerHTML',
          "Failed to set the 'innerHTML' property on 'Element': ",
          this,
        ),
      );
      invalidateDOMCollections();
    }
    get outerHTML() {
      return host.outerHTML(elementSlot(this).nodeId);
    }
    set outerHTML(v) {
      const slot = elementSlot(this);
      if (!slot) throw new TypeError('Illegal invocation');
      const name = host.setOuterHTML(
        slot.nodeId,
        trustedConvert(
          v === null ? '' : v,
          'TrustedHTML',
          'Element outerHTML',
          "Failed to set the 'outerHTML' property on 'Element': ",
          this,
        ),
      );
      if (name) throw platformDOMException('Cannot replace this element.', name);
      invalidateDOMCollections();
    }
    insertAdjacentHTML(position, text) {
      const id = elementSlot(this).nodeId;
      if (arguments.length < 2) throw new TypeError('Not enough arguments');
      position = bindingString(position).toLowerCase();
      text = trustedConvert(
        text,
        'TrustedHTML',
        'Element insertAdjacentHTML',
        "Failed to execute 'insertAdjacentHTML' on 'Element': ",
        this,
      );
      const name = host.insertAdjacentHTML(id, position, text);
      if (name) throw platformDOMException('Cannot insert adjacent HTML.', name);
      invalidateDOMCollections();
    }
    get parentNode() {
      return syntheticParents.get(this) || cachedDOMParent(this);
    }
    get parentElement() {
      const parent = this.parentNode;
      return parent instanceof Element ? parent : null;
    }
    get firstElementChild() {
      return wrap(host.firstElementChild(elementSlot(this).nodeId));
    }
    get nextSibling() {
      return wrap(host.sibling(elementSlot(this).nodeId, 1));
    }
    get previousSibling() {
      return wrap(host.sibling(elementSlot(this).nodeId, -1));
    }
    get children() {
      let revision = -1,
        values = [];
      return cachedHTMLCollection(this, 'children', '', () => {
        const current = styleReadCache?.version || domCollectionVersion();
        if (revision !== current) {
          values = host.elementChildren(elementSlot(this).nodeId);
          revision = current;
        }
        return values;
      });
    }
    get childElementCount() {
      return this.children.length;
    }
    getAttribute(n) {
      return cachedDOMAttribute(this, String(n));
    }
    hasAttribute(n) {
      return intrinsicGetAttribute.call(this, String(n)) !== null;
    }
    hasAttributes() {
      return cachedDOMAttributeNames(this).length !== 0;
    }
    getAttributeNames() {
      return cachedDOMAttributeNames(this);
    }
    setAttribute(n, v) {
      const inputName = String(n);
      n =
        this.namespaceURI === 'http://www.w3.org/1999/xhtml' ? inputName.toLowerCase() : inputName;
      v = trustedAttributeValue(this, n, v, '', "Failed to execute 'setAttribute' on 'Element': ");
      validateDOMName(inputName, true, 'setAttribute', 'Element');
      elementSlot(this).attributes[n] = v;
      host.setAttribute(elementSlot(this).nodeId, n, v);
      // Synthetic shadow descendants are intentionally detached in the native
      // arena, so their connected-document observation revision cannot advance
      // from the host mutation alone. The shadow projection is observable from
      // its connected host and must invalidate the realm style epoch.
      if (containingShadowRoot(this)) host.invalidateStyleObservations();
      retainGeometryAcrossAttributeMutation(this, n);
    }
    removeAttribute(n) {
      n =
        this.namespaceURI === 'http://www.w3.org/1999/xhtml' ? String(n).toLowerCase() : String(n);
      delete elementSlot(this).attributes[n];
      host.removeAttribute(elementSlot(this).nodeId, n);
      if (containingShadowRoot(this)) host.invalidateStyleObservations();
      retainGeometryAcrossAttributeMutation(this, n);
    }
    toggleAttribute(n, force) {
      n = String(n);
      const present = intrinsicHasAttribute.call(this, n);
      if (force === true || (!present && force !== false)) {
        intrinsicSetAttribute.call(this, n, '');
        return true;
      }
      if (present) intrinsicRemoveAttribute.call(this, n);
      return false;
    }
    querySelector(s) {
      return wrap(host.queryWithin(elementSlot(this).nodeId, String(s)));
    }
    querySelectorAll(s) {
      return nodeList(host.queryAllWithin(elementSlot(this).nodeId, String(s)));
    }
    appendChild(n) {
      insertHostNode(elementSlot(this).nodeId, elementSlot(n), null, n);
      return n;
    }
    insertBefore(n, before) {
      insertHostNode(
        elementSlot(this).nodeId,
        elementSlot(n),
        before ? elementSlot(before) : null,
        n,
      );
      return n;
    }
    removeChild(n) {
      host.removeNode(elementSlot(this).nodeId, elementSlot(n).nodeId);
      return n;
    }
    remove() {
      const p = this.parentNode;
      if (p) p.removeChild(this);
    }
  }
  // Internal reflection invokes the captured platform operations. Page code may
  // override public attribute methods without changing native IDL accessors.
  const intrinsicGetAttribute = Element.prototype.getAttribute;
  let intrinsicSetAttribute = Element.prototype.setAttribute;
  const intrinsicRemoveAttribute = Element.prototype.removeAttribute;
  const intrinsicHasAttribute = Element.prototype.hasAttribute;
  Object.defineProperty(Element.prototype, 'previousElementSibling', {
    get: function () {
      for (let node = this.previousSibling; node; node = node.previousSibling)
        if (node instanceof Element) return node;
      return null;
    },
    enumerable: true,
    configurable: true,
  });
  const namedAttributes = (element) => attributeCompatibility.namedMap(element);
  Object.defineProperty(Element.prototype, 'attributes', {
    get: function () {
      return namedAttributes(this);
    },
    enumerable: true,
    configurable: true,
  });
  for (const name of ['appendChild', 'insertBefore', 'removeChild', 'parentNode', 'parentElement'])
    delete Element.prototype[name];
  for (const name of ['appendChild', 'removeChild']) delete DocumentFragment.prototype[name];
  const canonicalDOMParent = (node) => {
    if (syntheticParents.has(node)) return syntheticParents.get(node);
    return elementSlot(node) ? cachedDOMParent(node) : null;
  };
  const canonicalDOMChildren = (node) => {
    const state = fragmentSlots.get(node);
    if (state) return state.children.slice();
    if (node === document) return host.nodeChildren(realmDocumentRootID).map(wrap);
    return elementSlot(node) ? cachedDOMChildren(node) : [];
  };
  def(Node.prototype, 'parentNode', {
    get() {
      return canonicalDOMParent(this);
    },
  });
  def(Node.prototype, 'parentElement', {
    get() {
      const parent = canonicalDOMParent(this);
      return parent instanceof Element ? parent : null;
    },
  });
  // DOM algorithms use canonical tree state even when author code shadows
  // public accessors. Host connectivity alone cannot see synthetic shadow roots.
  const isNodeConnected = (node) => {
    while (node) {
      const parent = syntheticParents.get(node);
      if (parent) {
        node = parent;
        continue;
      }
      const shadow = shadowSlots.get(node);
      if (shadow) {
        node = shadow.host;
        continue;
      }
      const slot = elementSlot(node);
      if (!slot) return node instanceof Document;
      const record = domReadRecord(node);
      let connected = record?.connected;
      if (connected === undefined) {
        connected = record?.observationState?.connected || host.isConnected(slot.nodeId);
        if (record) record.connected = connected;
      }
      if (connected) return true;
      if (!shadowHosts.size) return false;
      node = cachedDOMParent(node);
    }
    return false;
  };
  const runSyntheticInsertionSteps = (node) => {
    if (node instanceof HTMLIFrameElement && isNodeConnected(node))
      host.iframeWindow(elementSlot(node).nodeId, true);
    if (!(node instanceof Element)) return;
    if (!shadowHosts.size) {
      for (const frame of compatibilitySelectors.query(node, 'iframe'))
        if (isNodeConnected(frame)) host.iframeWindow(elementSlot(frame).nodeId, true);
      return;
    }
    for (const child of cachedDOMChildren(node)) runSyntheticInsertionSteps(child);
    const shadow = elementShadows.get(node),
      state = shadow && fragmentState(shadow);
    if (state) for (const child of state.children) runSyntheticInsertionSteps(child);
  };
  def(Node.prototype, 'appendChild', {
    value: function (node) {
      registerPolyfilledShadowRoot(this);
      if (fragmentSlots.has(this)) {
        if (!isDOMNode(node))
          throw new TypeError(
            "Failed to execute 'appendChild' on 'Node': parameter 1 is not of type 'Node'.",
          );
        const state = fragmentState(this),
          old = syntheticParents.get(node);
        if (old && fragmentSlots.has(old)) {
          const oldState = fragmentState(old),
            index = oldState.children.indexOf(node);
          if (index >= 0) oldState.children.splice(index, 1);
        }
        state.children.push(node);
        state.html = '';
        setSyntheticParent(node, this);
        invalidateDOMCollections();
        if (this instanceof ShadowRoot) runSyntheticInsertionSteps(node);
        return node;
      }
      const target = elementSlot(this),
        child = elementSlot(node);
      if (!target || !child)
        throw platformDOMException(
          'The operation is not supported for this node.',
          'HierarchyRequestError',
        );
      if (insertHostNode(target.nodeId, child, null, node)) runSyntheticInsertionSteps(node);
      return node;
    },
    writable: true,
  });
  def(Node.prototype, 'insertBefore', {
    value: function (node, before) {
      registerPolyfilledShadowRoot(this);
      if (fragmentSlots.has(this)) {
        const state = fragmentState(this),
          index = before == null ? state.children.length : state.children.indexOf(before);
        if (index < 0)
          throw platformDOMException(
            'The node before which the new node is to be inserted is not a child of this node.',
            'NotFoundError',
          );
        state.children.splice(index, 0, node);
        setSyntheticParent(node, this);
        invalidateDOMCollections();
        if (this instanceof ShadowRoot) runSyntheticInsertionSteps(node);
        return node;
      }
      const target = elementSlot(this),
        child = elementSlot(node);
      if (!target || !child)
        throw platformDOMException(
          'The operation is not supported for this node.',
          'HierarchyRequestError',
        );
      if (insertHostNode(target.nodeId, child, before ? elementSlot(before) : null, node))
        runSyntheticInsertionSteps(node);
      return node;
    },
    writable: true,
  });
  def(Node.prototype, 'removeChild', {
    value: function (node) {
      if (fragmentSlots.has(this)) {
        const state = fragmentState(this),
          index = state.children.indexOf(node);
        if (index < 0)
          throw platformDOMException(
            'The node to be removed is not a child of this node.',
            'NotFoundError',
          );
        state.children.splice(index, 1);
        deleteSyntheticParent(node);
        invalidateDOMCollections();
        return node;
      }
      const target = this === document ? { nodeId: realmDocumentRootID } : elementSlot(this),
        child = elementSlot(node);
      if (!target || !child)
        throw platformDOMException(
          'The operation is not supported for this node.',
          'NotFoundError',
        );
      if (host.prepareNodeRemoval(child.nodeId)) {
        dispatchTrusted(window, new Event('load'));
        host.completeSynchronousLoad();
      }
      host.removeNode(target.nodeId, child.nodeId);
      invalidateDOMCollections();
      return node;
    },
    writable: true,
  });
  def(Element.prototype, 'matches', {
    value: function (selectors) {
      return String(selectors)
        .split(',')
        .some((selector) => cssSelectorMatch(this, selector));
    },
    writable: true,
  });
  def(Element.prototype, 'webkitMatchesSelector', {
    value: Element.prototype.matches,
    writable: true,
  });
  def(Element.prototype, 'closest', {
    value: function (selectors) {
      for (let element = this; element instanceof Element; element = element.parentElement)
        if (element.matches(selectors)) return element;
      return null;
    },
    writable: true,
  });
  def(Node.prototype, 'ownerDocument', {
    get: function () {
      return this instanceof Document ? null : document;
    },
    enumerable: true,
  });
  // A fragment contributes its children, in order, and is empty afterwards.
  // Keep canonical host nodes; only detach the synthetic fragment parent link.
  const appendNode = Node.prototype.appendChild,
    insertNode = Node.prototype.insertBefore,
    removeNode = Node.prototype.removeChild;
  let validateTemplateInsertion = null;
  const prepareInsertion = (parent, node, before, forceValidation = false) => {
    if (!isDOMNode(node)) throw new TypeError('Expected a Node');
    if (
      !['element', 'document'].includes(elementSlot(parent)?.type) &&
      !isDOMFragment(parent) &&
      parent !== document
    )
      throw platformDOMException('Unsupported parent node.', 'HierarchyRequestError');
    // Canonical ordinary insertion validates membership and cycles together
    // with the mutation in Go. Synthetic parents and callback-bearing resource
    // paths still require validation before their separate insertion steps.
    const slot = elementSlot(node),
      combined =
        !forceValidation &&
        !shadowHosts.size &&
        elementSlot(parent) &&
        slot &&
        (before == null || elementSlot(before)) &&
        !isDOMFragment(node) &&
        plainInsertion(slot);
    if (
      before != null &&
      (!combined || node === before) &&
      (elementSlot(before) && elementSlot(parent)
        ? host.parentNode(elementSlot(before).nodeId) !== elementSlot(parent).nodeId
        : !fragmentState(parent).children.includes(before))
    )
      throw platformDOMException('Reference node is not a child.', 'NotFoundError');
    if (!combined && (node === parent || node.contains(parent)))
      throw platformDOMException('Insertion would create a cycle.', 'HierarchyRequestError');
    if (validateTemplateInsertion && !combined) validateTemplateInsertion(parent, node);
  };
  const detachForInsertion = (parent, node) => {
    const old =
      syntheticParents.get(node) || (fragmentSlots.has(parent) ? canonicalDOMParent(node) : null);
    if (old) old.removeChild(node);
  };
  // Drain fragment membership once. Repeated removal from the front shifts
  // the remaining array for every child, making one insertion quadratic.
  const takeFragmentChildren = (node) => {
    const slot = elementSlot(node);
    if (slot && slot.type === 'fragment') {
      const ids = host.drainFragment(slot.nodeId);
      if (ids !== null) return ids.map(wrap);
      const children = Array.from(node.childNodes);
      for (const child of children) removeNode.call(node, child);
      return children;
    }
    const state = fragmentState(node),
      children = state.children;
    state.children = [];
    state.html = '';
    for (const child of children) deleteSyntheticParent(child);
    return children;
  };
  const insertPlainFragment = (parent, node, before) => {
    const target = elementSlot(parent),
      fragment = elementSlot(node);
    if (!target || fragment?.type !== 'fragment' || shadowHosts.size) return false;
    const result = host.insertPlainFragment(
      target.nodeId,
      fragment.nodeId,
      before ? elementSlot(before).nodeId : 0,
    );
    if (result === -1)
      throw platformDOMException('Reference node is not a child.', 'NotFoundError');
    if (result === -2)
      throw platformDOMException('Insertion would create a cycle.', 'HierarchyRequestError');
    if (result >= 0) invalidateDOMCollections();
    return result;
  };
  def(Node.prototype, 'appendChild', {
    value: function (node) {
      prepareInsertion(this, node, null);
      if (isDOMFragment(node)) {
        if (!insertPlainFragment(this, node, null))
          for (const child of takeFragmentChildren(node)) this.appendChild(child);
        return node;
      }
      detachForInsertion(this, node);
      return appendNode.call(this, node);
    },
    writable: true,
  });
  def(Node.prototype, 'insertBefore', {
    value: function (node, before) {
      prepareInsertion(this, node, before);
      if (node === before) return node;
      if (isDOMFragment(node)) {
        if (!insertPlainFragment(this, node, before))
          for (const child of takeFragmentChildren(node)) this.insertBefore(child, before);
        return node;
      }
      detachForInsertion(this, node);
      return insertNode.call(this, node, before);
    },
    writable: true,
  });
  const styleCache = new WeakMap();
  const domRectSlots = new WeakMap();
  class DOMRectReadOnly {
    constructor(x = 0, y = 0, width = 0, height = 0) {
      const left = Number(x),
        top = Number(y),
        w = Number(width),
        h = Number(height);
      domRectSlots.set(this, {
        x: left,
        y: top,
        width: w,
        height: h,
        left: Math.min(left, left + w),
        right: Math.max(left, left + w),
        top: Math.min(top, top + h),
        bottom: Math.max(top, top + h),
      });
    }
    get x() {
      return domRectSlots.get(this).x;
    }
    get y() {
      return domRectSlots.get(this).y;
    }
    get width() {
      return domRectSlots.get(this).width;
    }
    get height() {
      return domRectSlots.get(this).height;
    }
    get top() {
      return domRectSlots.get(this).top;
    }
    get right() {
      return domRectSlots.get(this).right;
    }
    get bottom() {
      return domRectSlots.get(this).bottom;
    }
    get left() {
      return domRectSlots.get(this).left;
    }
    toJSON() {
      return { ...domRectSlots.get(this) };
    }
    static fromRect(other = {}) {
      return new DOMRectReadOnly(other.x || 0, other.y || 0, other.width || 0, other.height || 0);
    }
  }
  class DOMRect extends DOMRectReadOnly {
    constructor(x = 0, y = 0, width = 0, height = 0) {
      super(x, y, width, height);
    }
    get x() {
      return domRectSlots.get(this).x;
    }
    set x(value) {
      const state = domRectSlots.get(this);
      state.x = Number(value);
      state.left = Math.min(state.x, state.x + state.width);
      state.right = Math.max(state.x, state.x + state.width);
    }
    get y() {
      return domRectSlots.get(this).y;
    }
    set y(value) {
      const state = domRectSlots.get(this);
      state.y = Number(value);
      state.top = Math.min(state.y, state.y + state.height);
      state.bottom = Math.max(state.y, state.y + state.height);
    }
    get width() {
      return domRectSlots.get(this).width;
    }
    set width(value) {
      const state = domRectSlots.get(this);
      state.width = Number(value);
      state.left = Math.min(state.x, state.x + state.width);
      state.right = Math.max(state.x, state.x + state.width);
    }
    get height() {
      return domRectSlots.get(this).height;
    }
    set height(value) {
      const state = domRectSlots.get(this);
      state.height = Number(value);
      state.top = Math.min(state.y, state.y + state.height);
      state.bottom = Math.max(state.y, state.y + state.height);
    }
    static fromRect(other = {}) {
      return new DOMRect(other.x || 0, other.y || 0, other.width || 0, other.height || 0);
    }
  }
  /* shared_dom_matrix */
  const layoutPositionFor = (element) =>
      String(
        computedCSSDeclarations(element).find((entry) => entry.name === 'position')?.value ||
          'static',
      ).toLowerCase(),
    participatesInFlow = (element) => !['absolute', 'fixed'].includes(layoutPositionFor(element));
  const geometryParent = (element) => {
    const cache =
      styleReadCache && (styleReadCache.parents || (styleReadCache.parents = new WeakMap()));
    if (cache?.has(element)) return cache.get(element);
    const parent = cssObservationParent(element),
      value =
        elementSlot(parent)?.type === 'element' ? parent : shadowSlots.get(parent)?.host || null;
    cache?.set(element, value);
    return value;
  };
  // Resolve variables at their defining element, including variables nested
  // in CSS math, so inherited values do not accidentally use a descendant's
  // custom properties.
  const geometryValue = (element, text, seen) => {
    if (!text?.includes('var(')) return text;
    seen = seen || new Set();
    const match =
      /^var\(\s*(--[\w-]+)\s*(?:,([\s\S]*))?\)$/.exec(text) ||
      /var\(\s*(--[\w-]+)\s*(?:,([^()]*))?\)/.exec(text);
    if (!match) return null;
    let replacement = cssCustomPropertyValue(element, match[1], seen);
    replacement ??= geometryValue(element, match[2]?.trim(), seen);
    return replacement == null
      ? null
      : geometryValue(
          element,
          text.slice(0, match.index) + replacement + text.slice(match.index + match[0].length),
          seen,
        );
  };
  const definiteGeometryHeight = (element, seen = new Set()) => {
    if (!element || seen.has(element)) return false;
    seen.add(element);
    const value = geometryValue(
      element,
      computedCSSDeclarations(element).find((entry) => entry.name === 'height')?.value,
    );
    if (value === '0' || /^(?:\d+(?:\.\d+)?|\.\d+)px$/.test(value || '')) return true;
    return value?.endsWith('%')
      ? element === document.documentElement ||
          definiteGeometryHeight(geometryParent(element), seen)
      : false;
  };
  const geometryPixels = (text) =>
    text && /^[+-]?(?:\d+(?:\.\d+)?|\.\d+)px$/.test(text)
      ? Number.parseFloat(text)
      : text === '0'
        ? 0
        : null;
  const replacedGeometryTags = new Set([
    'IMG',
    'INPUT',
    'TEXTAREA',
    'SELECT',
    'BUTTON',
    'VIDEO',
    'AUDIO',
    'CANVAS',
    'IFRAME',
    'OBJECT',
    'EMBED',
  ]);
  /* shared_css_box_geometry */
  const layoutWidthFor = (element) => cssBoxModel.width(element);
  const layoutWidthBoxFor = (element) =>
    withStyleReadCache(
      () =>
        blitzLayoutRect(element) ??
        foreignCSSObservation(element, 'layout') ??
        cssBoxModel.widthBox(element),
    );
  const layoutHeightBoxFor = (element) =>
    withStyleReadCache(
      () =>
        blitzLayoutRect(element) ??
        foreignCSSObservation(element, 'layout') ??
        cssBoxModel.heightBox(element),
    );
  const layoutRectInObservation = (element) =>
    blitzLayoutRect(element) ??
    foreignCSSObservation(element, 'layout') ??
    cssBoxModel.rect(element);
  const layoutRectFor = (element) => withStyleReadCache(() => layoutRectInObservation(element));
  const makeDOMRect = (value, element) =>
    element
      ? callRealmBinding(element, requireRealmBinding(element, 'ElementGeometry'), 'rect', [])
      : new DOMRect(value.x, value.y, value.width, value.height);
  // Internal batch consumers already own an observation. Re-entering the host
  // epoch boundary for every candidate is unnecessary: these helpers read
  // private canonical state and do not invoke author conversion callbacks.
  let compatibilityScrolling = null;
  const uncachedClientRectInObservation = (element) => {
    const foreign = blitzPackedRecord(element) ? null : foreignCSSObservation(element, 'rect');
    if (foreign !== null) return foreign;
    const box = layoutRectInObservation(element),
      keywords = { left: '0%', top: '0%', center: '50%', right: '100%', bottom: '100%' };
    let points = [
        [box.x, box.y],
        [box.x + box.width, box.y],
        [box.x, box.y + box.height],
        [box.x + box.width, box.y + box.height],
      ],
      transformed = false;
    for (let node = element; node; node = geometryParent(node)) {
      const nativeTransform = blitzStyleValue(node, 'transform');
      const entries =
          nativeTransform === null
            ? computedCSSDeclarations(node)
            : ['transform', 'transform-origin', 'display', 'perspective'].map((name) => ({
                name,
                value: blitzStyleValue(node, name),
              })),
        get = (k) => entries.find((e) => e.name === k)?.value;
      const raw = entries.find((e) => e.name === 'transform')?.parsedValue ?? get('transform');
      if (!raw || raw === 'none' || get('display') === 'none') continue;
      let nativeProduct = null;
      if (nativeTransform !== null) {
        const record = blitzPackedRecord(node),
          key = record.at;
        if (record.packed.transforms.has(key)) nativeProduct = record.packed.transforms.get(key);
        else {
          nativeProduct = host.blitzObserve(elementSlot(node).nodeId, 'transform', '');
          if (record.packed.transforms.size < 4096)
            record.packed.transforms.set(key, nativeProduct);
        }
      }
      const bounds = layoutRectInObservation(node),
        len = (v, size) => {
          const resolved = cssResolveLength(v, cssGeometryLengthContext(node, size));
          return resolved === null ? null : cssGeometryLength(resolved);
        };
      let matrix;
      try {
        matrix = nativeProduct
          ? nativeProduct.slice(0, 16)
          : compatibilityMatrix.parse(raw, (value, axis) =>
              len(value, axis === 0 ? bounds.width : axis === 1 ? bounds.height : 0),
            );
      } catch {
        host.semanticMissingAt('surface.js/clientRectFor', 'CSS.clientRectTransform', raw);
        continue;
      }
      const origin = String(get('transform-origin') || '50% 50%').split(/\s+/),
        ox = nativeProduct
          ? nativeProduct[16]
          : len(keywords[origin[0]] || origin[0], bounds.width),
        oy = nativeProduct
          ? nativeProduct[17]
          : len(keywords[origin[1]] || origin[1] || '50%', bounds.height);
      if (ox === null || oy === null) {
        host.semanticMissingAt('surface.js/clientRectFor', 'CSS.clientRectTransformOrigin');
        continue;
      }
      const oz = nativeProduct ? nativeProduct[18] : origin[2] ? len(origin[2], 0) : 0;
      if (oz === null) {
        host.semanticMissingAt('surface.js/clientRectFor', 'CSS.clientRectTransformOrigin');
        continue;
      }
      const cx = bounds.x + ox,
        cy = bounds.y + oy,
        projected = points.map(([x, y]) => compatibilityMatrix.point(matrix, x - cx, y - cy, -oz));
      if (projected.some((point) => point[3] <= 0)) {
        host.semanticMissingAt('surface.js/clientRectFor', 'CSS.clientRectPerspectiveClipping');
        continue;
      }
      points = projected.map((point) => [
        compatibilityMatrix.geometryCoordinate(point[0] / point[3] + cx),
        compatibilityMatrix.geometryCoordinate(point[1] / point[3] + cy),
      ]);
      transformed = true;
    }
    const offset = compatibilityScrolling?.offset(element) || { x: 0, y: 0 };
    if (!transformed && !offset.x && !offset.y) return box;
    if (!transformed)
      return {
        ...box,
        x: box.x - offset.x,
        y: box.y - offset.y,
        left: box.x - offset.x,
        top: box.y - offset.y,
        right: box.right - offset.x,
        bottom: box.bottom - offset.y,
      };
    const xs = points.map((p) => p[0]),
      ys = points.map((p) => p[1]),
      x = Math.min(...xs),
      y = Math.min(...ys);
    const width = Math.fround(Math.max(...xs) - x),
      height = Math.fround(Math.max(...ys) - y);
    return {
      x: x - offset.x,
      y: y - offset.y,
      left: x - offset.x,
      top: y - offset.y,
      right: x - offset.x + width,
      bottom: y - offset.y + height,
      width,
      height,
    };
  };
  const clientRectInObservation = (element) => {
    const cache =
      styleReadCache &&
      (styleReadCache.clientRects || (styleReadCache.clientRects = new WeakMap()));
    const scrollRevision = compatibilityScrolling?.revision() || 0,
      cached = cache?.get(element);
    if (cached?.scrollRevision === scrollRevision) return cached.value;
    const value = uncachedClientRectInObservation(element);
    // This projection includes transforms and every current scroll offset.
    // scrolling.js ends the observation after an effective scroll, while DOM,
    // style, viewport, and pseudo-state changes advance its canonical epoch.
    cache?.set(element, { scrollRevision, value });
    return value;
  };
  const clientRectFor = (element) => withStyleReadCache(() => clientRectInObservation(element));
  const frameViewportSizes = new WeakMap();
  const readFrameViewport = (nodeID) =>
    withStyleReadCache(() => {
      const element = wrap(nodeID);
      if (!element) return [0, 0];
      if (!isNodeConnected(element)) return [0, 0];
      for (let n = element; n instanceof Element; n = geometryParent(n))
        if (
          computedCSSDeclarations(n).some((e) => e.name === 'display' && e.value === 'none') ||
          host.getAttribute(elementSlot(n).nodeId, 'hidden') !== null
        )
          return frameViewportSizes.get(element) || [0, 0];
      const entries = computedCSSDeclarations(element),
        get = (name) => geometryValue(element, entries.find((e) => e.name === name)?.value);
      const parent = geometryParent(element);
      const dimension = (axis, fallback) => {
        const text = get(axis),
          attribute = host.getAttribute(elementSlot(element).nodeId, axis);
        let size = geometryPixels(text);
        if (text?.endsWith('%'))
          size =
            (Number.parseFloat(text) *
              (axis === 'width'
                ? parent
                  ? layoutWidthFor(parent)
                  : host.viewport().width
                : parent
                  ? layoutRectFor(parent).height
                  : host.viewport().height)) /
            100;
        if (size === null)
          size = attribute && /^\d+(?:\.\d+)?$/.test(attribute) ? Number(attribute) : fallback;
        if (get('box-sizing') === 'border-box')
          for (const side of axis === 'width' ? ['left', 'right'] : ['top', 'bottom'])
            size -=
              (geometryPixels(get('padding-' + side)) || 0) +
              (geometryPixels(get('border-' + side + '-width')) ?? 2);
        return Math.max(0, Math.round(size));
      };
      const size = [dimension('width', 300), dimension('height', 150)];
      frameViewportSizes.set(element, size);
      return size;
    });
  const frameHasLayout = (nodeID) =>
    withStyleReadCache(() => {
      const cache = styleReadCache.frameLayout || (styleReadCache.frameLayout = new WeakMap());
      const visited = [];
      let connected = false;
      for (let n = wrap(nodeID); elementSlot(n)?.type === 'element'; n = geometryParent(n)) {
        if (cache.has(n)) {
          connected = cache.get(n);
          break;
        }
        visited.push(n);
        // Membership in the validated native projection proves connectivity.
        // Fallback Documents still read the canonical DOM host directly.
        if (blitzPackedRecord(n) || host.isConnected(elementSlot(n).nodeId)) connected = true;
        if (cssComputedValue(n, 'display') === 'none') {
          connected = false;
          break;
        }
      }
      for (const element of visited) cache.set(element, connected);
      return connected;
    });
  registerBootstrapCallback('installFrameViewport', readFrameViewport, frameHasLayout);
  const pseudoContentRules = new WeakMap();
  registerBootstrapCallback('installComputedStyleFlatTree', (nodeID, kind, name) => {
    const element = wrap(nodeID);
    if (kind === 'protocolComputedStyle')
      return withStyleReadCache(() => {
        let target = element;
        if (elementSlot(target)?.type !== 'element') {
          if (elementSlot(target)?.type === 'fragment')
            throw new Error('Node is not an element and does not have a parent element');
          target = cssFontParent(target);
        }
        if (elementSlot(target)?.type !== 'element')
          throw new Error('Node is not an element and does not have a parent element');
        const names = new Set(cssComputedNames);
        for (
          let ancestor = target;
          elementSlot(ancestor)?.type === 'element';
          ancestor = cssFontParent(ancestor)
        )
          for (const entry of computedCSSDeclarations(ancestor))
            if (entry.name.startsWith('--')) names.add(entry.name);
        const appearance = cssComputedValue(target, 'appearance');
        return JSON.stringify({
          computedStyle: Array.from(names, (property) => ({
            name: property,
            value: cssComputedValue(target, property),
          })),
          extraFields: { isAppearanceBase: appearance === 'base' || appearance === 'base-select' },
        });
      });
    if (kind === 'imageResources')
      return withStyleReadCache(() => {
        const sources = constructedStyleSheets.resourceSources(document);
        if (
          !sources.some((source) => /url\s*\(/i.test(source)) &&
          !host.blitzInlineStyles().some((source) => /url\s*\(/i.test(source))
        )
          return '[]';
        const urls = new Set();
        for (const candidate of compatibilitySelectors.query(document, '*')) {
          const value = cssComputedValue(candidate, 'background-image');
          if (!/url\s*\(/i.test(value)) continue;
          // Most elements have no image. Only actual resource candidates need
          // the ancestor visibility walk; style resolution is side-effect free.
          if (!frameHasLayout(elementSlot(candidate).nodeId)) continue;
          const ast = mimicSelectorLibrary.parseStylesheet(value, { context: 'value' });
          const visit = (node) => {
            if (node.type === 'Url') urls.add(node.value);
            if (node.children) node.children.forEach(visit);
          };
          visit(ast);
        }
        return JSON.stringify(Array.from(urls));
      });
    return kind === 'values'
      ? withStyleReadCache(() => {
          const values = {};
          for (const property of JSON.parse(name))
            values[property] = cssComputedValue(element, property);
          return JSON.stringify(values);
        })
      : kind === 'pseudoContent'
        ? withStyleReadCache(() => {
            // Without shadow distribution every connected document element
            // participates in the flat tree. A missing pseudo content rule
            // computes to normal, so only possible content overrides need a
            // cascade. Shadow documents use canonical scalar observations.
            if (shadowHosts.size) return false;
            const rules = styleSheetRules(document.documentElement);
            let candidates = pseudoContentRules.get(rules);
            if (!candidates) {
              candidates = rules.filter(
                (rule) =>
                  rule.pseudo &&
                  rule
                    .declarations()
                    .some((entry) => entry.name === 'content' || entry.name === 'all'),
              );
              pseudoContentRules.set(rules, candidates);
            }
            const active = host.documentActive();
            const rows = [];
            for (const candidate of compatibilitySelectors.query(document, '*')) {
              if (rows.length === 32768) return false;
              const values = ['before', 'after'].map((pseudo) =>
                !active
                  ? ''
                  : compatibilitySelectors.matchingStyles(candidate, candidates, pseudo, false)
                        .length
                    ? cssComputedValue(candidate, 'content', pseudo)
                    : 'normal',
              );
              rows.push([elementSlot(candidate).nodeId, ...values]);
            }
            const encoded = JSON.stringify(rows);
            return encoded.length <= 4 * 1024 * 1024 ? encoded : false;
          })
        : kind === 'pseudoValue'
          ? (() => {
              const request = JSON.parse(name);
              return cssComputedValue(element, request.name, request.pseudo);
            })()
          : kind === 'documentValues'
            ? withStyleReadCache(() => {
                const properties = JSON.parse(name),
                  rows = [];
                for (const candidate of compatibilitySelectors.query(document, '*')) {
                  const values = {};
                  for (const property of properties)
                    values[property] = cssComputedValue(candidate, property);
                  rows.push([
                    elementSlot(candidate).nodeId,
                    values,
                    observeElementVisibility(candidate),
                  ]);
                }
                return JSON.stringify(rows);
              })
            : kind === 'innerText'
              ? renderedInnerText(element)
              : kind === 'scroll'
                ? compatibilityScrolling.dispatch(element, JSON.parse(name))
                : kind === 'visibility'
                  ? observeElementVisibility(element, JSON.parse(name))
                  : kind === 'box'
                    ? cssBoxModel.hasBox(element)
                    : kind === 'value'
                      ? cssComputedValue(element, name)
                      : kind === 'rect'
                        ? clientRectFor(element)
                        : kind === 'layout'
                          ? layoutRectFor(element)
                          : kind === 'document'
                            ? computedStyleDocumentAvailable(element)
                            : computedStyleAvailable(element);
  });
  registerBootstrapCallback('installProtocolBoxModel', (nodeID) =>
    withStyleReadCache(() => {
      const element = wrap(nodeID),
        prior = geometryReadHints.indexOf(element);
      if (prior >= 0) geometryReadHints.splice(prior, 1);
      geometryReadHints.push(element);
      if (geometryReadHints.length > 16) geometryReadHints.shift();
      const r = clientRectFor(element),
        n = (name) => Number.parseFloat(cssComputedValue(element, name)) || 0,
        q = (left, top, right, bottom) => [left, top, right, top, right, bottom, left, bottom];
      if (!host.isConnected(nodeID) || cssComputedValue(element, 'display') === 'none')
        throw new Error('Could not compute box model');
      if (!r.width || !r.height) throw new Error('Could not compute box model');
      const padding = q(
        r.left + n('border-left-width'),
        r.top + n('border-top-width'),
        r.right - n('border-right-width'),
        r.bottom - n('border-bottom-width'),
      );
      return {
        border: q(r.left, r.top, r.right, r.bottom),
        padding,
        content: q(
          padding[0] + n('padding-left'),
          padding[1] + n('padding-top'),
          padding[2] - n('padding-right'),
          padding[5] - n('padding-bottom'),
        ),
        margin: q(
          r.left - n('margin-left'),
          r.top - n('margin-top'),
          r.right + n('margin-right'),
          r.bottom + n('margin-bottom'),
        ),
        width: r.width,
        height: r.height,
      };
    }),
  );
  registerBootstrapCallback('installProtocolScroll', (nodeID, payload) =>
    compatibilityScrolling.dispatch(wrap(nodeID), JSON.parse(payload)),
  );
  let constructCustomElement = null,
    customElementCloneInert = 0;
  let templateTreeIsInert = () => false;
  const viewportClientElement = (element) =>
    (element === document.documentElement && document.compatMode !== 'BackCompat') ||
    (element === document.body && document.compatMode === 'BackCompat');
  const innerTextExcludedTags = new Set(['SCRIPT', 'STYLE', 'TEMPLATE', 'NOSCRIPT']);
  const innerTextBlockDisplays = new Set([
    'block',
    'flex',
    'grid',
    'list-item',
    'table',
    'table-caption',
    'table-row',
  ]);
  function renderedInnerText(element) {
    return withStyleReadCache(() => {
      const foreign = foreignCSSObservation(element, 'innerText');
      if (foreign !== null) return foreign;
      return renderedInnerTextInObservation(element);
    });
  }
  function renderedInnerTextInObservation(element) {
    // Chrome falls back to textContent for detached/inert subtrees. For rendered
    // trees innerText observes CSS visibility and generated line boundaries.
    if (!element.isConnected) return element.textContent;
    const chunks = [];
    // Adjacent required line breaks combine by maximum, rather than adding
    // lines for every enclosing block (for example a list and its list item).
    const newline = (count = 1) => chunks.push(count);
    const visit = (node) => {
      if (node.nodeType === 3) {
        // Source line breaks collapse to spaces in normal inline text. Keep
        // them separate from the rendered line boundaries inserted below.
        const whiteSpace = cssComputedValue(node.parentElement, 'white-space');
        chunks.push(
          whiteSpace === 'pre' || whiteSpace === 'pre-wrap' || whiteSpace === 'break-spaces'
            ? node.data
            : whiteSpace === 'pre-line'
              ? node.data.replace(/[\t\f\r ]+/g, ' ')
              : node.data.replace(/[\t\n\f\r ]+/g, ' '),
        );
        return;
      }

      if (node.nodeType !== 1 || innerTextExcludedTags.has(node.tagName)) return;

      const display = cssComputedValue(node, 'display');
      if (display === 'none') return;

      const visibility = cssComputedValue(node, 'visibility');
      if (visibility === 'hidden' || visibility === 'collapse') return;

      if (node.tagName === 'BR') {
        chunks.push('\n');
        return;
      }

      const block = innerTextBlockDisplays.has(display);
      const paragraph = block && node.tagName === 'P';

      if (block) {
        newline(paragraph ? 2 : 1);
      }

      for (const child of node.childNodes) visit(child);

      if (block) {
        newline(paragraph ? 2 : 1);
      }
    };
    visit(element);
    let text = '',
      pendingBreaks = 0;
    for (const chunk of chunks) {
      if (typeof chunk === 'number') {
        pendingBreaks = Math.max(pendingBreaks, chunk);
      } else if (pendingBreaks && !chunk.trim() && !chunk.includes('\n')) {
        continue;
      } else {
        if (pendingBreaks) {
          text = text.replace(/ +$/g, '');
          if (text) text += '\n'.repeat(pendingBreaks);
          pendingBreaks = 0;
        }
        text += chunk;
      }
    }
    return text
      .replace(/[\t\f\r ]+/g, ' ')
      .replace(/ *\n */g, '\n')
      .replace(/\n{3,}/g, '\n\n')
      .replace(/^[ \n]+|[ \n]+$/g, '');
  }
  class HTMLElement extends Element {
    constructor(token, data) {
      if (token === hostToken) {
        super(token, data);
        return;
      }
      if (!constructCustomElement) illegal('HTMLElement');
      return constructCustomElement(new.target);
    }
    get nonce() {
      return host.elementNonce(elementSlot(this).nodeId);
    }
    set nonce(v) {
      host.elementNonce(elementSlot(this).nodeId, bindingString(v));
    }
    get title() {
      return intrinsicGetAttribute.call(this, 'title') || '';
    }
    set title(v) {
      intrinsicSetAttribute.call(this, 'title', String(v));
    }
    get lang() {
      return intrinsicGetAttribute.call(this, 'lang') || '';
    }
    set lang(value) {
      intrinsicSetAttribute.call(this, 'lang', String(value));
    }
    get hidden() {
      const value = intrinsicGetAttribute.call(this, 'hidden');
      return value?.toLowerCase() === 'until-found' ? 'until-found' : value !== null;
    }
    set hidden(v) {
      if (typeof v === 'string' && v.toLowerCase() === 'until-found')
        intrinsicSetAttribute.call(this, 'hidden', 'until-found');
      else this.toggleAttribute('hidden', !!v);
    }
    get innerText() {
      return renderedInnerText(this);
    }
    set innerText(v) {
      this.textContent = v == null ? '' : String(v);
    }
    get ariaLive() {
      return intrinsicGetAttribute.call(this, 'aria-live');
    }
    set ariaLive(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'aria-live');
      else intrinsicSetAttribute.call(this, 'aria-live', String(v));
    }
    get ariaAtomic() {
      return intrinsicGetAttribute.call(this, 'aria-atomic');
    }
    set ariaAtomic(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'aria-atomic');
      else intrinsicSetAttribute.call(this, 'aria-atomic', String(v));
    }
    get style() {
      let style = styleCache.get(this);
      if (!style) {
        style = cssDeclaration(this);
        styleCache.set(this, style);
      }
      return style;
    }
    set style(value) {
      this.style.cssText = value;
    }
    get offsetWidth() {
      return layoutWidthBoxFor(this).width;
    }
    get offsetHeight() {
      return layoutHeightBoxFor(this).height;
    }
    get clientWidth() {
      return viewportClientElement(this)
        ? host.viewport().width
        : Math.round(layoutWidthBoxFor(this).clientWidth ?? 0);
    }
    get clientHeight() {
      return viewportClientElement(this)
        ? host.viewport().height
        : Math.round(layoutHeightBoxFor(this).clientHeight ?? 0);
    }
    get offsetTop() {
      return withStyleReadCache(() => {
        const box = cssBoxModel.nativeOffsets(this) ?? layoutRectFor(this);
        return Math.round(box.offsetTop ?? box.top);
      });
    }
    get offsetLeft() {
      return withStyleReadCache(() => {
        const box = cssBoxModel.nativeOffsets(this) ?? layoutRectFor(this);
        return Math.round(box.offsetLeft ?? box.left);
      });
    }
    get scrollHeight() {
      return Math.max(this.clientHeight, Math.round(layoutRectFor(this).height));
    }
    get scrollWidth() {
      return Math.max(this.clientWidth, Math.round(layoutRectFor(this).width));
    }
    getBoundingClientRect() {
      return makeDOMRect(null, this);
    }
  }
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    get() {
      requireRealmBinding(this, 'ElementGeometry');
      return withStyleReadCache(() => {
        if (
          !cssBoxModel.hasBox(this) ||
          ['HTML', 'BODY'].includes(elementSlot(this).tagName) ||
          cssBoxModel.state(this).position === 'fixed'
        )
          return null;
        for (let parent = geometryParent(this); parent; parent = geometryParent(parent)) {
          if (!cssBoxModel.hasBox(parent)) continue;
          const tag = elementSlot(parent).tagName;
          if (
            cssBoxModel.state(parent).position !== 'static' ||
            ['BODY', 'TABLE', 'TD', 'TH'].includes(tag)
          )
            return parent;
        }
        return null;
      });
    },
    enumerable: true,
    configurable: true,
  });
  class DOMStringMap {
    constructor() {
      throw new TypeError("Failed to construct 'DOMStringMap': Illegal constructor");
    }
  }
  Object.defineProperty(DOMStringMap.prototype, Symbol.toStringTag, {
    value: 'DOMStringMap',
    configurable: true,
  });
  const datasetCache = new WeakMap(),
    datasetName = (name) => String(name).replace(/[A-Z]/g, (c) => '-' + c.toLowerCase()),
    datasetKey = (name) =>
      String(name)
        .slice(5)
        .replace(/-([a-z])/g, (_m, c) => c.toUpperCase());
  Object.defineProperty(HTMLElement.prototype, 'dataset', {
    get: function () {
      let value = datasetCache.get(this);
      if (!value) {
        value = new Proxy(Object.create(DOMStringMap.prototype), {
          // Supported data names shadow inherited properties; missing names
          // still expose the ordinary DOMStringMap/Object prototype chain.
          get: (target, key, receiver) => {
            if (typeof key === 'string') {
              const attribute = intrinsicGetAttribute.call(this, 'data-' + datasetName(key));
              if (attribute !== null) return attribute;
            }
            return Reflect.get(target, key, receiver);
          },
          has: (target, key) =>
            (typeof key === 'string' &&
              intrinsicHasAttribute.call(this, 'data-' + datasetName(key))) ||
            Reflect.has(target, key),
          set: (target, key, next, receiver) => {
            if (typeof key !== 'string') return Reflect.set(target, key, next, receiver);
            intrinsicSetAttribute.call(this, 'data-' + datasetName(key), String(next));
            return true;
          },
          deleteProperty: (target, key) => {
            if (typeof key !== 'string') return Reflect.deleteProperty(target, key);
            intrinsicRemoveAttribute.call(this, 'data-' + datasetName(key));
            return true;
          },
          ownKeys: (target) => [
            ...this.getAttributeNames()
              .filter((name) => name.startsWith('data-'))
              .map(datasetKey),
            ...Reflect.ownKeys(target),
          ],
          getOwnPropertyDescriptor: (target, key) =>
            typeof key === 'string' && intrinsicHasAttribute.call(this, 'data-' + datasetName(key))
              ? {
                  value: intrinsicGetAttribute.call(this, 'data-' + datasetName(key)),
                  writable: true,
                  enumerable: true,
                  configurable: true,
                }
              : Reflect.getOwnPropertyDescriptor(target, key),
        });
        datasetCache.set(this, value);
      }
      return value;
    },
    enumerable: true,
    configurable: true,
  });
  class SVGElement extends Element {
    get style() {
      let style = styleCache.get(this);
      if (!style) {
        style = cssDeclaration(this);
        styleCache.set(this, style);
      }
      return style;
    }
  }
  const svgRectSlots = new WeakMap();
  class SVGRect {
    constructor(token) {
      if (token !== hostToken) illegal('SVGRect');
      svgRectSlots.set(this, { x: 0, y: 0, width: 0, height: 0 });
    }
    get x() {
      return svgRectSlots.get(this).x;
    }
    set x(value) {
      svgRectSlots.get(this).x = Number(value);
    }
    get y() {
      return svgRectSlots.get(this).y;
    }
    set y(value) {
      svgRectSlots.get(this).y = Number(value);
    }
    get width() {
      return svgRectSlots.get(this).width;
    }
    set width(value) {
      svgRectSlots.get(this).width = Number(value);
    }
    get height() {
      return svgRectSlots.get(this).height;
    }
    set height(value) {
      svgRectSlots.get(this).height = Number(value);
    }
  }
  class SVGSVGElement extends SVGElement {
    createSVGRect() {
      return new SVGRect(hostToken);
    }
  }
  const svgElementInterfaces = {
    svg: 'SVGSVGElement',
    g: 'SVGGElement',
    a: 'SVGAElement',
    defs: 'SVGDefsElement',
    symbol: 'SVGSymbolElement',
    switch: 'SVGSwitchElement',
    rect: 'SVGRectElement',
    circle: 'SVGCircleElement',
    ellipse: 'SVGEllipseElement',
    line: 'SVGLineElement',
    polyline: 'SVGPolylineElement',
    polygon: 'SVGPolygonElement',
    path: 'SVGPathElement',
    text: 'SVGTextElement',
    tspan: 'SVGTSpanElement',
    textPath: 'SVGTextPathElement',
    image: 'SVGImageElement',
    use: 'SVGUseElement',
    foreignObject: 'SVGForeignObjectElement',
    clipPath: 'SVGClipPathElement',
    mask: 'SVGMaskElement',
    pattern: 'SVGPatternElement',
    marker: 'SVGMarkerElement',
    linearGradient: 'SVGLinearGradientElement',
    radialGradient: 'SVGRadialGradientElement',
    stop: 'SVGStopElement',
    title: 'SVGTitleElement',
    desc: 'SVGDescElement',
    metadata: 'SVGMetadataElement',
    style: 'SVGStyleElement',
    filter: 'SVGFilterElement',
    feBlend: 'SVGFEBlendElement',
    feColorMatrix: 'SVGFEColorMatrixElement',
    feComponentTransfer: 'SVGFEComponentTransferElement',
    feComposite: 'SVGFECompositeElement',
    feConvolveMatrix: 'SVGFEConvolveMatrixElement',
    feDiffuseLighting: 'SVGFEDiffuseLightingElement',
    feDisplacementMap: 'SVGFEDisplacementMapElement',
    feDistantLight: 'SVGFEDistantLightElement',
    feDropShadow: 'SVGFEDropShadowElement',
    feFlood: 'SVGFEFloodElement',
    feFuncA: 'SVGFEFuncAElement',
    feFuncB: 'SVGFEFuncBElement',
    feFuncG: 'SVGFEFuncGElement',
    feFuncR: 'SVGFEFuncRElement',
    feGaussianBlur: 'SVGFEGaussianBlurElement',
    feImage: 'SVGFEImageElement',
    feMerge: 'SVGFEMergeElement',
    feMergeNode: 'SVGFEMergeNodeElement',
    feMorphology: 'SVGFEMorphologyElement',
    feOffset: 'SVGFEOffsetElement',
    fePointLight: 'SVGFEPointLightElement',
    feSpecularLighting: 'SVGFESpecularLightingElement',
    feSpotLight: 'SVGFESpotLightElement',
    feTile: 'SVGFETileElement',
    feTurbulence: 'SVGFETurbulenceElement',
    animate: 'SVGAnimateElement',
    animateMotion: 'SVGAnimateMotionElement',
    animateTransform: 'SVGAnimateTransformElement',
    set: 'SVGSetElement',
    mpath: 'SVGMPathElement',
    view: 'SVGViewElement',
    script: 'SVGScriptElement',
  };
  const svgElementPrototypes = new Map();
  const createSVGWrapper = (data) => {
    const prototype = svgElementPrototypes.get(
      (data.qualifiedName || String(data.tagName).toLowerCase()).split(':').at(-1),
    );
    if (prototype) {
      const value = Object.create(prototype);
      elementData.set(value, data);
      return value;
    }
    return new SVGElement(hostToken, data);
  };
  const elementHandlers = new WeakMap(),
    handlersFor = (value) => {
      let handlers = elementHandlers.get(value);
      if (!handlers) {
        handlers = {};
        elementHandlers.set(value, handlers);
      }
      return handlers;
    };
  class HTMLScriptElement extends HTMLElement {
    constructor(token, data) {
      super(token, data);
    }
    get onload() {
      return handlersFor(this).load || null;
    }
    set onload(v) {
      handlersFor(this).load = typeof v === 'function' ? v : null;
    }
    get onerror() {
      return handlersFor(this).error || null;
    }
    set onerror(v) {
      handlersFor(this).error = typeof v === 'function' ? v : null;
    }
    get src() {
      const value = intrinsicGetAttribute.call(this, 'src');
      return value === null ? '' : host.urlParts(value).href;
    }
    set src(v) {
      trustedSetAttributeProperty(this, 'src', v, 'TrustedScriptURL', 'HTMLScriptElement');
    }
    get text() {
      return super.textContent;
    }
    set text(v) {
      trustedSetScriptSource(this, v, 'text');
    }
    get textContent() {
      return super.textContent;
    }
    set textContent(v) {
      trustedSetScriptSource(this, v, 'textContent');
    }
    get innerText() {
      return super.textContent;
    }
    set innerText(v) {
      trustedSetScriptSource(this, v, 'innerText');
    }
    get async() {
      return host.scriptAsync(elementSlot(this).nodeId);
    }
    set async(v) {
      host.scriptAsync(elementSlot(this).nodeId, false);
      if (v) host.setAttribute(elementSlot(this).nodeId, 'async', '');
      else host.removeAttribute(elementSlot(this).nodeId, 'async');
    }
    get defer() {
      return intrinsicGetAttribute.call(this, 'defer') !== null;
    }
    set defer(v) {
      if (v) host.setAttribute(elementSlot(this).nodeId, 'defer', '');
      else host.removeAttribute(elementSlot(this).nodeId, 'defer');
    }
    get crossOrigin() {
      return intrinsicGetAttribute.call(this, 'crossorigin');
    }
    set crossOrigin(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'crossorigin');
      else intrinsicSetAttribute.call(this, 'crossorigin', String(v));
    }
    get referrerPolicy() {
      return intrinsicGetAttribute.call(this, 'referrerpolicy') || '';
    }
    set referrerPolicy(v) {
      intrinsicSetAttribute.call(this, 'referrerpolicy', String(v));
    }
  }
  class HTMLImageElement extends HTMLElement {
    constructor(token, data) {
      super(token, data);
    }
    get onload() {
      return handlersFor(this).load || null;
    }
    set onload(v) {
      handlersFor(this).load = typeof v === 'function' ? v : null;
    }
    get onerror() {
      return handlersFor(this).error || null;
    }
    set onerror(v) {
      handlersFor(this).error = typeof v === 'function' ? v : null;
    }
    get src() {
      return host.urlParts(intrinsicGetAttribute.call(this, 'src') || '').href;
    }
    set src(v) {
      intrinsicSetAttribute.call(this, 'src', String(v));
    }
    get alt() {
      return intrinsicGetAttribute.call(this, 'alt') || '';
    }
    set alt(v) {
      intrinsicSetAttribute.call(this, 'alt', String(v));
    }
    get crossOrigin() {
      return intrinsicGetAttribute.call(this, 'crossorigin');
    }
    set crossOrigin(v) {
      if (v == null) intrinsicRemoveAttribute.call(this, 'crossorigin');
      else intrinsicSetAttribute.call(this, 'crossorigin', String(v));
    }
    get referrerPolicy() {
      return intrinsicGetAttribute.call(this, 'referrerpolicy') || '';
    }
    set referrerPolicy(v) {
      intrinsicSetAttribute.call(this, 'referrerpolicy', String(v));
    }
  }
  const mediaStates = new WeakMap(),
    mediaState = (element) => {
      let state = mediaStates.get(element);
      if (!state) {
        state = {
          paused: true,
          currentTime: 0,
          duration: NaN,
          volume: 1,
          muted: false,
          playbackRate: 1,
          defaultPlaybackRate: 1,
          readyState: 0,
          networkState: 0,
          currentSrc: '',
          pendingPlay: [],
        };
        mediaStates.set(element, state);
      }
      return state;
    };
  // Loading/reset observations are supported without a media decoder.
  // Playback is an explicit unsupported boundary, never a resolved fake play.
  const mediaControlsLists = new WeakMap();
  let createMediaTimeRanges;
  class HTMLMediaElement extends HTMLElement {
    get buffered() {
      const binding = requireRealmBinding(this, 'ElementGeometry');
      return callRealmBinding(this, binding, 'mediaTimeRanges', ['buffered']);
    }
    get played() {
      const binding = requireRealmBinding(this, 'ElementGeometry');
      return callRealmBinding(this, binding, 'mediaTimeRanges', ['played']);
    }
    get seekable() {
      const binding = requireRealmBinding(this, 'ElementGeometry');
      return callRealmBinding(this, binding, 'mediaTimeRanges', ['seekable']);
    }
    get controlsList() {
      let list = mediaControlsLists.get(this);
      if (!list) {
        list = new DOMTokenList(this, 'controlslist');
        mediaControlsLists.set(this, list);
      }
      return list;
    }
    set controlsList(value) {
      this.controlsList.value = String(value);
    }
    get src() {
      const value = intrinsicGetAttribute.call(this, 'src');
      return value === null ? '' : host.urlParts(value).href;
    }
    set src(value) {
      intrinsicSetAttribute.call(this, 'src', String(value));
      const state = mediaState(this);
      state.currentSrc = '';
      state.readyState = 0;
      state.networkState = 2;
    }
    get currentSrc() {
      return host.mediaCurrentSrc(elementSlot(this).nodeId);
    }
    get paused() {
      return mediaState(this).paused;
    }
    get ended() {
      return false;
    }
    get duration() {
      const source = host.mediaSourceState(elementSlot(this).nodeId);
      return source ? source.duration : mediaState(this).duration;
    }
    get readyState() {
      const source = host.mediaSourceState(elementSlot(this).nodeId);
      return source ? source.readyState : mediaState(this).readyState;
    }
    get networkState() {
      return mediaState(this).networkState;
    }
    load() {
      const state = mediaState(this);
      state.paused = true;
      state.readyState = 0;
      state.currentSrc = '';
      state.networkState =
        intrinsicHasAttribute.call(this, 'src') || this.querySelector('source') ? 2 : 0;
      host.mediaLoad(elementSlot(this).nodeId);
    }
    play() {
      const state = mediaState(this);
      state.paused = false;
      if (state.readyState > 0) {
        queueMicrotask(() => dispatchTrusted(this, new Event('play')));
        queueMicrotask(() => dispatchTrusted(this, new Event('playing')));
        return platformPromiseResolve();
      }
      host.mediaLoad(elementSlot(this).nodeId);
      return new platformPromise((resolve, reject) => state.pendingPlay.push({ resolve, reject }));
    }
    pause() {
      const state = mediaState(this);
      if (state.paused) return;
      state.paused = true;
      queueMicrotask(() => dispatchTrusted(this, new Event('pause')));
    }
  }
  for (const key of ['currentTime', 'volume', 'muted', 'playbackRate', 'defaultPlaybackRate'])
    def(HTMLMediaElement.prototype, key, {
      get() {
        return mediaState(this)[key];
      },
      set(value) {
        const state = mediaState(this),
          number = key === 'muted' ? Boolean(value) : Number(value);
        if (key === 'volume' && (number < 0 || number > 1))
          throw platformDOMException(
            'The volume provided is outside the range [0, 1].',
            'IndexSizeError',
          );
        state[key] = number;
      },
    });
  class HTMLAudioElement extends HTMLMediaElement {}
  class HTMLVideoElement extends HTMLMediaElement {
    get videoWidth() {
      const source = host.mediaSourceState(elementSlot(this).nodeId);
      return source ? source.width : 0;
    }
    get videoHeight() {
      const source = host.mediaSourceState(elementSlot(this).nodeId);
      return source ? source.height : 0;
    }
    get poster() {
      const value = intrinsicGetAttribute.call(this, 'poster');
      return value === null ? '' : host.urlParts(value).href;
    }
    set poster(value) {
      intrinsicSetAttribute.call(this, 'poster', String(value));
    }
    get width() {
      return Number(intrinsicGetAttribute.call(this, 'width') || 0);
    }
    set width(value) {
      intrinsicSetAttribute.call(this, 'width', String(Number(value) >>> 0));
    }
    get height() {
      return Number(intrinsicGetAttribute.call(this, 'height') || 0);
    }
    set height(value) {
      intrinsicSetAttribute.call(this, 'height', String(Number(value) >>> 0));
    }
  }
  const iframeSandboxCache = new WeakMap();
  class HTMLIFrameElement extends HTMLElement {
    get name() {
      return intrinsicGetAttribute.call(this, 'name') || '';
    }
    set name(v) {
      intrinsicSetAttribute.call(this, 'name', String(v));
    }
    get allow() {
      return intrinsicGetAttribute.call(this, 'allow') || '';
    }
    set allow(v) {
      intrinsicSetAttribute.call(this, 'allow', String(v));
    }
    get referrerPolicy() {
      const value = (intrinsicGetAttribute.call(this, 'referrerpolicy') || '').toLowerCase();
      return [
        'no-referrer',
        'no-referrer-when-downgrade',
        'same-origin',
        'origin',
        'strict-origin',
        'origin-when-cross-origin',
        'strict-origin-when-cross-origin',
        'unsafe-url',
      ].includes(value)
        ? value
        : '';
    }
    set referrerPolicy(v) {
      intrinsicSetAttribute.call(this, 'referrerpolicy', String(v));
    }
    get src() {
      return host.urlParts(intrinsicGetAttribute.call(this, 'src') || '').href;
    }
    set src(v) {
      intrinsicSetAttribute.call(this, 'src', String(v));
    }
    get srcdoc() {
      return intrinsicGetAttribute.call(this, 'srcdoc') || '';
    }
    set srcdoc(v) {
      trustedSetAttributeProperty(this, 'srcdoc', v, 'TrustedHTML', 'HTMLIFrameElement');
    }
    get width() {
      return intrinsicGetAttribute.call(this, 'width') || '';
    }
    set width(v) {
      intrinsicSetAttribute.call(this, 'width', String(v));
    }
    get height() {
      return intrinsicGetAttribute.call(this, 'height') || '';
    }
    set height(v) {
      intrinsicSetAttribute.call(this, 'height', String(v));
    }
    get sandbox() {
      let list = iframeSandboxCache.get(this);
      if (!list) {
        list = new DOMTokenList(this, 'sandbox');
        iframeSandboxCache.set(this, list);
      }
      return list;
    }
    set sandbox(v) {
      this.sandbox.value = String(v);
    }
    get contentWindow() {
      const id = host.iframeWindow(elementSlot(this).nodeId, isNodeConnected(this));
      return id == null ? null : remoteWindow(id);
    }
    get contentDocument() {
      const id = host.iframeWindow(elementSlot(this).nodeId, isNodeConnected(this));
      return id == null ? null : remoteDocument(id, true);
    }
  }
  const anchorRelLists = new WeakMap();
  class HTMLAnchorElement extends HTMLElement {
    get type() {
      return intrinsicGetAttribute.call(this, 'type') || '';
    }
    set type(value) {
      host.setAttribute(elementSlot(this).nodeId, 'type', String(value));
    }
    get href() {
      return host.urlParts(intrinsicGetAttribute.call(this, 'href') || '').href;
    }
    set href(v) {
      intrinsicSetAttribute.call(this, 'href', String(v));
    }
    get target() {
      return intrinsicGetAttribute.call(this, 'target') || '';
    }
    set target(v) {
      intrinsicSetAttribute.call(this, 'target', String(v));
    }
    get rel() {
      return intrinsicGetAttribute.call(this, 'rel') || '';
    }
    set rel(v) {
      intrinsicSetAttribute.call(this, 'rel', String(v));
    }
    get relList() {
      let list = anchorRelLists.get(this);
      if (!list) {
        list = new DOMTokenList(this, 'rel');
        anchorRelLists.set(this, list);
      }
      return list;
    }
    get origin() {
      return host.urlParts(this.href).origin;
    }
    get protocol() {
      return host.urlParts(this.href).protocol;
    }
    set protocol(v) {
      const p = host.urlParts(this.href);
      this.href = String(v) + p.href.slice(p.protocol.length);
    }
    get host() {
      return host.urlParts(this.href).host;
    }
    get hostname() {
      return host.urlParts(this.href).hostname;
    }
    get port() {
      return host.urlParts(this.href).port;
    }
    get pathname() {
      return host.urlParts(this.href).pathname;
    }
    get search() {
      return host.urlParts(this.href).search;
    }
    get hash() {
      return host.urlParts(this.href).hash;
    }
    toString() {
      return this.href;
    }
  }
  class HTMLLinkElement extends HTMLElement {
    get type() {
      return intrinsicGetAttribute.call(this, 'type') || '';
    }
    set type(value) {
      intrinsicSetAttribute.call(this, 'type', String(value));
    }
    get onload() {
      return handlersFor(this).load || null;
    }
    set onload(value) {
      handlersFor(this).load = typeof value === 'function' ? value : null;
    }
    get onerror() {
      return handlersFor(this).error || null;
    }
    set onerror(value) {
      handlersFor(this).error = typeof value === 'function' ? value : null;
    }
    get href() {
      const value = intrinsicGetAttribute.call(this, 'href');
      return value === null ? '' : host.urlParts(value).href;
    }
    set href(value) {
      intrinsicSetAttribute.call(this, 'href', String(value));
    }
    get rel() {
      return intrinsicGetAttribute.call(this, 'rel') || '';
    }
    set rel(value) {
      intrinsicSetAttribute.call(this, 'rel', String(value));
    }
    get as() {
      return intrinsicGetAttribute.call(this, 'as') || '';
    }
    set as(value) {
      intrinsicSetAttribute.call(this, 'as', String(value));
    }
    get crossOrigin() {
      return intrinsicGetAttribute.call(this, 'crossorigin');
    }
    set crossOrigin(value) {
      if (value == null) intrinsicRemoveAttribute.call(this, 'crossorigin');
      else intrinsicSetAttribute.call(this, 'crossorigin', String(value));
    }
  }
  class HTMLMetaElement extends HTMLElement {
    get httpEquiv() {
      return intrinsicGetAttribute.call(this, 'http-equiv') || '';
    }
    set httpEquiv(value) {
      intrinsicSetAttribute.call(this, 'http-equiv', String(value));
    }
    get content() {
      return intrinsicGetAttribute.call(this, 'content') || '';
    }
    set content(value) {
      intrinsicSetAttribute.call(this, 'content', String(value));
    }
  }
  const htmlElementInterfaces = {
    DETAILS: 'HTMLDetailsElement',
    SLOT: 'HTMLSlotElement',
    DIALOG: 'HTMLDialogElement',
    HTML: 'HTMLHtmlElement',
    HEAD: 'HTMLHeadElement',
    BODY: 'HTMLBodyElement',
    DIV: 'HTMLDivElement',
    H1: 'HTMLHeadingElement',
    H2: 'HTMLHeadingElement',
    H3: 'HTMLHeadingElement',
    H4: 'HTMLHeadingElement',
    H5: 'HTMLHeadingElement',
    H6: 'HTMLHeadingElement',
    P: 'HTMLParagraphElement',
    SPAN: 'HTMLSpanElement',
    STYLE: 'HTMLStyleElement',
    LINK: 'HTMLLinkElement',
    META: 'HTMLMetaElement',
    OL: 'HTMLOListElement',
    UL: 'HTMLUListElement',
    LI: 'HTMLLIElement',
    FORM: 'HTMLFormElement',
    FIELDSET: 'HTMLFieldSetElement',
    OUTPUT: 'HTMLOutputElement',
    INPUT: 'HTMLInputElement',
    BUTTON: 'HTMLButtonElement',
    LABEL: 'HTMLLabelElement',
    TABLE: 'HTMLTableElement',
    THEAD: 'HTMLTableSectionElement',
    TBODY: 'HTMLTableSectionElement',
    TFOOT: 'HTMLTableSectionElement',
    TR: 'HTMLTableRowElement',
    TD: 'HTMLTableCellElement',
    TH: 'HTMLTableCellElement',
    SELECT: 'HTMLSelectElement',
    OPTION: 'HTMLOptionElement',
    TEXTAREA: 'HTMLTextAreaElement',
    CANVAS: 'HTMLCanvasElement',
    AUDIO: 'HTMLAudioElement',
    VIDEO: 'HTMLVideoElement',
  };
  const elementWrappers = new Map();
  // Document identity is stable for the realm, including document.open().
  // Restore its host ID when cloning a bootstrap snapshot into a new realm.
  let realmDocumentRootID = host.documentRootID();
  bootstrapRestoreHooks.push(() => {
    realmDocumentRootID = host.documentRootID();
  });
  const wrap = (d) => {
    if (typeof d === 'number') {
      if (d === realmDocumentRootID) return document;
      const cached = elementWrappers.get(String(d));
      if (cached) return cached;
      d = host.nodeData(d);
    }
    if (!d) return null;
    if (d.type === 'document') return wrapDocumentNode(d);
    const key = String(d.nodeId),
      cached = elementWrappers.get(key);
    if (cached) {
      const slot = elementSlot(cached);
      if (slot) Object.assign(slot, d);
      return cached;
    }
    const label =
      d.type === 'element'
        ? 'Element<' + String(d.tagName || '').toLowerCase() + '>'
        : d.type === 'comment'
          ? 'Comment'
          : 'Text';
    // Canonical nodes without own fields can use the final observed object
    // directly. This avoids retaining a second element through its geometry
    // closure and constructing then copying a temporary object.
    if (typeof host.createObservedObject === 'function') {
      let prototype;
      if (d.type === 'doctype') prototype = (globalThis.DocumentType || Node).prototype;
      else if (d.type === 'fragment') prototype = DocumentFragment.prototype;
      else if (d.type === 'text') prototype = Text.prototype;
      else if (d.type === 'comment') prototype = Comment.prototype;
      else if (
        d.type === 'element' &&
        d.namespaceURI !== 'http://www.w3.org/2000/svg' &&
        d.tagName !== 'SCRIPT' &&
        d.tagName !== 'IMG' &&
        d.tagName !== 'IFRAME' &&
        d.tagName !== 'A'
      ) {
        const name = htmlElementInterfaces[d.tagName],
          ctor = name && globalThis[name];
        prototype =
          typeof ctor === 'function' && ctor.prototype ? ctor.prototype : HTMLElement.prototype;
      }
      if (prototype) {
        const node = createNativeObserved(label);
        Object.setPrototypeOf(node, prototype);
        elementData.set(node, d);
        if (d.type === 'element') registerElementGeometry(node);
        elementWrappers.set(key, node);
        return node;
      }
    }
    let element;
    if (d.type === 'doctype') {
      element = Object.create((globalThis.DocumentType || Node).prototype);
      elementData.set(element, d);
    } else if (d.type === 'fragment') {
      element = Object.create(DocumentFragment.prototype);
      elementData.set(element, d);
    } else if (d.type === 'text') element = new Text(hostToken, d);
    else if (d.type === 'comment') element = new Comment(hostToken, d);
    else if (d.namespaceURI === 'http://www.w3.org/2000/svg') element = createSVGWrapper(d);
    else if (d.tagName === 'SCRIPT') element = new HTMLScriptElement(hostToken, d);
    else if (d.tagName === 'IMG') element = new HTMLImageElement(hostToken, d);
    else if (d.tagName === 'IFRAME') element = new HTMLIFrameElement(hostToken, d);
    else if (d.tagName === 'A') element = new HTMLAnchorElement(hostToken, d);
    else {
      const name = htmlElementInterfaces[d.tagName],
        ctor = name && globalThis[name];
      if (typeof ctor === 'function' && ctor.prototype) {
        element = Object.create(ctor.prototype);
        elementData.set(element, d);
      } else element = new HTMLElement(hostToken, d);
    }
    if (d.type === 'element' && !bindingGet(element)) registerElementGeometry(element);
    const proxy = observe(label, element);
    elementWrappers.set(key, proxy);
    return proxy;
  };
  globalThis.__mimicDispatchFrameLoad = (nodeId) => {
    const element = elementWrappers.get(String(nodeId));
    if (element) dispatchNative(element, new Event('load'));
  };
  globalThis.__mimicDispatchResourceEvent = (nodeId, type) => {
    const element = wrap(nodeId);
    if (!element) return;
    if (element instanceof HTMLMediaElement) {
      const state = mediaState(element);
      if (type === 'loadedmetadata') {
        state.currentSrc = host.mediaCurrentSrc(elementSlot(element).nodeId);
        state.networkState = 1;
        state.readyState = 1;
      } else if (type === 'canplay') {
        state.networkState = 1;
        state.readyState = 4;
        const pending = state.pendingPlay.splice(0);
        for (const promise of pending) promise.resolve();
        if (!state.paused) {
          queueMicrotask(() => dispatchTrusted(element, new Event('play')));
          queueMicrotask(() => dispatchTrusted(element, new Event('playing')));
        }
      } else if (type === 'error') {
        state.currentSrc = '';
        state.networkState = 3;
        state.readyState = 0;
        state.paused = true;
        const error = platformDOMException(
          'The element has no supported sources.',
          'NotSupportedError',
        );
        for (const promise of state.pendingPlay.splice(0)) promise.reject(error);
      }
    }
    dispatchTrusted(element, new Event(type));
  };
  globalThis.__mimicNetworkStateEvent = (type) => dispatchNative(window, new Event(type));
  // Cache by owner, query kind and original argument: equivalent selectors can
  // still identify distinct collections in Chrome. Weak owners release on teardown.
  const liveCollectionCache = new WeakMap();
  let domCollectionRevision = 0;
  // Loading parsers mutate the DOM outside JS. After loading, the collection
  // revision is local until document.open() resets this hint.
  let collectionLoadingFinished = false;
  const resetCollectionReadyState = () => {
    collectionLoadingFinished = false;
  };
  const domCollectionVersion = () => {
    if (canonicalRevisionWord) return canonicalDOMRevision() + ':' + domCollectionRevision;
    if (!collectionLoadingFinished) collectionLoadingFinished = host.readyState() !== 'loading';
    return collectionLoadingFinished ? domCollectionRevision : 'host:' + host.domRevision();
  };
  const invalidateDOMCollections = (geometryTarget) => {
    domCollectionRevision++;
    invalidateRetainedGeometry(geometryTarget);
    // A CDP call can hold one read snapshot across a large injected-script
    // traversal. Once that call mutates DOM, later reads must re-enter through
    // the canonical host epoch rather than observe the pre-mutation snapshot.
    styleReadCache = null;
  };
  bootstrapRestoreHooks.push(invalidateDOMCollections);
  const cachedHTMLCollection = (owner, kind, key, read) => {
    let kinds = liveCollectionCache.get(owner);
    if (!kinds) {
      kinds = new Map();
      liveCollectionCache.set(owner, kinds);
    }
    let queries = kinds.get(kind);
    if (!queries) {
      queries = new Map();
      kinds.set(kind, queries);
    }
    if (!queries.has(key)) queries.set(key, htmlCollection(read));
    return queries.get(key);
  };
  const htmlCollection = (get) => {
    const indexed = (key) =>
      typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key) && Number(key) < 4294967295;
    const named = (values, name) => {
      name = String(name);
      if (!name) return null;
      for (const value of values) {
        const node = wrap(value);
        if (
          node &&
          (node.getAttribute('id') === name ||
            (node.namespaceURI === 'http://www.w3.org/1999/xhtml' &&
              node.getAttribute('name') === name))
        )
          return node;
      }
      return null;
    };
    const proxy = new Proxy(Object.create(HTMLCollection.prototype), {
      ownKeys(o) {
        const values = get(),
          keys = values.map((_, index) => String(index));
        for (const value of values) {
          const node = wrap(value);
          for (const name of [
            node.getAttribute('id'),
            node.namespaceURI === 'http://www.w3.org/1999/xhtml' ? node.getAttribute('name') : null,
          ])
            if (name && !keys.includes(name)) keys.push(name);
        }
        // A JS Proxy cannot reproduce Blink's duplicate numeric keys when an
        // element ID equals an index: proxy invariants require unique keys.
        // Exact parity for that case requires a native legacy-object interceptor.
        return [...new Set([...keys, ...Reflect.ownKeys(o)])];
      },
      getOwnPropertyDescriptor(o, key) {
        if (indexed(key)) {
          const values = get();
          if (Number(key) < values.length)
            return {
              value: wrap(values[Number(key)]),
              writable: false,
              enumerable: true,
              configurable: true,
            };
        }
        const own = Reflect.getOwnPropertyDescriptor(o, key);
        if (own) return own;
        if (typeof key === 'string' && !Reflect.has(o, key)) {
          const value = named(get(), key);
          if (value !== null)
            return { value, writable: false, enumerable: false, configurable: true };
        }
      },
      has(o, key) {
        if (indexed(key)) return Number(key) < get().length;
        return Reflect.has(o, key) || (typeof key === 'string' && named(get(), key) !== null);
      },
      get(o, key, receiver) {
        if (indexed(key)) {
          const values = get();
          return Number(key) < values.length ? wrap(values[Number(key)]) : undefined;
        }
        if (Reflect.has(o, key)) return Reflect.get(o, key, receiver);
        return typeof key === 'string' ? named(get(), key) || undefined : undefined;
      },
    });
    registerRealmBinding(proxy, 'HTMLCollection', {
      length: () => get().length,
      item: (index) => {
        const values = get();
        return index < values.length ? wrap(values[index]) : null;
      },
      namedItem: (name) => named(get(), name),
    });
    return proxy;
  };
  class HTMLCollection {
    constructor() {
      illegal('HTMLCollection');
    }
    get length() {
      const binding = requireRealmBinding(this, 'HTMLCollection');
      return callRealmBinding(this, binding, 'length', []);
    }
    item(index) {
      const binding = requireRealmBinding(this, 'HTMLCollection');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      index = +index >>> 0;
      return callRealmBinding(this, binding, 'item', [index]);
    }
    namedItem(name) {
      const binding = requireRealmBinding(this, 'HTMLCollection');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      name = bindingString(name);
      return callRealmBinding(this, binding, 'namedItem', [name]);
    }
  }
  Object.defineProperty(HTMLCollection.prototype, Symbol.iterator, {
    value: Array.prototype.values,
    writable: true,
    configurable: true,
  });
  const collectionIndex = (key) =>
    typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key) && Number(key) < 4294967295;
  const nodeListView = (length, item) => {
    const proxy = new Proxy(Object.create(NodeList.prototype), {
      get(target, key, receiver) {
        if (collectionIndex(key)) return item(Number(key)) ?? undefined;
        return Reflect.get(target, key, receiver);
      },
      has(target, key) {
        return (collectionIndex(key) && Number(key) < length()) || Reflect.has(target, key);
      },
      ownKeys(target) {
        return [
          ...new Set([
            ...Array.from({ length: length() }, (_, i) => String(i)),
            ...Reflect.ownKeys(target),
          ]),
        ];
      },
      getOwnPropertyDescriptor(target, key) {
        if (collectionIndex(key) && Number(key) < length())
          return {
            value: item(Number(key)),
            writable: false,
            enumerable: true,
            configurable: true,
          };
        return Reflect.getOwnPropertyDescriptor(target, key);
      },
    });
    registerRealmBinding(proxy, 'NodeList', { length, item: (index) => item(index) ?? null });
    return proxy;
  };
  const nodeList = (data) => {
    const read = () => (typeof data === 'function' ? data() : data);
    return nodeListView(
      () => read().length,
      (index) => wrap(read()[index]),
    );
  };
  class NodeList {
    constructor() {
      illegal('NodeList');
    }
    get length() {
      const binding = requireRealmBinding(this, 'NodeList');
      return callRealmBinding(this, binding, 'length', []);
    }
    item(index) {
      const binding = requireRealmBinding(this, 'NodeList');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      index = +index >>> 0;
      return callRealmBinding(this, binding, 'item', [index]);
    }
  }
  for (const name of ['entries', 'keys', 'values', 'forEach'])
    Object.defineProperty(NodeList.prototype, name, {
      value: Array.prototype[name],
      writable: true,
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(NodeList.prototype, Symbol.iterator, {
    value: Array.prototype.values,
    writable: true,
    configurable: true,
  });
  // New node shape is already known here; only canonical identity crosses back.
  const freshNodeData = (nodeId, type, tagName, namespaceURI, text) => ({
    attributes: {},
    children: [],
    namespaceURI,
    nodeId,
    parentId: 0,
    tagName,
    text,
    type,
  });
  // Host character data is a trusted JSON string containing exact UTF-16 code
  // units. Decode it directly: some engines' JSON parsers replace lone
  // surrogates with U+FFFD, losing canonical DOMString observations.
  const domStringJSONEncode = JSON.stringify,
    domStringFromCodeUnit = String.fromCharCode,
    domStringCodeUnit = String.prototype.charCodeAt,
    domStringSlice = String.prototype.slice,
    domStringParseInt = parseInt;
  const decodeDOMStringJSON = (encoded) => {
    let text = '';
    const escapes = { '"': '"', '\\': '\\', '/': '/', b: '\b', f: '\f', n: '\n', r: '\r', t: '\t' };
    for (let i = 1; i < encoded.length - 1; i++) {
      if (encoded[i] !== '\\') {
        text += encoded[i];
        continue;
      }
      const escape = encoded[++i];
      if (escape === 'u') {
        text += domStringFromCodeUnit(
          domStringParseInt(functionSourceApply(domStringSlice, encoded, [i + 1, i + 5]), 16),
        );
        i += 4;
      } else text += escapes[escape];
    }
    return text;
  };
  const canonicalTextContent = (id) => decodeDOMStringJSON(host.textContentJSON(id));
  const hasDOMStringSurrogate = (text) => {
    for (let i = 0; i < text.length; i++) {
      const unit = functionSourceApply(domStringCodeUnit, text, [i]);
      if (unit >= 0xd800 && unit <= 0xdfff) return true;
    }
    return false;
  };
  const setCanonicalTextContent = (id, text) => {
    if (hasDOMStringSurrogate(text)) host.setTextContent(id, '', domStringJSONEncode(text));
    else host.setTextContent(id, text);
  };
  const freshCharacterData = (id, type, text) => {
    if (hasDOMStringSurrogate(text)) {
      host.setCharacterDataJSON(id, domStringJSONEncode(text));
      return wrap(id);
    }
    return wrap(freshNodeData(id, type, '', '', text));
  };
  class Document extends Node {
    get currentScript() {
      return wrap(host.currentScript());
    }
    get title() {
      return host.title();
    }
    set title(v) {
      host.setTitle(String(v));
    }
    get lang() {
      return this.documentElement.getAttribute('lang') || '';
    }
    set lang(v) {
      this.documentElement.setAttribute('lang', String(v));
    }
    get dir() {
      return this.documentElement.getAttribute('dir') || '';
    }
    set dir(v) {
      this.documentElement.setAttribute('dir', String(v));
    }
    get readyState() {
      return host.readyState();
    }
    get visibilityState() {
      return host.windowState('documentHidden') ? 'hidden' : 'visible';
    }
    get hidden() {
      return host.windowState('documentHidden');
    }
    get prerendering() {
      return false;
    }
    get wasDiscarded() {
      return false;
    }
    get documentElement() {
      return wrap(host.query('html'));
    }
    get head() {
      return wrap(host.query('head'));
    }
    get body() {
      return wrap(host.query('body'));
    }
    get location() {
      return loc;
    }
    get URL() {
      return loc.href;
    }
    get documentURI() {
      return loc.href;
    }
    get cookie() {
      return host.documentCookie();
    }
    set cookie(v) {
      host.setDocumentCookie(String(v));
    }
    get featurePolicy() {
      return documentPolicy;
    }
    get permissionsPolicy() {
      return documentPolicy;
    }
    querySelector(s) {
      return wrap(host.query(String(s)));
    }
    querySelectorAll(s) {
      return nodeList(host.queryAll(String(s)));
    }
    getElementById(id) {
      return this.querySelector('#' + id);
    }
    getElementsByTagName(tag) {
      const name = String(tag);
      return htmlCollection(() => host.elementsByTagName(name));
    }
    createElement(tag) {
      tag = String(tag);
      validateDOMName(tag, false, 'createElement', 'Document');
      const value = host.create(tag);
      const element = wrap(
        typeof value === 'number'
          ? freshNodeData(value, 'element', tag.toUpperCase(), 'http://www.w3.org/1999/xhtml', '')
          : value,
      );
      invalidateRetainedGeometry(element);
      return element;
    }
    createElementNS(namespace, qualifiedName) {
      namespace = namespace == null ? '' : String(namespace);
      qualifiedName = validateDOMQualifiedName(
        namespace,
        String(qualifiedName),
        false,
        'createElementNS',
        'Document',
      );
      const element = wrap(host.createNS(namespace, qualifiedName));
      invalidateRetainedGeometry(element);
      return element;
    }
    createTextNode(data) {
      data = String(data);
      const node = freshCharacterData(host.createText(data), 'text', data);
      invalidateRetainedGeometry(node);
      return node;
    }
    createComment(data) {
      data = String(data);
      const node = freshCharacterData(host.createComment(data), 'comment', data);
      invalidateRetainedGeometry(node);
      return node;
    }
    createDocumentFragment() {
      return new DocumentFragment(hostToken);
    }
  }
  // HTML documents have an otherwise empty interface layer above Document.
  // Keeping that layer on the canonical wrapper preserves inherited ownership.
  class HTMLDocument extends Document {
    constructor(token) {
      if (token !== hostToken)
        throw new TypeError("Failed to construct 'HTMLDocument': Illegal constructor");
      super(token);
    }
  }
  Object.defineProperty(Document.prototype, 'createNodeIterator', {
    value: function (root, whatToShow = 0xffffffff, filter = null) {
      if (!isDOMNode(root))
        throw new TypeError(
          "Failed to execute 'createNodeIterator' on 'Document': parameter 1 is not of type 'Node'.",
        );
      const nodes = [],
        visit = (node) => {
          if (node instanceof Element) nodes.push(node);
          if (node instanceof Document) {
            const element = node.documentElement;
            if (element) visit(element);
          } else if (node && node.children)
            for (const child of Array.from(node.children)) visit(child);
        };
      visit(root);
      const shown = Number(whatToShow) >>> 0,
        accepted = nodes.filter((node) => {
          if (!(shown & 1)) return false;
          if (!filter) return true;
          const result = typeof filter === 'function' ? filter(node) : filter.acceptNode(node);
          return Number(result) === 1;
        });
      let index = -1;
      const iterator = Object.create(
        (globalThis.NodeIterator && globalThis.NodeIterator.prototype) || Object.prototype,
      );
      Object.defineProperties(iterator, {
        root: { value: root, enumerable: true },
        whatToShow: { value: shown, enumerable: true },
        filter: { value: filter, enumerable: true },
        referenceNode: { get: () => accepted[Math.max(index, 0)] || root, enumerable: true },
        pointerBeforeReferenceNode: { get: () => index < 0, enumerable: true },
        nextNode: { value: () => accepted[++index] || null },
        previousNode: { value: () => (index > 0 ? accepted[--index] : null) },
        detach: { value: () => {} },
      });
      return iterator;
    },
    writable: true,
    enumerable: true,
    configurable: true,
  });
  Object.defineProperties(Document.prototype, {
    all: {
      get: function () {
        return undefined;
      },
      enumerable: true,
      configurable: true,
    },
    scripts: {
      get: function () {
        return this.getElementsByTagName('script');
      },
      enumerable: true,
      configurable: true,
    },
    styleSheets: {
      get: function () {
        const owners = [
            ...Array.from(this.querySelectorAll('style')),
            ...Array.from(this.querySelectorAll('link')).filter(
              (node) =>
                String(node.getAttribute('rel') || '')
                  .toLowerCase()
                  .split(/\s+/)
                  .includes('stylesheet') && node.getAttribute('href') !== null,
            ),
          ],
          sheets = owners.map((ownerNode) => ({
            ownerNode,
            disabled: false,
            href:
              ownerNode.localName === 'link'
                ? new URL(ownerNode.getAttribute('href'), this.URL).href
                : null,
            media: { length: 0, mediaText: ownerNode.getAttribute('media') || '' },
            cssRules: [],
          }));
        return new Proxy(
          Object.create(
            (globalThis.StyleSheetList && globalThis.StyleSheetList.prototype) || Object.prototype,
          ),
          {
            get(target, key, receiver) {
              if (key === 'length') return sheets.length;
              if (key === 'item') return (index) => sheets[Number(index)] || null;
              if (key === Symbol.iterator) return sheets[Symbol.iterator].bind(sheets);
              if (typeof key === 'string' && /^\d+$/.test(key)) return sheets[Number(key)];
              return Reflect.get(target, key, receiver);
            },
          },
        );
      },
      enumerable: true,
      configurable: true,
    },
    referrer: { get: () => host.documentReferrer(), enumerable: true, configurable: true },
  });
  const permissionsPolicySlots = new WeakMap();
  class PermissionsPolicy {
    constructor(token, data) {
      if (token !== hostToken) illegal('PermissionsPolicy');
      const clauses = new Map();
      for (const raw of String(data.header || '').split(',')) {
        const clause = raw.trim();
        if (!clause) continue;
        const index = clause.indexOf('=');
        if (index < 0) continue;
        clauses.set(clause.slice(0, index).trim(), clause.slice(index + 1).trim());
      }
      permissionsPolicySlots.set(this, { clauses, origin: String(data.origin || 'null') });
    }
    features() {
      return Array.from(permissionsPolicySlots.get(this).clauses.keys()).sort();
    }
    allowsFeature(feature, origin) {
      if (!permissionsPolicySlots.has(this)) throw new TypeError('Illegal invocation');
      const current = host.permissionsPolicy(),
        hint = current.clientHints[String(feature)];
      if (hint !== undefined) {
        const target = origin === undefined ? current.origin : String(origin);
        return Array.from(hint || []).includes(target) || Array.from(hint || []).includes('*');
      }
      const state = permissionsPolicySlots.get(this),
        value = state.clauses.get(String(feature));
      if (value === undefined) return true;
      if (value === '()') return false;
      if (value === '*') return true;
      const target = origin === undefined ? state.origin : String(origin);
      return (
        this.getAllowlistForFeature(feature).includes(target) ||
        this.getAllowlistForFeature(feature).includes('*')
      );
    }
    allowedFeatures() {
      return this.features().filter((feature) => this.allowsFeature(feature));
    }
    getAllowlistForFeature(feature) {
      if (!permissionsPolicySlots.has(this)) throw new TypeError('Illegal invocation');
      const hint = host.permissionsPolicy().clientHints[String(feature)];
      if (hint !== undefined) return Array.from(hint || []);
      const state = permissionsPolicySlots.get(this),
        value = state.clauses.get(String(feature));
      if (value === undefined) return [state.origin];
      if (value === '()') return [];
      if (value === '*') return ['*'];
      const body = value.replace(/^\(|\)$/g, '').trim();
      if (!body) return [];
      return body
        .split(/\s+/)
        .map((item) => item.replace(/^['"]|['"]$/g, ''))
        .map((item) => (item === 'self' ? state.origin : item));
    }
  }
  const FeaturePolicy = PermissionsPolicy;
  class Navigator {
    constructor() {
      illegal('Navigator');
    }
  }
  class MimeType {
    constructor(token, type, suffixes, description) {
      if (token !== hostToken) illegal('MimeType');
      this.__type = type;
      this.__suffixes = suffixes;
      this.__description = description;
      this.__plugin = null;
    }
    get type() {
      return this.__type;
    }
    get suffixes() {
      return this.__suffixes;
    }
    get description() {
      return this.__description;
    }
    get enabledPlugin() {
      return this.__plugin;
    }
  }
  class Plugin {
    constructor(token, name) {
      if (token !== hostToken) illegal('Plugin');
      this.__name = name;
      this.__mimes = [];
    }
    get name() {
      return this.__name;
    }
    get filename() {
      return 'internal-pdf-viewer';
    }
    get description() {
      return 'Portable Document Format';
    }
    get length() {
      return this.__mimes.length;
    }
    item(index) {
      return this.__mimes[Number(index)] || null;
    }
    namedItem(name) {
      return this.__mimes.find((m) => m.type === String(name)) || null;
    }
    [Symbol.iterator]() {
      return this.__mimes[Symbol.iterator]();
    }
  }
  class PluginArray {
    constructor(token, plugins) {
      if (token !== hostToken) illegal('PluginArray');
      this.__plugins = plugins;
      plugins.forEach((plugin, index) => {
        Object.defineProperty(this, index, { value: plugin, enumerable: true });
        Object.defineProperty(this, plugin.name, { value: plugin, enumerable: false });
      });
    }
    get length() {
      return this.__plugins.length;
    }
    item(index) {
      return this.__plugins[Number(index)] || null;
    }
    namedItem(name) {
      return this.__plugins.find((p) => p.name === String(name)) || null;
    }
    refresh() {}
    [Symbol.iterator]() {
      return this.__plugins[Symbol.iterator]();
    }
  }
  class MimeTypeArray {
    constructor(token, mimes) {
      if (token !== hostToken) illegal('MimeTypeArray');
      this.__mimes = mimes;
      mimes.forEach((mime, index) => {
        Object.defineProperty(this, index, { value: mime, enumerable: true });
        Object.defineProperty(this, mime.type, { value: mime, enumerable: false });
      });
    }
    get length() {
      return this.__mimes.length;
    }
    item(index) {
      return this.__mimes[Number(index)] || null;
    }
    namedItem(name) {
      return this.__mimes.find((m) => m.type === String(name)) || null;
    }
    [Symbol.iterator]() {
      return this.__mimes[Symbol.iterator]();
    }
  }
  for (const ctor of [MimeType, Plugin, PluginArray, MimeTypeArray])
    Object.defineProperty(ctor.prototype, Symbol.toStringTag, {
      value: ctor.name,
      configurable: true,
    });
  const uaDataSlots = new WeakMap();
  const uaSnapshot = (value) => {
    if (!uaDataSlots.has(value)) throw new TypeError('Illegal invocation');
    return host.navigator();
  };
  class NavigatorUAData {
    constructor(token, data) {
      if (token !== hostToken) illegal('NavigatorUAData');
      uaDataSlots.set(this, data);
    }
    get brands() {
      return uaSnapshot(this).uaBrands.map((x) => ({ brand: x.brand, version: x.version }));
    }
    get mobile() {
      return uaSnapshot(this).uaMobile;
    }
    get platform() {
      return uaSnapshot(this).uaPlatform;
    }
    getHighEntropyValues(hints = []) {
      const data = uaSnapshot(this),
        out = { brands: this.brands, mobile: this.mobile, platform: this.platform };
      for (const hint of hints.map(String)) {
        if (hint === 'architecture') out.architecture = data.architecture;
        if (hint === 'bitness') out.bitness = data.bitness;
        if (hint === 'model') out.model = data.model;
        if (hint === 'platformVersion') out.platformVersion = data.platformVersion;
        if (hint === 'uaFullVersion') out.uaFullVersion = data.uaFullVersion;
        if (hint === 'fullVersionList')
          out.fullVersionList = data.uaFullVersionList.map((x) => ({ ...x }));
        if (hint === 'wow64') out.wow64 = data.wow64;
        if (hint === 'formFactors') out.formFactors = data.formFactors.slice();
      }
      return platformPromiseResolve(out);
    }
    toJSON() {
      return { brands: this.brands, mobile: this.mobile, platform: this.platform };
    }
  }
  class Screen {
    constructor() {
      illegal('Screen');
    }
  }
  class Location {
    constructor() {
      illegal('Location');
    }
    assign(value) {
      const binding = requireRealmBinding(this, 'Location');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      value = bindingString(value);
      callRealmBinding(this, binding, 'assign', [value]);
    }
    replace(value) {
      const binding = requireRealmBinding(this, 'Location');
      if (!arguments.length) throw new TypeError('Not enough arguments');
      value = bindingString(value);
      callRealmBinding(this, binding, 'replace', [value]);
    }
    reload() {
      const binding = requireRealmBinding(this, 'Location');
      callRealmBinding(this, binding, 'reload', []);
    }
    toString() {
      const binding = requireRealmBinding(this, 'Location');
      return callRealmBinding(this, binding, 'get', ['href']);
    }
  }
  class DOMStringList {
    constructor() {
      illegal('DOMStringList');
    }
  }
  delete DOMStringList.prototype.constructor;
  Object.defineProperties(DOMStringList.prototype, {
    length: {
      get() {
        const binding = requireRealmBinding(this, 'DOMStringList');
        return callRealmBinding(this, binding, 'length', []);
      },
      enumerable: true,
      configurable: true,
    },
    contains: {
      value: {
        contains(value) {
          const binding = requireRealmBinding(this, 'DOMStringList');
          if (!arguments.length) throw new TypeError('Not enough arguments');
          value = bindingString(value);
          return callRealmBinding(this, binding, 'contains', [value]);
        },
      }.contains,
      writable: true,
      enumerable: true,
      configurable: true,
    },
    item: {
      value: {
        item(index) {
          const binding = requireRealmBinding(this, 'DOMStringList');
          if (!arguments.length) throw new TypeError('Not enough arguments');
          index = +index >>> 0;
          return callRealmBinding(this, binding, 'item', [index]);
        },
      }.item,
      writable: true,
      enumerable: true,
      configurable: true,
    },
    constructor: { value: DOMStringList, writable: true, configurable: true },
    [Symbol.toStringTag]: { value: 'DOMStringList', configurable: true },
    [Symbol.iterator]: { value: Array.prototype.values, writable: true, configurable: true },
  });
  const createDOMStringList = (values) => {
    const target = Object.create(DOMStringList.prototype),
      index = (key) =>
        typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key) && Number(key) < 4294967295;
    for (let i = 0; i < values.length; i++)
      Object.defineProperty(target, String(i), {
        value: values[i],
        enumerable: true,
        configurable: true,
      });
    const list = new Proxy(target, {
      set: (target, key, value, receiver) =>
        index(key) || Reflect.set(target, key, value, receiver),
      defineProperty: (target, key, descriptor) =>
        index(key) || Reflect.defineProperty(target, key, descriptor),
      deleteProperty: (target, key) =>
        index(key) ? Number(key) >= values.length : Reflect.deleteProperty(target, key),
      preventExtensions: () => false,
    });
    registerRealmBinding(
      list,
      'DOMStringList',
      {
        length: () => values.length,
        item: (index) => values[index] ?? null,
        contains: (value) => values.includes(value),
      },
      true,
    );
    return list;
  };

  const historySlots = new WeakMap();
  const historyCloneBrandRejectors = [];
  // History keeps a private storage copy and a separate cached state object.
  // Native V8 cloning handles ECMAScript exotic objects without invoking proxy traps.
  const historyUncloneableHost = (value) => {
    if (blobSlots.has(value) || fileSlots.has(value))
      throw platformDOMException(
        'History storage of Blob and File requires platform serialization support.',
        'NotSupportedError',
      );
    return (
      value === globalThis ||
      value === document ||
      elementData.has(value) ||
      documentWrappers.has(value) ||
      eventSlots.has(value) ||
      historyCloneBrandRejectors.some((reject) => reject(value))
    );
  };
  let cloneCrossRealmHistoryState = null;
  const cloneHistoryState = (value) => {
    if (cloneCrossRealmHistoryState && referenceGet(value))
      return cloneCrossRealmHistoryState(value);
    const fail = () => {
      throw platformDOMException('The value could not be cloned.', 'DataCloneError');
    };
    if (historyUncloneableHost(value)) fail();
    return cloneCodec.clone(value);
  };
  registerBootstrapCallback('installHistoryClone', cloneHistoryState);
  /* shared_structured_clone */
  const cloneCodec = createStructuredCloneCodec(historyUncloneableHost, (value) =>
    referenceGet(value) && cloneCrossRealmHistoryState ? cloneCrossRealmHistoryState(value) : value,
  );
  globalThis.structuredClone = cloneCodec.clone;

  const requireActiveHistory = () => {
    if (!host.historyIsActive())
      throw platformDOMException('The document is not fully active.', 'SecurityError');
  };
  class History {
    constructor() {
      illegal('History');
    }
    pushState(s, t, u) {
      const binding = requireRealmBinding(this, 'History');
      if (arguments.length < 2) throw new TypeError('2 arguments required');
      bindingString(t);
      u = u == null ? '' : bindingString(u);
      return callRealmBinding(this, binding, 'update', [s, u, false]);
    }
    replaceState(s, t, u) {
      const binding = requireRealmBinding(this, 'History');
      if (arguments.length < 2) throw new TypeError('2 arguments required');
      bindingString(t);
      u = u == null ? '' : bindingString(u);
      return callRealmBinding(this, binding, 'update', [s, u, true]);
    }
    back() {
      const binding = requireRealmBinding(this, 'History');
      return callRealmBinding(this, binding, 'go', [-1]);
    }
    forward() {
      const binding = requireRealmBinding(this, 'History');
      return callRealmBinding(this, binding, 'go', [1]);
    }
    go(n = 0) {
      const binding = requireRealmBinding(this, 'History');
      n = +n | 0;
      return callRealmBinding(this, binding, 'go', [n]);
    }
    get length() {
      requireActiveHistory();
      return host.historyLength();
    }
    get state() {
      requireActiveHistory();
      return host.historyState();
    }
    get scrollRestoration() {
      requireActiveHistory();
      return historySlots.get(this).scrollRestoration;
    }
    set scrollRestoration(value) {
      value = String(value);
      if (value === 'auto' || value === 'manual') {
        requireActiveHistory();
        historySlots.get(this).scrollRestoration = value;
      }
    }
  }
  const storageAreas = new WeakMap();
  class Storage {
    constructor() {
      illegal('Storage');
    }
    get length() {
      return host.storageLength(storageAreas.get(this));
    }
    key(i) {
      const binding = requireRealmBinding(this, 'Storage');
      if (!arguments.length) throw new TypeError('1 argument required');
      i = +i >>> 0;
      return callRealmBinding(this, binding, 'key', [i]);
    }
    getItem(k) {
      const binding = requireRealmBinding(this, 'Storage');
      if (!arguments.length) throw new TypeError('1 argument required');
      k = bindingString(k);
      return callRealmBinding(this, binding, 'getItem', [k]);
    }
    setItem(k, v) {
      const binding = requireRealmBinding(this, 'Storage');
      if (arguments.length < 2) throw new TypeError('2 arguments required');
      k = bindingString(k);
      v = bindingString(v);
      return callRealmBinding(this, binding, 'setItem', [k, v]);
    }
    removeItem(k) {
      const binding = requireRealmBinding(this, 'Storage');
      if (!arguments.length) throw new TypeError('1 argument required');
      k = bindingString(k);
      return callRealmBinding(this, binding, 'removeItem', [k]);
    }
    clear() {
      const binding = requireRealmBinding(this, 'Storage');
      return callRealmBinding(this, binding, 'clear', []);
    }
  }
  const nativeEvalSource = host.nativeEvalSource();
  /* shared_trusted_types */
  const readableSlots = new WeakMap(),
    readableState = (value) => readableSlots.get(value),
    readableControllerSlots = new WeakMap(),
    readerSlots = new WeakMap(),
    pullReadable = (stream) => {
      const state = readableState(stream);
      if (state.pulling || state.state !== 'readable' || typeof state.source.pull !== 'function')
        return;
      state.pulling = true;
      platformPromiseResolve()
        .then(() => state.source.pull(state.controller))
        .catch((e) => state.controller.error(e))
        .then(() => {
          state.pulling = false;
          if (state.reads.length && state.state === 'readable') pullReadable(stream);
        });
    };
  class ReadableStreamDefaultController {
    constructor(stream) {
      readableControllerSlots.set(this, stream);
    }
    get desiredSize() {
      const state = readableState(readableControllerSlots.get(this));
      return state.state === 'readable' ? 1 - state.queue.length : null;
    }
    enqueue(chunk) {
      const state = readableState(readableControllerSlots.get(this));
      if (state.state !== 'readable') throw new TypeError('ReadableStream is not readable');
      if (state.reads.length) state.reads.shift().resolve({ done: false, value: chunk });
      else state.queue.push(chunk);
    }
    close() {
      const state = readableState(readableControllerSlots.get(this));
      if (state.state !== 'readable') return;
      state.state = 'closed';
      if (state.closed) state.closed.resolve();
      while (state.reads.length) state.reads.shift().resolve({ done: true, value: undefined });
    }
    error(reason) {
      const state = readableState(readableControllerSlots.get(this));
      if (state.state !== 'readable') return;
      state.state = 'errored';
      state.error = reason;
      if (state.closed) state.closed.reject(reason);
      while (state.reads.length) state.reads.shift().reject(reason);
    }
  }
  class ReadableStreamDefaultReader {
    constructor(stream) {
      if (!(stream instanceof ReadableStream))
        throw new TypeError('ReadableStreamDefaultReader requires a ReadableStream');
      if (stream.locked) throw new TypeError('ReadableStream is locked');
      const state = readableState(stream),
        closed =
          state.state === 'closed'
            ? platformPromiseResolve()
            : state.state === 'errored'
              ? platformPromiseReject(state.error)
              : new platformPromise((resolve, reject) => {
                  state.closed = { resolve, reject };
                });
      readerSlots.set(this, { stream, closed });
      state.reader = this;
    }
    get closed() {
      return readerSlots.get(this).closed;
    }
    read() {
      const slot = readerSlots.get(this),
        stream = slot.stream;
      if (!stream) return platformPromiseReject(new TypeError('Reader has been released'));
      const state = readableState(stream);
      state.disturbed = true;
      if (state.queue.length)
        return platformPromiseResolve({ done: false, value: state.queue.shift() });
      if (state.state === 'closed') return platformPromiseResolve({ done: true, value: undefined });
      if (state.state === 'errored') return platformPromiseReject(state.error);
      const result = new platformPromise((resolve, reject) =>
        state.reads.push({ resolve, reject }),
      );
      pullReadable(stream);
      return result;
    }
    cancel(reason) {
      const stream = readerSlots.get(this).stream;
      return stream
        ? stream.cancel(reason)
        : platformPromiseReject(new TypeError('Reader has been released'));
    }
    releaseLock() {
      const slot = readerSlots.get(this),
        stream = slot.stream;
      if (!stream) return;
      const state = readableState(stream);
      if (state.reads.length) throw new TypeError('Cannot release a reader with pending reads');
      state.reader = null;
      slot.stream = null;
    }
  }
  class ReadableStream {
    constructor(source = {}, strategy = {}) {
      const state = {
        source: source || {},
        queue: [],
        reads: [],
        reader: null,
        state: 'readable',
        error: undefined,
        disturbed: false,
        pulling: false,
        closed: null,
        controller: null,
      };
      readableSlots.set(this, state);
      state.controller = new ReadableStreamDefaultController(this);
      try {
        const started =
          typeof state.source.start === 'function'
            ? state.source.start(state.controller)
            : undefined;
        platformPromiseResolve(started).catch((e) => state.controller.error(e));
      } catch (e) {
        state.controller.error(e);
      }
    }
    get locked() {
      return readableState(this).reader !== null;
    }
    cancel(reason) {
      const state = readableState(this);
      if (this.locked) return platformPromiseReject(new TypeError('Cannot cancel a locked stream'));
      state.queue.length = 0;
      state.controller.close();
      try {
        return platformPromiseResolve(
          typeof state.source.cancel === 'function' ? state.source.cancel(reason) : undefined,
        );
      } catch (e) {
        return platformPromiseReject(e);
      }
    }
    getReader() {
      return new ReadableStreamDefaultReader(this);
    }
    pipeThrough(transform, options) {
      this.pipeTo(transform.writable, options);
      return transform.readable;
    }
    async pipeTo(destination) {
      const reader = this.getReader(),
        writer = destination.getWriter();
      try {
        for (;;) {
          const result = await reader.read();
          if (result.done) break;
          await writer.write(result.value);
        }
        await writer.close();
      } catch (e) {
        await writer.abort(e);
        throw e;
      } finally {
        reader.releaseLock();
        writer.releaseLock();
      }
    }
    tee() {
      const a = new ReadableStream(),
        b = new ReadableStream(),
        ca = readableState(a).controller,
        cb = readableState(b).controller,
        reader = this.getReader();
      (async () => {
        try {
          for (;;) {
            const r = await reader.read();
            if (r.done) {
              ca.close();
              cb.close();
              break;
            }
            ca.enqueue(r.value);
            cb.enqueue(r.value);
          }
        } catch (e) {
          ca.error(e);
          cb.error(e);
        }
      })();
      return [a, b];
    }
    values() {
      const reader = this.getReader();
      return {
        next: () => reader.read(),
        return: async () => {
          await reader.cancel();
          reader.releaseLock();
          return { done: true };
        },
        [Symbol.asyncIterator]() {
          return this;
        },
      };
    }
    [Symbol.asyncIterator]() {
      return this.values();
    }
  }
  const writerSlots = new WeakMap();
  class WritableStreamDefaultWriter {
    get ready() {
      return writerSlots.get(this).ready;
    }
    get closed() {
      return writerSlots.get(this).closed;
    }
    constructor(stream) {
      if (!(stream instanceof WritableStream) || stream.locked)
        throw new TypeError('WritableStream is locked');
      this.__stream = stream;
      stream.__writer = this;
      writerSlots.set(this, {
        ready: platformPromiseResolve(),
        closed:
          stream.__state === 'closed'
            ? platformPromiseResolve()
            : new platformPromise((resolve, reject) => (stream.__closed = { resolve, reject })),
      });
    }
    write(chunk) {
      return this.__stream.__write(chunk);
    }
    close() {
      return this.__stream.close();
    }
    abort(reason) {
      return this.__stream.abort(reason);
    }
    releaseLock() {
      if (this.__stream) {
        this.__stream.__writer = null;
        this.__stream = null;
      }
    }
  }
  class WritableStream {
    constructor(sink = {}, strategy = {}) {
      this.__sink = sink || {};
      this.__writer = null;
      this.__state = 'writable';
      this.__closed = null;
      try {
        platformPromiseResolve(
          typeof this.__sink.start === 'function' ? this.__sink.start(this) : undefined,
        ).catch((e) => this.__fail(e));
      } catch (e) {
        this.__fail(e);
      }
    }
    get locked() {
      return this.__writer !== null;
    }
    getWriter() {
      return new WritableStreamDefaultWriter(this);
    }
    __write(chunk) {
      if (this.__state !== 'writable')
        return platformPromiseReject(new TypeError('WritableStream is not writable'));
      try {
        return platformPromiseResolve(
          typeof this.__sink.write === 'function' ? this.__sink.write(chunk, this) : undefined,
        );
      } catch (e) {
        return platformPromiseReject(e);
      }
    }
    close() {
      if (this.__state !== 'writable')
        return platformPromiseReject(new TypeError('WritableStream is not writable'));
      this.__state = 'closed';
      try {
        return platformPromiseResolve(
          typeof this.__sink.close === 'function' ? this.__sink.close() : undefined,
        ).then((v) => {
          if (this.__closed) this.__closed.resolve();
          return v;
        });
      } catch (e) {
        return platformPromiseReject(e);
      }
    }
    abort(reason) {
      this.__state = 'errored';
      try {
        return platformPromiseResolve(
          typeof this.__sink.abort === 'function' ? this.__sink.abort(reason) : undefined,
        ).then((v) => {
          if (this.__closed) this.__closed.reject(reason);
          return v;
        });
      } catch (e) {
        return platformPromiseReject(e);
      }
    }
    __fail(e) {
      this.__state = 'errored';
      if (this.__closed) this.__closed.reject(e);
    }
  }
  class TransformStreamDefaultController {
    constructor(readable) {
      this.__readable = readable;
    }
    enqueue(chunk) {
      readableState(this.__readable).controller.enqueue(chunk);
    }
    error(reason) {
      readableState(this.__readable).controller.error(reason);
    }
    terminate() {
      readableState(this.__readable).controller.close();
    }
  }
  const transformSlots = new WeakMap();
  class TransformStream {
    get readable() {
      return transformSlots.get(this).readable;
    }
    get writable() {
      return transformSlots.get(this).writable;
    }
    constructor(transformer = {}) {
      const slot = { readable: new ReadableStream(), writable: null };
      transformSlots.set(this, slot);
      const controller = new TransformStreamDefaultController(slot.readable);
      slot.writable = new WritableStream({
        start: () =>
          typeof transformer.start === 'function' ? transformer.start(controller) : undefined,
        write: (chunk) =>
          typeof transformer.transform === 'function'
            ? transformer.transform(chunk, controller)
            : controller.enqueue(chunk),
        close: () =>
          platformPromiseResolve(
            typeof transformer.flush === 'function' ? transformer.flush(controller) : undefined,
          ).then(() => controller.terminate()),
        abort: (reason) => controller.error(reason),
      });
    }
  }
  const workerSlots = new WeakMap();
  class Worker extends EventTarget {
    constructor(scriptURL, options = {}) {
      super();
      const state = { id: 0, onmessage: null, onmessageerror: null, onerror: null };
      workerSlots.set(this, state);
      const deliver = (data) =>
          dispatchTrusted(this, new MessageEvent('message', { data: cloneCodec.decode(data) })),
        report = (message) => dispatchTrusted(this, new ErrorEvent('error', { message }));
      state.id = host.createWorker(
        deliver,
        report,
        trustedConvert(
          scriptURL,
          'TrustedScriptURL',
          'Worker constructor',
          "Failed to construct 'Worker': ",
        ),
        '',
        String(options.type || 'classic'),
        String(options.name || ''),
      );
    }
    postMessage(message) {
      const wire = cloneCodec.encode(message);
      host.workerPost(workerSlots.get(this).id, wire, cloneCodec.trace(wire));
    }
    terminate() {
      host.terminateWorker(workerSlots.get(this).id);
    }
  }
  for (const key of ['onmessage', 'onmessageerror', 'onerror'])
    def(Worker.prototype, key, {
      get() {
        return workerSlots.get(this)[key];
      },
      set(value) {
        workerSlots.get(this)[key] = value;
      },
    });
  /* shared_base64 */
  /* shared_webcrypto */
  const subtleCrypto = Object.create(SubtleCrypto.prototype);
  class Crypto {
    constructor() {
      illegal('Crypto');
    }
    get subtle() {
      return subtleCrypto;
    }
    getRandomValues(view) {
      if (
        !ArrayBuffer.isView(view) ||
        view instanceof Float32Array ||
        view instanceof Float64Array ||
        view instanceof DataView
      )
        throw new TypeError(
          "Failed to execute 'getRandomValues' on 'Crypto': parameter 1 is not of type 'ArrayBufferView'.",
        );
      if (view.byteLength > 65536)
        throw platformDOMException(
          "The ArrayBufferView's byte length exceeds the number of bytes of entropy available via this API (65536).",
          'QuotaExceededError',
        );
      const bytes = host.randomBytes(view.byteLength),
        raw = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
      for (let i = 0; i < raw.length; i++) raw[i] = bytes[i];
      return view;
    }
    randomUUID() {
      return host.randomUUID();
    }
  }
  /* shared_performance */
  /* shared_intersection_observer */
  class GPUAdapterInfo {
    constructor() {
      illegal('GPUAdapterInfo');
    }
  }
  class GPUSupportedFeatures {
    constructor() {
      illegal('GPUSupportedFeatures');
    }
  }
  class GPUSupportedLimits {
    constructor() {
      illegal('GPUSupportedLimits');
    }
  }
  class GPUAdapter {
    constructor() {
      illegal('GPUAdapter');
    }
  }
  class GPUDevice extends EventTarget {
    constructor() {
      super();
      illegal('GPUDevice');
    }
  }
  class GPU {
    constructor() {
      illegal('GPU');
    }
  }
  const rtcDescriptionSlots = new WeakMap();
  const rtcDescriptionState = (value) => {
    const state = rtcDescriptionSlots.get(value);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  class RTCSessionDescription {
    constructor(init = {}) {
      init = init == null ? {} : Object(init);
      rtcDescriptionSlots.set(this, { type: String(init.type || ''), sdp: String(init.sdp || '') });
    }
    get type() {
      return rtcDescriptionState(this).type;
    }
    get sdp() {
      return rtcDescriptionState(this).sdp;
    }
    toJSON() {
      const state = rtcDescriptionState(this);
      return { type: state.type, sdp: state.sdp };
    }
  }
  const rtcCandidateSlots = new WeakMap(),
    rtcIceEventSlots = new WeakMap();
  class RTCIceCandidate {
    constructor(init = {}) {
      if (init.sdpMid == null && init.sdpMLineIndex == null && String(init.candidate || ''))
        throw new TypeError(
          "Failed to construct 'RTCIceCandidate': sdpMid and sdpMLineIndex are both null.",
        );
      rtcCandidateSlots.set(this, {
        candidate: String(init.candidate || ''),
        sdpMid: init.sdpMid == null ? null : String(init.sdpMid),
        sdpMLineIndex: init.sdpMLineIndex == null ? null : Number(init.sdpMLineIndex),
        usernameFragment: init.usernameFragment == null ? null : String(init.usernameFragment),
      });
    }
    get candidate() {
      return rtcCandidateSlots.get(this).candidate;
    }
    get sdpMid() {
      return rtcCandidateSlots.get(this).sdpMid;
    }
    get sdpMLineIndex() {
      return rtcCandidateSlots.get(this).sdpMLineIndex;
    }
    get usernameFragment() {
      return rtcCandidateSlots.get(this).usernameFragment;
    }
    toJSON() {
      return {
        candidate: this.candidate,
        sdpMid: this.sdpMid,
        sdpMLineIndex: this.sdpMLineIndex,
        usernameFragment: this.usernameFragment,
      };
    }
  }
  class RTCPeerConnectionIceEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      rtcIceEventSlots.set(this, {
        candidate: init.candidate ?? null,
        url: String(init.url || ''),
      });
    }
    get candidate() {
      return rtcIceEventSlots.get(this).candidate;
    }
    get url() {
      return rtcIceEventSlots.get(this).url;
    }
  }
  const rtcDataStates = new WeakMap(),
    rtcPeerStates = new WeakMap(),
    rtcPeerPrivate = new WeakMap();
  class RTCDataChannel extends EventTarget {
    constructor(token, label, options = {}) {
      super();
      if (token !== hostToken) illegal('RTCDataChannel');
      rtcDataStates.set(this, {
        label: String(label),
        ordered: options.ordered === undefined ? true : Boolean(options.ordered),
        maxPacketLifeTime:
          options.maxPacketLifeTime == null ? null : Number(options.maxPacketLifeTime),
        maxRetransmits: options.maxRetransmits == null ? null : Number(options.maxRetransmits),
        protocol: String(options.protocol || ''),
        negotiated: Boolean(options.negotiated),
        id: options.id == null ? null : Number(options.id),
        readyState: 'connecting',
        bufferedAmount: 0,
        bufferedAmountLowThreshold: 0,
        binaryType: 'arraybuffer',
        onopen: null,
        onbufferedamountlow: null,
        onerror: null,
        onclosing: null,
        onclose: null,
        onmessage: null,
      });
    }
    get reliable() {
      const state = rtcDataStates.get(this);
      return state.ordered && state.maxPacketLifeTime === null && state.maxRetransmits === null;
    }
    send() {
      if (rtcDataStates.get(this).readyState !== 'open')
        throw platformDOMException('RTCDataChannel.readyState is not open', 'InvalidStateError');
    }
    close() {
      const state = rtcDataStates.get(this);
      if (state.readyState === 'closed' || state.readyState === 'closing') return;
      state.readyState = 'closing';
    }
  }
  for (const key of [
    'label',
    'ordered',
    'maxPacketLifeTime',
    'maxRetransmits',
    'protocol',
    'negotiated',
    'id',
    'readyState',
    'bufferedAmount',
  ])
    def(RTCDataChannel.prototype, key, {
      get() {
        return rtcDataStates.get(this)?.[key];
      },
    });
  for (const key of [
    'bufferedAmountLowThreshold',
    'binaryType',
    'onopen',
    'onbufferedamountlow',
    'onerror',
    'onclosing',
    'onclose',
    'onmessage',
  ])
    def(RTCDataChannel.prototype, key, {
      get() {
        return rtcDataStates.get(this)?.[key];
      },
      set(value) {
        rtcDataStates.get(this)[key] = value;
      },
    });
  const normalizeRTCConfiguration = (configuration) => {
    configuration = configuration || {};
    return {
      iceServers: Array.from(configuration.iceServers || [], (server) => ({
        urls: Array.isArray(server.urls) ? server.urls.map(String) : [String(server.urls || '')],
        username: String(server.username || ''),
        credential: String(server.credential || ''),
      })),
      iceTransportPolicy: String(configuration.iceTransportPolicy || 'all'),
      bundlePolicy: String(configuration.bundlePolicy || 'balanced'),
      rtcpMuxPolicy: 'require',
      iceCandidatePoolSize: Number(configuration.iceCandidatePoolSize || 0),
      certificates: Array.from(configuration.certificates || []),
      encodedInsertableStreams: Boolean(configuration.encodedInsertableStreams),
      alwaysNegotiateDataChannels: Boolean(configuration.alwaysNegotiateDataChannels),
    };
  };
  class RTCPeerConnection extends EventTarget {
    constructor(configuration = {}) {
      super();
      rtcPeerStates.set(this, {
        localDescription: null,
        currentLocalDescription: null,
        pendingLocalDescription: null,
        remoteDescription: null,
        currentRemoteDescription: null,
        pendingRemoteDescription: null,
        signalingState: 'stable',
        iceGatheringState: 'new',
        iceConnectionState: 'new',
        connectionState: 'new',
        canTrickleIceCandidates: null,
        onicecandidate: null,
        onicegatheringstatechange: null,
        oniceconnectionstatechange: null,
        onsignalingstatechange: null,
        onconnectionstatechange: null,
        ondatachannel: null,
      });
      rtcPeerPrivate.set(this, {
        configuration: normalizeRTCConfiguration(configuration),
        closed: false,
        hasDataChannel: false,
        iceUfrag: '',
      });
      return observe('RTCPeerConnection', this);
    }
    getConfiguration() {
      const configuration = rtcPeerPrivate.get(this).configuration;
      return {
        ...configuration,
        iceServers: configuration.iceServers.map((server) => ({
          ...server,
          urls: server.urls.slice(),
        })),
        certificates: configuration.certificates.slice(),
      };
    }
    setConfiguration(v) {
      rtcPeerPrivate.get(this).configuration = normalizeRTCConfiguration(v);
    }
    createOffer() {
      if (rtcPeerPrivate.get(this).closed)
        return platformPromiseReject(
          platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError'),
        );
      return platformPromiseResolve(
        new RTCSessionDescription({
          type: 'offer',
          sdp: 'v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n',
        }),
      );
    }
    createAnswer() {
      return platformPromiseResolve(
        new RTCSessionDescription({
          type: 'answer',
          sdp: 'v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n',
        }),
      );
    }
    setLocalDescription(description) {
      const state = rtcPeerStates.get(this),
        value =
          description instanceof RTCSessionDescription
            ? description
            : new RTCSessionDescription(description || { type: 'offer' });
      state.localDescription = value;
      state.currentLocalDescription = value;
      state.iceGatheringState = 'complete';
      return platformPromiseResolve();
    }
    setRemoteDescription(description) {
      const state = rtcPeerStates.get(this),
        value =
          description instanceof RTCSessionDescription
            ? description
            : new RTCSessionDescription(description);
      state.remoteDescription = value;
      state.currentRemoteDescription = value;
      return platformPromiseResolve();
    }
    addIceCandidate() {
      return platformPromiseResolve();
    }
    getSenders() {
      return [];
    }
    getReceivers() {
      return [];
    }
    getTransceivers() {
      return [];
    }
    close() {
      rtcPeerPrivate.get(this).closed = true;
      const state = rtcPeerStates.get(this);
      state.signalingState = 'closed';
      state.iceConnectionState = 'closed';
      state.connectionState = 'closed';
    }
  }
  for (const key of [
    'localDescription',
    'currentLocalDescription',
    'pendingLocalDescription',
    'remoteDescription',
    'currentRemoteDescription',
    'pendingRemoteDescription',
    'signalingState',
    'iceGatheringState',
    'iceConnectionState',
    'connectionState',
    'canTrickleIceCandidates',
  ])
    def(RTCPeerConnection.prototype, key, {
      get() {
        return rtcPeerStates.get(this)?.[key];
      },
    });
  for (const key of [
    'onicecandidate',
    'onicegatheringstatechange',
    'oniceconnectionstatechange',
    'onsignalingstatechange',
    'onconnectionstatechange',
    'ondatachannel',
  ])
    def(RTCPeerConnection.prototype, key, {
      get() {
        return rtcPeerStates.get(this)?.[key];
      },
      set(value) {
        rtcPeerStates.get(this)[key] = value;
      },
    });
  Object.defineProperty(RTCPeerConnection.prototype, 'createDataChannel', {
    value: function (label, options = {}) {
      const state = rtcPeerPrivate.get(this);
      if (state.closed)
        throw platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError');
      state.hasDataChannel = true;
      return new RTCDataChannel(hostToken, label, options);
    },
    writable: true,
    configurable: true,
  });
  const rtcRandomBytes = (n) => Uint8Array.from(host.internalRandomBytes(n));
  const rtcHex = (bytes, separator = '') =>
    Array.from(bytes, (x) => x.toString(16).padStart(2, '0').toUpperCase()).join(separator);
  const rtcBase64 = (bytes) => btoa(String.fromCharCode(...bytes));
  Object.defineProperty(RTCPeerConnection.prototype, 'setLocalDescription', {
    value: function (description) {
      const state = rtcPeerPrivate.get(this);
      if (state.closed)
        return platformPromiseReject(
          platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError'),
        );
      const value =
        description instanceof RTCSessionDescription
          ? description
          : new RTCSessionDescription(description || rtcSessionModel.offer(this));
      const observable = rtcPeerStates.get(this);
      return new platformPromise((resolve, reject) =>
        setTimeout(() => {
          if (state.closed) {
            reject(platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError'));
            return;
          }
          observable.localDescription = value;
          observable.pendingLocalDescription = value;
          rtcSessionModel.applyLocal(this, value);
          if (value.type === 'offer') observable.signalingState = 'have-local-offer';
          else if (value.type === 'answer') observable.signalingState = 'stable';
          dispatchTrusted(this, new Event('signalingstatechange'));
          resolve();
          if (!/^m=/m.test(value.sdp)) return;
          setTimeout(
            () => {
              if (state.closed) return;
              observable.iceGatheringState = 'gathering';
              dispatchTrusted(this, new Event('icegatheringstatechange'));
              const environment = host.rtcEnvironment(),
                hasSTUN = state.configuration.iceServers.some((server) =>
                  server.urls.some((url) => /^stuns?:/i.test(url)),
                ),
                count = hasSTUN ? Math.max(1, Number(environment.hostCandidateCount) || 1) : 1,
                reflexiveCount = hasSTUN
                  ? Math.max(0, Number(environment.reflexiveCandidateCount) || 0)
                  : 0,
                networkCost = Math.max(0, Number(environment.networkCost) || 0),
                id = host.internalRandomUUID(),
                hostFoundation = String(parseInt(rtcHex(rtcRandomBytes(4)), 16) >>> 0),
                reflexiveFoundation = String(parseInt(rtcHex(rtcRandomBytes(4)), 16) >>> 0),
                priority = 2113937151,
                reflexivePriority = 1677729535,
                basePort = 49152 + (parseInt(rtcHex(rtcRandomBytes(2)), 16) % 16380),
                hostCandidates = [],
                reflexiveCandidates = [];
              for (let index = 0; index < count; index++) {
                const port = basePort + Number((environment.portOffsets || [])[index] ?? index * 2),
                  suffix =
                    ' generation 0 ufrag ' +
                    String(state.iceUfrag || '') +
                    ' network-cost ' +
                    networkCost;
                hostCandidates.push(
                  new RTCIceCandidate({
                    candidate:
                      'candidate:' +
                      hostFoundation +
                      ' 1 udp ' +
                      priority +
                      ' ' +
                      id +
                      '.local ' +
                      port +
                      ' typ host' +
                      suffix,
                    sdpMid: '0',
                    sdpMLineIndex: 0,
                    usernameFragment: state.iceUfrag || null,
                  }),
                );
              }
              if (hasSTUN && environment.publicAddress)
                for (let index = 0; index < reflexiveCount; index++) {
                  const port =
                      basePort +
                      Number(
                        (environment.reflexivePortOffsets || environment.portOffsets || [])[
                          index
                        ] ?? index * 2,
                      ),
                    suffix =
                      ' generation 0 ufrag ' +
                      String(state.iceUfrag || '') +
                      ' network-cost ' +
                      networkCost;
                  reflexiveCandidates.push(
                    new RTCIceCandidate({
                      candidate:
                        'candidate:' +
                        reflexiveFoundation +
                        ' 1 udp ' +
                        reflexivePriority +
                        ' ' +
                        String(environment.publicAddress) +
                        ' ' +
                        port +
                        ' typ srflx raddr 0.0.0.0 rport 0' +
                        suffix,
                      sdpMid: '0',
                      sdpMLineIndex: 0,
                      usernameFragment: state.iceUfrag || null,
                    }),
                  );
                }
              rtcSessionModel.expandCandidates(value.sdp, hostCandidates, state.configuration);
              rtcSessionModel.expandCandidates(value.sdp, reflexiveCandidates, state.configuration);
              const withCandidates = (candidates) =>
                rtcSessionModel.candidateDescription(value, candidates);
              const hostDescription = withCandidates(hostCandidates);
              observable.localDescription = hostDescription;
              observable.pendingLocalDescription = hostDescription;
              for (const candidate of hostCandidates)
                dispatchTrusted(this, new RTCPeerConnectionIceEvent('icecandidate', { candidate }));
              setTimeout(
                () => {
                  if (state.closed) return;
                  const allCandidates = hostCandidates.concat(reflexiveCandidates),
                    reflexiveDescription = withCandidates(allCandidates);
                  observable.localDescription = reflexiveDescription;
                  observable.pendingLocalDescription = reflexiveDescription;
                  for (const candidate of reflexiveCandidates)
                    dispatchTrusted(
                      this,
                      new RTCPeerConnectionIceEvent('icecandidate', { candidate }),
                    );
                  setTimeout(
                    () => {
                      if (state.closed) return;
                      observable.iceGatheringState = 'complete';
                      dispatchTrusted(this, new Event('icegatheringstatechange'));
                      dispatchTrusted(
                        this,
                        new RTCPeerConnectionIceEvent('icecandidate', { candidate: null }),
                      );
                    },
                    Math.max(
                      0,
                      Number(environment.endDelayMillis) - Number(environment.reflexiveDelayMillis),
                    ),
                  );
                },
                Math.max(
                  0,
                  Number(environment.reflexiveDelayMillis) - Number(environment.hostDelayMillis),
                ),
              );
            },
            Math.max(0, Number(host.rtcEnvironment().hostDelayMillis) || 0),
          );
        }, 0),
      );
    },
    writable: true,
    configurable: true,
  });
  globalThis.RTCDataChannel = RTCDataChannel;
  Object.defineProperty(RTCDataChannel.prototype, Symbol.toStringTag, {
    value: 'RTCDataChannel',
    configurable: true,
  });
  const targetAbsentProperties = new Set(['Document.namespaces', 'Element<script>.crossorigin']);
  // Traps depend on the interface label, never on one particular wrapper.
  // Share their code and qualified property names; still check support on every
  // access so own properties and prototype changes retain their tracing semantics.
  const observationHandlers = new Map();
  const observationHandler = (name) => {
    let handler = observationHandlers.get(name);
    if (handler) return handler;
    const names = new Map(),
      qualified = (p) => {
        let value = names.get(p);
        if (value === undefined) {
          value = name + '.' + p;
          if (names.size < 128) names.set(p, value);
        }
        return value;
      };
    handler = {
      get(t, p, r) {
        if (typeof p === 'string' && !p.startsWith('_') && (p !== 'then' || Reflect.has(t, p))) {
          const key = qualified(p);
          recordAPIAccess(key, Reflect.has(t, p) || targetAbsentProperties.has(key));
        }
        return Reflect.get(t, p, r);
      },
      set(t, p, v, r) {
        if (typeof p === 'string' && !p.startsWith('_')) {
          const key = qualified(p);
          recordAPIAccess(key, Reflect.has(t, p) || targetAbsentProperties.has(key));
        }
        return Reflect.set(t, p, v, r);
      },
    };
    observationHandlers.set(name, handler);
    return handler;
  };
  const nativeObserverCallbacks = new Map();
  const createNativeObserved = (name) => {
    let callback = nativeObserverCallbacks.get(name);
    if (!callback) {
      callback = (object, property, write, supported) => {
        if (
          typeof property === 'string' &&
          !property.startsWith('_') &&
          (write || property !== 'then' || supported)
        ) {
          const key = name + '.' + property;
          recordAPIAccess(key, supported || targetAbsentProperties.has(key));
        }
      };
      nativeObserverCallbacks.set(name, callback);
    }
    return host.createObservedObject(callback);
  };
  const observe = (name, target) => {
    let proxy;
    if (typeof host.createObservedObject === 'function') {
      // The native observer falls through before the property's own operation.
      // Data and properties live on this one object; no Proxy receiver or
      // independently synchronized target survives into the public API.
      proxy = createNativeObserved(name);
      Object.setPrototypeOf(proxy, Object.getPrototypeOf(target));
      Object.defineProperties(proxy, Object.getOwnPropertyDescriptors(target));
    } else {
      proxy = new Proxy(target, observationHandler(name));
    }
    const binding = bindingGet(target);
    if (binding) bindingSet(proxy, binding);
    if (elementData.has(target)) elementData.set(proxy, elementData.get(target));
    if (elementHandlers.has(target)) elementHandlers.set(proxy, elementHandlers.get(target));
    if (storageAreas.has(target)) storageAreas.set(proxy, storageAreas.get(target));
    if (rtcPeerStates.has(target)) rtcPeerStates.set(proxy, rtcPeerStates.get(target));
    if (rtcPeerPrivate.has(target)) rtcPeerPrivate.set(proxy, rtcPeerPrivate.get(target));
    if (rtcDataStates.has(target)) rtcDataStates.set(proxy, rtcDataStates.get(target));
    if (permissionsPolicySlots.has(target))
      permissionsPolicySlots.set(proxy, permissionsPolicySlots.get(target));
    if (trustedFactorySlots.has(target))
      trustedFactorySlots.set(proxy, trustedFactorySlots.get(target));
    return proxy;
  };
  const storageTarget = Object.create(Storage.prototype),
    sessionStorageTarget = Object.create(Storage.prototype);
  storageAreas.set(storageTarget, 'local');
  storageAreas.set(sessionStorageTarget, 'session');
  const nav = observe('Navigator', Object.create(Navigator.prototype)),
    scr = observe('Screen', Object.create(Screen.prototype)),
    loc = observe('Location', Object.create(Location.prototype)),
    histTarget = Object.create(History.prototype),
    hist = observe('History', histTarget),
    storage = observe('Storage', storageTarget),
    sessionStorage = observe('Storage', sessionStorageTarget),
    crypto = observe('Crypto', Object.create(Crypto.prototype)),
    gpu = observe('GPU', Object.create(GPU.prototype)),
    trustedTypes = observe('TrustedTypePolicyFactory', new TrustedTypePolicyFactory(hostToken)),
    documentPolicy = observe(
      'PermissionsPolicy',
      new PermissionsPolicy(hostToken, host.permissionsPolicy()),
    );
  historySlots.set(histTarget, { state: null, scrollRestoration: 'auto' });
  historySlots.set(hist, historySlots.get(histTarget));
  for (const k of [
    'userAgent',
    'appVersion',
    'platform',
    'languages',
    'language',
    'hardwareConcurrency',
    'deviceMemory',
    'onLine',
    'cookieEnabled',
    'vendor',
    'product',
    'appName',
    'maxTouchPoints',
    'webdriver',
    'pdfViewerEnabled',
  ])
    def(Navigator.prototype, k, { get: () => host.navigator()[k] });
  let uaData;
  def(Navigator.prototype, 'userAgentData', {
    get: () => uaData || (uaData = new NavigatorUAData(hostToken, host.navigator())),
  });
  const pdfPluginNames = [
    'PDF Viewer',
    'Chrome PDF Viewer',
    'Chromium PDF Viewer',
    'Microsoft Edge PDF Viewer',
    'WebKit built-in PDF',
  ];
  const pdfPlugins = pdfPluginNames.map((name) => new Plugin(hostToken, name));
  const pdfMimeTypes = [
    new MimeType(hostToken, 'application/pdf', 'pdf', 'Portable Document Format'),
    new MimeType(hostToken, 'text/pdf', 'pdf', 'Portable Document Format'),
  ];
  for (const plugin of pdfPlugins) {
    plugin.__mimes = pdfMimeTypes;
    for (const [index, mime] of pdfMimeTypes.entries()) {
      Object.defineProperty(plugin, index, { value: mime, enumerable: true });
      Object.defineProperty(plugin, mime.type, { value: mime, enumerable: false });
    }
  }
  for (const mime of pdfMimeTypes) mime.__plugin = pdfPlugins[0];
  const pluginArray = new PluginArray(hostToken, pdfPlugins),
    mimeTypeArray = new MimeTypeArray(hostToken, pdfMimeTypes);
  def(Navigator.prototype, 'plugins', { get: () => pluginArray });
  def(Navigator.prototype, 'mimeTypes', { get: () => mimeTypeArray });
  def(Navigator.prototype, 'gpu', { get: () => gpu });
  for (const k of ['width', 'height', 'availWidth', 'availHeight', 'colorDepth', 'pixelDepth'])
    def(Screen.prototype, k, { get: () => host.screen()[k] });
  let locationAncestors;
  bootstrapRestoreHooks.push(() => {
    locationAncestors = undefined;
  });
  registerRealmBinding(loc, 'Location', {
    get: (key) =>
      key === 'ancestorOrigins'
        ? (locationAncestors ??= createDOMStringList(host.locationAncestorOrigins()))
        : host.locationPart(key),
    set: (key, value) => host.setLocationPart(key, value),
    assign: (value) => host.navigate(value),
    replace: (value) => host.navigate(value, true),
    reload: () => host.navigate(host.location(), true, true),
  });
  Object.defineProperty(loc, 'valueOf', { value: Object.prototype.valueOf });
  for (const key of [
    'ancestorOrigins',
    'href',
    'origin',
    'protocol',
    'host',
    'hostname',
    'port',
    'pathname',
    'search',
    'hash',
  ]) {
    const get = Object.getOwnPropertyDescriptor(
      {
        get [key]() {
          const binding = requireRealmBinding(this, 'Location');
          return callRealmBinding(this, binding, 'get', [key]);
        },
      },
      key,
    ).get;
    const set = ['origin', 'ancestorOrigins'].includes(key)
      ? undefined
      : Object.getOwnPropertyDescriptor(
          {
            set [key](value) {
              const binding = requireRealmBinding(this, 'Location');
              value = bindingString(value);
              callRealmBinding(this, binding, 'set', [key, value]);
            },
          },
          key,
        ).set;
    markNative(get, key, 'get ');
    markNative(set, key, 'set ');
    Object.defineProperty(loc, key, { get, set, enumerable: true });
  }
  for (const key of ['assign', 'reload', 'replace', 'toString']) {
    const value = Location.prototype[key];
    markNative(value, key);
    Object.defineProperty(loc, key, { value, enumerable: true });
    delete Location.prototype[key];
  }
  Object.defineProperty(loc, Symbol.toPrimitive, { value: undefined });

  /* shared_console */
  const document = observe('Document', new HTMLDocument(hostToken));
  const documentLocationDescriptor = Object.getOwnPropertyDescriptor(
    {
      get location() {
        return documentLocation(this);
      },
      set location(value) {
        const target = documentLocation(this);
        if (target === null) throw new TypeError('Document has no browsing context');
        target.href = value;
      },
    },
    'location',
  );
  documentLocationDescriptor.configurable = false;
  markNative(documentLocationDescriptor.get, 'location', 'get ');
  markNative(documentLocationDescriptor.set, 'location', 'set ');
  Object.defineProperty(document, 'location', documentLocationDescriptor);
  let frameElementCache;
  const frameElement = () => {
    const result = host.frameElement();
    return result ? unwrapCrossRealm(result.frame, result) : null;
  };
  const remoteWindowCache = new Map(),
    remoteDocumentCache = new Map(),
    crossRealmCache = new Map(),
    crossRealmReferences = new WeakMap();
  let bridgeRealmID = host.selfRealmID();
  const bridgeApply = Reflect.apply;
  const bridgeParentNodeIntrinsic = {
    getter: Object.getOwnPropertyDescriptor(Node.prototype, 'parentNode').get,
  };
  const finalizeFrameIntrinsicBindings = () => {
    bridgeParentNodeIntrinsic.getter = Object.getOwnPropertyDescriptor(
      Node.prototype,
      'parentNode',
    ).get;
  };
  const bridgeHasOwn = Object.prototype.hasOwnProperty;
  const bridgeParse = JSON.parse,
    bridgeStringify = JSON.stringify,
    bridgeCreate = Object.create,
    bridgeKeys = Object.keys,
    bridgeSetPrototype = Object.setPrototypeOf,
    bridgeIsArray = Array.isArray;
  const bridgeOwnKeys = Reflect.ownKeys,
    bridgeDescriptor = Object.getOwnPropertyDescriptor,
    bridgeDefine = Object.defineProperty,
    bridgeExtensible = Reflect.isExtensible,
    bridgePreventExtensions = Reflect.preventExtensions,
    bridgeDelete = Reflect.deleteProperty,
    bridgeIncludes = Array.prototype.includes,
    bridgeBind = Function.prototype.bind;
  // Only private encoded records enter this codec. Null prototypes prevent
  // author toJSON hooks from observing or changing transport metadata.
  const bridgeWireRecord = (value) => {
    if (value === null || typeof value !== 'object') return value;
    const out = bridgeIsArray(value) ? bridgeSetPrototype([], null) : bridgeCreate(null),
      names = bridgeKeys(value);
    for (let i = 0; i < names.length; i++) {
      const key = names[i];
      out[key] = bridgeWireRecord(value[key]);
    }
    return out;
  };
  const frameTransaction = (id, realm, handle, operation, data = null) =>
    bridgeParse(
      host.frameTransaction(id, realm, handle, operation, bridgeStringify(bridgeWireRecord(data))),
    );
  const bridgeWeakGet = WeakMap.prototype.get,
    bridgeWeakSet = WeakMap.prototype.set,
    bridgeWeakHas = WeakMap.prototype.has;
  const referenceGet = (value) => bridgeApply(bridgeWeakGet, crossRealmReferences, [value]),
    referenceSet = (value, record) =>
      bridgeApply(bridgeWeakSet, crossRealmReferences, [value, record]),
    referenceHas = (value) => bridgeApply(bridgeWeakHas, crossRealmReferences, [value]);
  // Only native ArrayIterator.next creates a result with no preexisting user
  // aliases. Its primitive own fields remain valid until mutation or escape.
  // Keep identity/prototypes on the ordinary reference bridge; never prefetch
  // array elements or cache user iterator results.
  const freshIteratorFields = new WeakMap();
  const invalidateIteratorFields = (value) => {
    const fields = bridgeApply(bridgeWeakGet, freshIteratorFields, [value]);
    if (fields) fields.valid = false;
  };
  // Some engines reject even identical nonconfigurable accessor descriptors
  // returned by a Proxy. Detect the invariant once using bootstrap intrinsics.
  const crossRealmAccessorDescriptors = (() => {
    const describe = Reflect.getOwnPropertyDescriptor,
      define = Object.defineProperty,
      getter = () => 0,
      target = {};
    define(target, 'value', { get: getter, configurable: false });
    try {
      const descriptor = describe(
        new Proxy(target, { getOwnPropertyDescriptor: describe }),
        'value',
      );
      return descriptor.get === getter && !descriptor.configurable;
    } catch {
      return false;
    }
  })();
  const crossRealmSymbols = new Map(),
    crossRealmSymbolReferences = new Map();
  const localCrossRealmSymbols = new Map();
  let nextCrossRealmSymbolID = 0;
  const wellKnownSymbols = new Map(
    Reflect.ownKeys(Symbol)
      .filter((name) => typeof name === 'string' && typeof Symbol[name] === 'symbol')
      .map((name) => [Symbol[name], name]),
  );
  const encodeCrossRealmKey = (key) => {
    if (typeof key !== 'symbol') return { kind: 'string', value: String(key) };
    const wellKnown = wellKnownSymbols.get(key);
    if (wellKnown) return { kind: 'symbol', wellKnown };
    const global = Symbol.keyFor(key);
    if (global !== undefined) return { kind: 'symbol', global };
    if (!crossRealmSymbolReferences.has(key)) {
      const localID = ++nextCrossRealmSymbolID;
      localCrossRealmSymbols.set(localID, key);
      crossRealmSymbolReferences.set(key, {
        kind: 'symbol',
        sourceRealm: hostToken,
        localID,
        description: key.description,
      });
    }
    return crossRealmSymbolReferences.get(key);
  };
  const decodeCrossRealmKey = (key) => {
    if (key.kind === 'string') return key.value;
    if (key.realm === host.selfRealmID()) return host.frameResolve(key.handle, key.realm);
    if (key.wellKnown !== undefined) return Symbol[key.wellKnown];
    if (key.global !== undefined) return Symbol.for(key.global);
    if (key.sourceRealm === hostToken && localCrossRealmSymbols.has(key.localID))
      return localCrossRealmSymbols.get(key.localID);
    const identity =
      key.localID === undefined
        ? key.realm + ':' + key.handle
        : key.sourceRealm + ':local:' + key.localID;
    if (!crossRealmSymbols.has(identity)) {
      const value = Symbol(key.description);
      crossRealmSymbols.set(identity, value);
      crossRealmSymbolReferences.set(value, key);
    }
    return crossRealmSymbols.get(identity);
  };
  const encodeCrossRealmArgument = (value) => {
    if (value === undefined) return { kind: 'undefined' };
    if (typeof value === 'symbol') return { kind: 'symbol', key: encodeCrossRealmKey(value) };
    if (typeof value === 'bigint') return { kind: 'bigint', value: String(value) };
    if (
      typeof value === 'number' &&
      (value !== value ||
        value === Infinity ||
        value === -Infinity ||
        (value === 0 && 1 / value === -Infinity))
    )
      return {
        kind: 'special-number',
        value: value !== value ? 'NaN' : value === 0 ? '-0' : value > 0 ? 'Infinity' : '-Infinity',
      };
    if (
      (typeof value === 'object' && value !== null) ||
      typeof value === 'function' ||
      typeof value === 'undefined'
    ) {
      const reference = referenceGet(value);
      if (reference) {
        invalidateIteratorFields(value);
        return { kind: 'reference', ...reference };
      }
      const local = host.frameReference(value);
      return {
        kind: 'reference',
        frame: local.frame,
        realm: local.realm,
        handle: local.handle,
        type: local.__mimicCrossRealm,
        array: local.array,
        constructable: local.constructable,
      };
    }
    return { kind: 'value', value };
  };
  const bridgeArguments = (args) => {
    const encoded = bridgeSetPrototype([], null);
    for (let i = 0; i < args.length; i++) encoded[i] = encodeCrossRealmArgument(args[i]);
    return encoded;
  };
  // Documents are canonical realm-owned objects. Only WindowProxy follows the
  // current document after navigation; saved Document references keep their owner.
  const bridgeAccess = (id, realm) => {
    if (!host.frameCanAccess(id, realm))
      throw platformDOMException('Blocked cross-origin frame access', 'SecurityError');
  };
  const remoteDocument = (id, nullable = false) => {
    if (nullable && !host.frameCanAccess(id)) return null;
    bridgeAccess(id);
    return unwrapCrossRealm(id, host.frameGlobalGet(id, { kind: 'string', value: 'document' }));
  };
  const unwrapCrossRealm = (id, result) => {
    if (result && result.frame !== undefined) id = result.frame;
    const kind = result && result.__mimicCrossRealm;
    if (kind === 'undefined') return undefined;
    if (kind === 'null') return null;
    if (kind === 'value') return result.value;
    if (kind === 'symbol') return decodeCrossRealmKey(result);
    if (kind === 'bigint') return BigInt(result.value);
    if (kind === 'special-number')
      return result.value === 'NaN'
        ? NaN
        : result.value === '-0'
          ? -0
          : result.value === 'Infinity'
            ? Infinity
            : -Infinity;
    if (kind === 'window') return remoteWindow(result.frame);
    if (kind !== 'object' && kind !== 'function' && kind !== 'undetectable') return result;
    if (result.realm === bridgeRealmID) return host.frameResolve(result.handle, result.realm);
    // A node owned by this document already has one wrapper in this realm.
    // Borrowing a getter from another Window must not replace that identity.
    if (
      result.nodeId &&
      host.nodeOwnerDocument(result.nodeId) === realmDocumentRootID &&
      (result.nodeId === realmDocumentRootID || elementWrappers.has(String(result.nodeId)))
    )
      return wrap(result.nodeId);
    const key = id + ':' + String(result.realm || '') + ':' + result.handle;
    if (crossRealmCache.has(key)) return crossRealmCache.get(key);
    const iteratorFields = result.iteratorResult
      ? { valid: true, fields: result.iteratorResult }
      : null;
    // Bound callables have no nonconfigurable prototype/caller/arguments of
    // their own. Remote descriptors supply those properties when they exist.
    // Import scaffolding is private. Site instrumentation of reflection or
    // bind must not observe it or recursively import its own temporary values.
    const target =
      kind === 'function'
        ? result.constructable
          ? bridgeApply(bridgeBind, function () {}, [null])
          : () => {}
        : result.array
          ? []
          : bridgeCreate(null);
    for (const name of bridgeOwnKeys(target))
      if (bridgeDescriptor(target, name).configurable) delete target[name];
    const unsupported = (operation) => {
      host.semanticMissingAt('surface.js:513', 'CrossRealm.' + operation);
      throw platformDOMException(
        'Cross-realm ' + operation + ' is not implemented',
        'NotSupportedError',
      );
    };
    // The remote owner is authoritative. A Proxy target only materializes the
    // prototype and descriptors required by ECMAScript's integrity invariants;
    // no getters are invoked and no ordinary property values are cached.
    const synchronizeIntegrityTarget = () => {
      const names = frameTransaction(id, result.realm, result.handle, 'keys').map(
        decodeCrossRealmKey,
      );
      for (const name of bridgeOwnKeys(target)) {
        if (!bridgeApply(bridgeIncludes, names, [name])) bridgeDelete(target, name);
      }
      bridgeSetPrototype(
        target,
        unwrapCrossRealm(id, frameTransaction(id, result.realm, result.handle, 'prototype')),
      );
      for (const name of names) {
        const descriptor = traps.getOwnPropertyDescriptor(target, name);
        if (descriptor) bridgeDefine(target, name, descriptor);
      }
      return bridgePreventExtensions(target);
    };
    const traps = {
      get(_target, property) {
        bridgeAccess(id, result.realm);
        if (
          iteratorFields?.valid &&
          (property === 'done' || property === 'value') &&
          bridgeApply(bridgeHasOwn, iteratorFields.fields, [property])
        )
          return unwrapCrossRealm(id, iteratorFields.fields[property]);
        return unwrapCrossRealm(
          id,
          frameTransaction(id, result.realm, result.handle, 'get', encodeCrossRealmKey(property)),
        );
      },
      set(_target, property, value) {
        if (iteratorFields) iteratorFields.valid = false;
        bridgeAccess(id, result.realm);
        const outcome = frameTransaction(id, result.realm, result.handle, 'set', [
            encodeCrossRealmKey(property),
            encodeCrossRealmArgument(value),
          ]),
          answer = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw answer;
        if (answer && !bridgeExtensible(target)) traps.getOwnPropertyDescriptor(target, property);
        return answer;
      },
      has(_target, property) {
        bridgeAccess(id, result.realm);
        return frameTransaction(
          id,
          result.realm,
          result.handle,
          'has',
          encodeCrossRealmKey(property),
        );
      },
      apply(_target, receiver, args) {
        bridgeAccess(id, result.realm);
        if (result.intrinsicCall && typeof receiver === 'function') {
          return bridgeApply(receiver, args.length ? args[0] : undefined, args.slice(1));
        }
        // A borrowed native Node getter observes the receiver's canonical DOM
        // state. For a local wrapper, invoke that getter in its owning realm.
        if (result.intrinsicParentNode && !referenceHas(receiver) && elementSlot(receiver)) {
          return bridgeApply(bridgeParentNodeIntrinsic.getter, receiver, []);
        }
        // A native Window operation keeps the callable's owner identity but
        // takes its incumbent document and transferable arguments from the
        // invoking realm. Do not first turn message data into remote wrappers.
        if (result.binding?.kind === 'WindowPostMessage') {
          const target =
            receiver == null
              ? id
              : receiver === window
                ? host.selfFrameID()
                : referenceGet(receiver)?.type === 'window'
                  ? referenceGet(receiver).frame
                  : null;
          if (target === null) throw new TypeError('Illegal invocation');
          return postToFrame(target, args);
        }
        if (result.eval) {
          if (!host.frameEvalAllowed(id)) return undefined;
          if (!nativeEvalSource) {
            const value = args[0],
              source =
                typeof value === 'string'
                  ? value
                  : trustedTypeOf(value) === 'TrustedScript'
                    ? trustedSource(value, 'TrustedScript')
                    : undefined;
            if (source === undefined) {
              host.frameEval(id, undefined, result.realm);
              return value;
            }
            return unwrapCrossRealm(id, host.frameEval(id, source, result.realm));
          }
        }
        const outcome = frameTransaction(id, result.realm, result.handle, 'apply', [
            encodeCrossRealmArgument(receiver),
            bridgeArguments(args),
            !!result.iteratorNext,
          ]),
          value = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw value;
        return value;
      },
      construct(_target, args, newTarget) {
        bridgeAccess(id, result.realm);
        const reference = referenceGet(newTarget);
        if (!reference || reference.frame !== id || reference.realm !== result.realm)
          return unsupported('constructNewTarget');
        const encoded = bridgeArguments(args);
        const outcome = frameTransaction(id, result.realm, result.handle, 'construct', [
            encodeCrossRealmArgument(newTarget),
            encoded,
          ]),
          value = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw value;
        return value;
      },
      defineProperty(_target, property, descriptor) {
        if (iteratorFields) iteratorFields.valid = false;
        bridgeAccess(id, result.realm);
        const outcome = frameTransaction(id, result.realm, result.handle, 'mutateDefine', [
            encodeCrossRealmKey(property),
            encodeCrossRealmArgument(descriptor),
          ]),
          value = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw value;
        // Materialize only the descriptor required by the local Proxy invariant.
        // The owner remains authoritative for configurable properties.
        if (
          value &&
          (!bridgeExtensible(target) ||
            descriptor.configurable === false ||
            descriptor.writable === false)
        )
          traps.getOwnPropertyDescriptor(target, property);
        return value;
      },
      deleteProperty(_target, property) {
        if (iteratorFields) iteratorFields.valid = false;
        bridgeAccess(id, result.realm);
        const outcome = frameTransaction(id, result.realm, result.handle, 'mutateDelete', [
            encodeCrossRealmKey(property),
            encodeCrossRealmArgument(undefined),
          ]),
          value = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw value;
        if (value) bridgeDelete(target, property);
        return value;
      },
      preventExtensions() {
        if (iteratorFields) iteratorFields.valid = false;
        if (result.binding?.unpreventable) {
          bridgeAccess(id, result.realm);
          return false;
        }
        bridgeAccess(id, result.realm);
        const outcome = frameTransaction(id, result.realm, result.handle, 'preventExtensions'),
          value = unwrapCrossRealm(id, outcome.value);
        if (outcome.threw) throw value;
        return value ? synchronizeIntegrityTarget() : false;
      },
      isExtensible() {
        bridgeAccess(id, result.realm);
        const value = frameTransaction(id, result.realm, result.handle, 'extensible');
        if (!value) synchronizeIntegrityTarget();
        return value;
      },
      setPrototypeOf() {
        if (iteratorFields) iteratorFields.valid = false;
        return unsupported('setPrototypeOf');
      },
      getPrototypeOf() {
        bridgeAccess(id, result.realm);
        return unwrapCrossRealm(id, frameTransaction(id, result.realm, result.handle, 'prototype'));
      },
      ownKeys() {
        bridgeAccess(id, result.realm);
        if (!bridgeExtensible(target)) synchronizeIntegrityTarget();
        return frameTransaction(id, result.realm, result.handle, 'keys').map(decodeCrossRealmKey);
      },
      getOwnPropertyDescriptor(_target, property) {
        bridgeAccess(id, result.realm);
        const raw = frameTransaction(
          id,
          result.realm,
          result.handle,
          'descriptor',
          encodeCrossRealmKey(property),
        );
        if (!raw || !raw.exists) {
          bridgeDelete(target, property);
          return undefined;
        }
        if (raw.accessor && !raw.configurable && !crossRealmAccessorDescriptors)
          return unsupported('nonconfigurableAccessorDescriptor');
        const descriptor = { enumerable: !!raw.enumerable, configurable: !!raw.configurable };
        if (raw.accessor) {
          descriptor.get = unwrapCrossRealm(id, raw.get);
          descriptor.set = unwrapCrossRealm(id, raw.set);
        } else {
          descriptor.value = unwrapCrossRealm(id, raw.value);
          descriptor.writable = !!raw.writable;
        }
        // Reporting a remote nonconfigurable property requires the same
        // descriptor on the local proxy target (ECMAScript proxy invariants).
        if (!descriptor.configurable || !bridgeExtensible(target))
          bridgeDefine(target, property, descriptor);
        return descriptor;
      },
    };
    // A Proxy cannot preserve [[IsHTMLDDA]]. Import an engine-native
    // undetectable object while retaining the original realm's identity.
    const proxy =
      kind === 'undetectable'
        ? host.createUndetectable({
            __proto__: null,
            call(args, construct, receiver) {
              if (construct) throw new TypeError('Illegal constructor');
              return traps.apply(target, receiver, args);
            },
            get(property) {
              return traps.has(target, property) ? [true, traps.get(target, property)] : [false];
            },
            set(property, value) {
              return [true, traps.set(target, property, value)];
            },
            deleteProperty(property) {
              return [true, traps.deleteProperty(target, property)];
            },
            defineProperty(property, descriptor) {
              return [true, traps.defineProperty(target, property, descriptor)];
            },
            getOwnPropertyDescriptor(property) {
              const descriptor = traps.getOwnPropertyDescriptor(target, property);
              return descriptor === undefined ? [false] : [true, descriptor];
            },
            ownKeys() {
              return traps.ownKeys();
            },
          })
        : new Proxy(target, traps);
    if (iteratorFields) bridgeApply(bridgeWeakSet, freshIteratorFields, [proxy, iteratorFields]);
    if (result.eval) markNative(proxy, 'eval');
    // Platform source identity belongs to the owning realm. Transport only
    // explicit private binding metadata; never inspect author callable fields.
    if (result.nativeName !== undefined) markNative(proxy, result.nativeName);
    else if (result.functionSource !== undefined)
      markForeignFunctionSource(proxy, result.functionSource);
    if (result.nodeId) {
      const data = host.nodeData(result.nodeId);
      if (data) {
        elementData.set(proxy, data);
        if (!elementWrappers.has(String(result.nodeId)))
          elementWrappers.set(String(result.nodeId), proxy);
      }
    }
    referenceSet(proxy, {
      frame: id,
      realm: result.realm,
      handle: result.handle,
      type: kind,
      array: result.array,
      constructable: result.constructable,
      nodeId: result.nodeId,
      document: result.document,
      eval: result.eval,
      binding: result.binding,
      nativeName: result.nativeName,
    });
    crossRealmCache.set(key, proxy);
    if (result.eventState)
      eventSlots.set(proxy, unwrapCrossRealm(result.eventState.frame, result.eventState));
    if (result.document) {
      remoteDocumentCache.set(result.realm, proxy);
      if (result.nodeId) documentWrappers.set(result.nodeId, proxy);
    }
    // Native undetectable objects cannot intercept [[GetPrototypeOf]]. This
    // preserves the initial remote prototype identity, but later replacement
    // of the owner's prototype is not reflected by this bridge wrapper.
    if (kind === 'undetectable') Object.setPrototypeOf(proxy, traps.getPrototypeOf());
    return proxy;
  };
  const bridgeOriginalEval = eval;
  let bridgeOriginalPostMessage;
  registerBootstrapCallback(
    'installFrameReferenceBridge',
    (encoded) => unwrapCrossRealm(encoded.frame, encoded),
    (value) => {
      if (
        value === null ||
        value === undefined ||
        (typeof value !== 'object' && typeof value !== 'function' && typeof value !== 'undefined')
      )
        return null;
      const reference = referenceGet(value);
      if (reference) {
        invalidateIteratorFields(value);
        return { ...reference, __mimicCrossRealm: reference.type };
      }
      return null;
    },
    (value, importNode = false) =>
      importNode
        ? wrap(host.nodeData(value))
        : value === document
          ? host.documentRootID()
          : bridgeApply(bridgeWeakGet, elementData, [value])?.nodeId || 0,
    (key) => {
      const value = Reflect.get(globalThis, key);
      return {
        value,
        intrinsic:
          (key === 'eval' && value === bridgeOriginalEval) ||
          (key === 'postMessage' && value === bridgeOriginalPostMessage),
      };
    },
    (value) => {
      const binding = bindingGet(value);
      return binding
        ? { kind: binding.kind, invoke: binding.invoke, unpreventable: binding.unpreventable }
        : null;
    },
    nativeFunctionNameGet,
    (value) => functionSourceApply(engineFunctionToString, value, []),
    (value) => bridgeApply(bridgeWeakGet, eventSlots, [value]) || null,
    bridgeParentNodeIntrinsic,
  );
  const postToFrame = (id, args) => {
    if (args.length === 0)
      throw new TypeError(
        "Failed to execute 'postMessage' on 'Window': 1 argument required, but only 0 present.",
      );
    const message = args[0];
    let targetOrigin = args[1] === undefined ? '/' : args[1],
      transfer = args[2] === undefined ? [] : args[2];
    if (targetOrigin && typeof targetOrigin === 'object') {
      transfer = targetOrigin.transfer || [];
      targetOrigin = targetOrigin.targetOrigin === undefined ? '/' : targetOrigin.targetOrigin;
    }
    return host.framePost(
      id,
      cloneCodec.encode(message),
      bindingString(targetOrigin),
      takeMessagePorts(transfer),
    );
  };
  const remoteWindow = (id) => {
    if (id == null) return null;
    if (id === host.selfFrameID()) return globalThis;
    if (remoteWindowCache.has(id)) return remoteWindowCache.get(id);
    host.retainWindowReference(id);
    let proxy;
    const framePost = new Proxy({ postMessage(message) {} }.postMessage, {
      apply(_target, _this, args) {
        return postToFrame(id, args);
      },
    });
    markNative(framePost, 'postMessage');
    const target = { postMessage: framePost };
    Object.defineProperty(target, Symbol.toStringTag, { value: 'Window', configurable: true });
    const crossKeys = [
      'window',
      'self',
      'location',
      'closed',
      'frames',
      'length',
      'top',
      'opener',
      'parent',
      'blur',
      'close',
      'focus',
      'postMessage',
      'then',
      Symbol.toStringTag,
      Symbol.hasInstance,
      Symbol.isConcatSpreadable,
    ];
    const read = (p) => {
      if (p === 'window' || p === 'self') return proxy;
      if (p === 'document') return remoteDocument(id);
      if (p === 'parent' || p === 'top') return remoteWindow(host.frameRelation(id, p));
      const accessible = host.frameCanAccess(id);
      if (!accessible) {
        if (
          p === 'then' ||
          p === Symbol.toStringTag ||
          p === Symbol.hasInstance ||
          p === Symbol.isConcatSpreadable
        )
          return undefined;
        if (p === 'location')
          return Object.freeze({
            get href() {
              bridgeAccess(id);
              return host.frameLocation(id);
            },
            toString() {
              return this.href;
            },
          });
      }
      if (p !== 'postMessage' && !accessible)
        throw platformDOMException('Blocked cross-origin frame access', 'SecurityError');
      const encoded = host.frameGlobalGet(id, encodeCrossRealmKey(p));
      if (encoded.intrinsic && p === 'postMessage' && !accessible) return framePost;
      const value = unwrapCrossRealm(id, encoded);
      if (typeof p === 'string') recordAPIAccess('WindowProxy.' + p, value !== undefined);
      return value;
    };
    const describe = (p) => {
      if (!host.frameCanAccess(id)) {
        if (
          p === 'then' ||
          p === Symbol.toStringTag ||
          p === Symbol.hasInstance ||
          p === Symbol.isConcatSpreadable
        )
          return { value: undefined, writable: false, enumerable: false, configurable: true };
        bridgeAccess(id);
      }
      const raw = host.frameGlobalReflect(id, 'descriptor', encodeCrossRealmKey(p));
      if (!raw || !raw.exists) return undefined;
      const descriptor = { enumerable: !!raw.enumerable, configurable: !!raw.configurable };
      if (raw.accessor) {
        descriptor.get = unwrapCrossRealm(id, raw.get);
        descriptor.set = unwrapCrossRealm(id, raw.set);
      } else {
        descriptor.value = unwrapCrossRealm(id, raw.value);
        descriptor.writable = !!raw.writable;
      }
      return descriptor;
    };
    const keys = () =>
      host.frameCanAccess(id)
        ? host.frameGlobalReflect(id, 'keys').map(decodeCrossRealmKey)
        : crossKeys.slice();
    const define = (p, d) => {
      bridgeAccess(id);
      const raw = {};
      for (const k of ['enumerable', 'configurable', 'writable']) if (k in d) raw[k] = d[k];
      for (const k of ['get', 'set', 'value']) if (k in d) raw[k] = encodeCrossRealmArgument(d[k]);
      return host.frameGlobalReflect(id, 'define', encodeCrossRealmKey(p), raw);
    };
    const remove = (p) => {
      bridgeAccess(id);
      return host.frameGlobalReflect(id, 'delete', encodeCrossRealmKey(p));
    };
    const prototype = () =>
      host.frameCanAccess(id)
        ? unwrapCrossRealm(id, host.frameGlobalReflect(id, 'prototype'))
        : null;
    const write = (p, v) => {
      bridgeAccess(id);
      return unwrapCrossRealm(
        id,
        host.frameGlobalSet(id, encodeCrossRealmKey(p), encodeCrossRealmArgument(v)),
      );
    };
    const writeReceiver = (p, v, receiver) => {
      if (receiver === proxy) return write(p, v);
      let d = describe(p),
        parent;
      if (!d) {
        parent = prototype();
        while (!d && parent !== null) {
          d = Reflect.getOwnPropertyDescriptor(parent, p);
          parent = Reflect.getPrototypeOf(parent);
        }
      }
      if (d && !('value' in d)) {
        if (!d.set) return false;
        Reflect.apply(d.set, receiver, [v]);
        return true;
      }
      if (
        (d && !d.writable) ||
        receiver === null ||
        (typeof receiver !== 'object' && typeof receiver !== 'function')
      )
        return false;
      const own = Reflect.getOwnPropertyDescriptor(receiver, p);
      if (own) {
        if (!('value' in own) || !own.writable) return false;
        return Reflect.defineProperty(receiver, p, { value: v });
      }
      return Reflect.defineProperty(receiver, p, {
        value: v,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    };
    if (host.createWindowObject) {
      // A native exotic object can replace nonconfigurable descriptors after
      // navigation. A JS Proxy target cannot do this without invariant errors.
      const native = host.createWindowObject({
        __proto__: null,
        get: (p) => [true, read(p)],
        set: (p, v) => [true, write(p, v)],
        getOwnPropertyDescriptor: (p) => {
          const d = describe(p);
          return d === undefined ? [false] : [true, d];
        },
        ownKeys: keys,
        defineProperty: (p, d) => [true, define(p, d)],
        deleteProperty: (p) => [true, remove(p)],
      });
      // Let direct writes reach the native setter with its own receiver. The
      // outer Proxy's default [[Set]] would instead query data attributes and
      // reject accessor properties before invoking their setters.
      proxy = new Proxy(native, {
        set: (_t, p, v, receiver) => writeReceiver(p, v, receiver),
        getPrototypeOf: prototype,
        preventExtensions: () => false,
        setPrototypeOf: (_t, next) => next === prototype(),
        has: (_t, p) => {
          if (!host.frameCanAccess(id)) {
            if (crossKeys.includes(p)) return true;
            bridgeAccess(id);
          }
          return unwrapCrossRealm(id, host.frameGlobalHas(id, encodeCrossRealmKey(p)));
        },
      });
    } else {
      proxy = new Proxy(target, {
        get: (_t, p) => read(p),
        set: (_t, p, v, receiver) => writeReceiver(p, v, receiver),
        has: (_t, p) => {
          bridgeAccess(id);
          return unwrapCrossRealm(id, host.frameGlobalHas(id, encodeCrossRealmKey(p)));
        },
        ownKeys: keys,
        getPrototypeOf: prototype,
        preventExtensions: () => false,
        setPrototypeOf: (_t, next) => next === prototype(),
        getOwnPropertyDescriptor: (_t, p) => {
          const d = describe(p);
          if (d && !d.configurable) Object.defineProperty(target, p, d);
          return d;
        },
        defineProperty: (_t, p, d) => define(p, d),
        deleteProperty: (_t, p) => remove(p),
      });
    }
    referenceSet(proxy, { frame: id, type: 'window' });
    remoteWindowCache.set(id, proxy);
    return proxy;
  };
  const closeEventSlots = new WeakMap();
  class CloseEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      closeEventSlots.set(this, {
        wasClean: !!init.wasClean,
        code: Number(init.code || 0),
        reason: String(init.reason || ''),
      });
    }
    get wasClean() {
      return closeEventSlots.get(this).wasClean;
    }
    get code() {
      return closeEventSlots.get(this).code;
    }
    get reason() {
      return closeEventSlots.get(this).reason;
    }
  }
  const webSocketSlots = new WeakMap();
  let nextWebSocketID = 0;
  class WebSocket extends EventTarget {
    constructor(url, protocols = []) {
      super();
      if (arguments.length === 0)
        throw new TypeError(
          "Failed to construct 'WebSocket': 1 argument required, but only 0 present.",
        );
      let parsed;
      try {
        parsed = new URL(String(url), location.href);
      } catch (_) {
        throw platformDOMException(`The URL '${String(url)}' is invalid.`, 'SyntaxError');
      }
      if ((parsed.protocol !== 'ws:' && parsed.protocol !== 'wss:') || parsed.hash)
        throw platformDOMException(
          `The URL's scheme must be either 'ws' or 'wss'. '${parsed.protocol.replace(':', '')}' is not allowed.`,
          'SyntaxError',
        );
      if (parsed.username || parsed.password)
        throw platformDOMException(
          'The URL contains a username and password, which is not allowed.',
          'SyntaxError',
        );
      if (location.protocol === 'https:' && parsed.protocol === 'ws:')
        throw platformDOMException(
          'An insecure WebSocket connection may not be initiated from a page loaded over HTTPS.',
          'SecurityError',
        );
      const list = typeof protocols === 'string' ? [protocols] : Array.from(protocols);
      const seen = new Set();
      for (const protocol of list) {
        const value = String(protocol);
        if (!value || /[^!#$%&'*+.^_`|~0-9A-Za-z-]/.test(value) || seen.has(value))
          throw platformDOMException(`The subprotocol '${value}' is invalid.`, 'SyntaxError');
        seen.add(value);
      }
      const state = {
        id: `websocket-${++nextWebSocketID}`,
        url:
          parsed.pathname === ''
            ? parsed.href.replace(/([?#]|$)/, (suffix) => `/${suffix}`)
            : parsed.href,
        readyState: 0,
        bufferedAmount: 0,
        extensions: '',
        protocol: '',
        binaryType: 'blob',
      };
      webSocketSlots.set(this, state);
      const admitted = host.openWebSocket(
        (event) => {
          if (event.type === 'open') {
            if (state.readyState !== 0) return;
            state.readyState = 1;
            state.protocol = String(event.protocol || '');
            dispatchTrusted(this, new Event('open'));
          } else if (event.type === 'message') {
            if (state.readyState !== 1) return;
            let data = event.data;
            if (event.binary) {
              const bytes = new Uint8Array(event.bytes || []);
              data = state.binaryType === 'arraybuffer' ? bytes.buffer : new Blob([bytes]);
            }
            dispatchTrusted(this, new MessageEvent('message', { data, origin: parsed.origin }));
          } else if (event.type === 'error') {
            state.readyState = 3;
            dispatchTrusted(this, new Event('error'));
          } else if (event.type === 'close') {
            if (state.closeDispatched) return;
            state.closeDispatched = true;
            state.readyState = 3;
            dispatchTrusted(
              this,
              new CloseEvent('close', {
                code: event.code,
                reason: event.reason,
                wasClean: event.wasClean,
              }),
            );
          }
        },
        state.id,
        state.url,
        list,
      );
      if (admitted === false) state.readyState = 3;
    }
    send(data) {
      const state = webSocketSlots.get(this);
      if (!state) throw new TypeError('Illegal invocation');
      if (state.readyState === 0)
        throw platformDOMException('Still in CONNECTING state.', 'InvalidStateError');
      if (state.readyState !== 1) return;
      if (data instanceof ArrayBuffer) host.sendWebSocket(state.id, new Uint8Array(data), true);
      else if (ArrayBuffer.isView(data))
        host.sendWebSocket(
          state.id,
          new Uint8Array(data.buffer, data.byteOffset, data.byteLength),
          true,
        );
      else if (data instanceof Blob) {
        data.arrayBuffer().then((buffer) => {
          if (state.readyState === 1) host.sendWebSocket(state.id, new Uint8Array(buffer), true);
        });
      } else host.sendWebSocket(state.id, String(data), false);
    }
    close(code = undefined, reason = '') {
      const state = webSocketSlots.get(this);
      if (!state) throw new TypeError('Illegal invocation');
      if (code !== undefined && code !== 1000 && (Number(code) < 3000 || Number(code) > 4999))
        throw platformDOMException(
          `The code must be either 1000, or between 3000 and 4999.`,
          'InvalidAccessError',
        );
      reason = String(reason);
      if (new TextEncoder().encode(reason).byteLength > 123)
        throw platformDOMException(
          'The message must not be greater than 123 bytes.',
          'SyntaxError',
        );
      if (state.readyState === 2 || state.readyState === 3) return;
      state.readyState = 2;
      host.closeWebSocket(state.id, code === undefined ? 1000 : Number(code), reason);
    }
  }
  for (const key of ['url', 'readyState', 'bufferedAmount', 'extensions', 'protocol'])
    def(WebSocket.prototype, key, {
      get() {
        const state = webSocketSlots.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        return state[key];
      },
    });
  def(WebSocket.prototype, 'binaryType', {
    get() {
      const state = webSocketSlots.get(this);
      if (!state) throw new TypeError('Illegal invocation');
      return state.binaryType;
    },
    set(value) {
      const state = webSocketSlots.get(this);
      if (!state) throw new TypeError('Illegal invocation');
      value = String(value);
      if (value === 'blob' || value === 'arraybuffer') state.binaryType = value;
    },
  });
  for (const type of ['open', 'message', 'error', 'close'])
    def(WebSocket.prototype, `on${type}`, {
      get() {
        return eventHandlerRecord(this, type).value;
      },
      set(value) {
        setEventHandlerValue(this, type, value);
      },
    });
  for (const [name, value] of Object.entries({ CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 })) {
    Object.defineProperty(WebSocket, name, { value, enumerable: true });
    Object.defineProperty(WebSocket.prototype, name, { value, enumerable: true });
  }
  let nextXHRID = 0;
  const xhrListenerMapForEach = Map.prototype.forEach;
  const xhrSlots = new WeakMap(),
    xhrEventTargetSlots = new WeakMap(),
    xhrState = (xhr) => {
      const state = xhrSlots.get(xhr);
      if (!state) throw new TypeError('Illegal invocation');
      return state;
    },
    xhrEventTargetState = (target) => {
      const state = xhrEventTargetSlots.get(target);
      if (!state) throw new TypeError('Illegal invocation');
      return state;
    };
  const fireXHREvent = (xhr, type) => dispatchTrusted(xhr, new Event(type)),
    setXHRState = (xhr, value) => {
      xhrState(xhr).readyState = value;
      fireXHREvent(xhr, 'readystatechange');
    };
  // Keep the WebIDL intermediate interface in the implementation's constructor
  // chain too. Its public constructor stays illegal; only XHR initializes it.
  const xhrConstructionToken = {};
  class XMLHttpRequestEventTarget extends EventTarget {
    constructor(token) {
      if (token !== xhrConstructionToken) throw new TypeError('Illegal constructor');
      super();
      xhrEventTargetSlots.set(this, {
        onloadstart: null,
        onprogress: null,
        onabort: null,
        onerror: null,
        onload: null,
        ontimeout: null,
        onloadend: null,
      });
    }
  }
  class XMLHttpRequestUpload extends XMLHttpRequestEventTarget {
    constructor(token) {
      if (token !== xhrConstructionToken)
        throw new TypeError("Failed to construct 'XMLHttpRequestUpload': Illegal constructor");
      super(token);
    }
  }
  class XMLHttpRequest extends XMLHttpRequestEventTarget {
    constructor() {
      super(xhrConstructionToken);
      xhrSlots.set(this, {
        readyState: 0,
        status: 0,
        statusText: '',
        responseText: '',
        response: null,
        responseURL: '',
        responseType: '',
        timeout: 0,
        withCredentials: false,
        onreadystatechange: null,
        upload: new XMLHttpRequestUpload(xhrConstructionToken),
        requestHeaders: {},
        requestHeaderOrder: [],
        responseHeaders: {},
        sent: false,
        method: 'GET',
        url: '',
        async: true,
        requestID: '',
        generation: 0,
      });
    }
    open(method, url, async = true, user, password) {
      method = String(method).toUpperCase();
      if (!method || /[^A-Z-]/.test(method))
        throw platformDOMException('Invalid HTTP method', 'SyntaxError');
      const state = xhrState(this);
      if (state.sent && typeof host.abortXHR === 'function') host.abortXHR(state.requestID);
      state.generation++;
      state.method = method;
      state.url = String(url);
      state.async = Boolean(async);
      state.sent = false;
      state.requestHeaders = {};
      state.requestHeaderOrder = [];
      state.status = 0;
      state.statusText = '';
      state.responseText = '';
      state.response = null;
      state.responseURL = '';
      state.responseHeaders = {};
      setXHRState(this, 1);
    }
    setRequestHeader(name, value) {
      const state = xhrState(this);
      if (state.readyState !== 1 || state.sent)
        throw platformDOMException("The object's state must be OPENED.", 'InvalidStateError');
      name = String(name).trim().toLowerCase();
      value = String(value).trim();
      if (!name || /[^!#$%&'*+.^_`|~0-9a-z-]/i.test(name) || /[\0\r\n]/.test(value))
        throw platformDOMException('Invalid HTTP header', 'SyntaxError');
      if (state.requestHeaders[name] === undefined) state.requestHeaderOrder.push(name);
      state.requestHeaders[name] = state.requestHeaders[name]
        ? state.requestHeaders[name] + ', ' + value
        : value;
    }
    getResponseHeader(name) {
      const state = xhrState(this);
      if (state.readyState < 2) return null;
      const value = state.responseHeaders[String(name).toLowerCase()];
      return value === undefined ? null : value;
    }
    getAllResponseHeaders() {
      const state = xhrState(this);
      if (state.readyState < 2) return '';
      return Object.keys(state.responseHeaders)
        .sort()
        .map((k) => k + ': ' + state.responseHeaders[k] + '\r\n')
        .join('');
    }
    send(body = null) {
      const state = xhrState(this);
      if (state.readyState !== 1 || state.sent)
        throw platformDOMException("The object's state must be OPENED.", 'InvalidStateError');
      state.sent = true;
      const generation = ++state.generation,
        requestID = (state.requestID = 'xhr-' + ++nextXHRID);
      let uploadListenerFlag = false;
      // Read the canonical event registry without calling author-replaced
      // collection methods while a platform operation inspects its listeners.
      bridgeApply(
        xhrListenerMapForEach,
        bridgeApply(bridgeWeakGet, eventListeners, [state.upload]),
        [
          (listeners) => {
            if (listeners.length) uploadListenerFlag = true;
          },
        ],
      );
      host.xhr(
        (result) => {
          if (state.generation !== generation || !state.sent) return;
          if (result.error) {
            state.sent = false;
            state.status = 0;
            setXHRState(this, 4);
            fireXHREvent(this, result.error);
            fireXHREvent(this, 'loadend');
            return;
          }
          state.status = result.status;
          state.statusText = result.statusText;
          state.responseURL = result.responseURL;
          state.responseHeaders = result.responseHeaders || {};
          setXHRState(this, 2);
          if (state.generation !== generation) return;
          const bytes = new Uint8Array(result.responseBytes || []);
          state.responseText =
            result.responseText === undefined
              ? new TextDecoder().decode(bytes)
              : result.responseText;
          switch (state.responseType) {
            case 'json':
              try {
                state.response = JSON.parse(state.responseText);
              } catch (_) {
                state.response = null;
              }
              break;
            case 'arraybuffer':
              state.response = bytes.slice().buffer;
              break;
            case 'blob':
              state.response = new Blob([bytes], {
                type: state.responseHeaders['content-type'] || '',
              });
              break;
            default:
              state.response = state.responseText;
          }
          setXHRState(this, 3);
          if (state.generation !== generation) return;
          state.sent = false;
          setXHRState(this, 4);
          fireXHREvent(this, 'load');
          fireXHREvent(this, 'loadend');
        },
        state.method,
        state.url,
        state.requestHeaders,
        body == null ? '' : String(body),
        Number(state.timeout) || 0,
        state.requestHeaderOrder,
        Boolean(state.withCredentials),
        body != null,
        requestID,
        uploadListenerFlag,
      );
    }
    abort() {
      const state = xhrState(this);
      const active =
        (state.readyState === 1 && state.sent) || state.readyState === 2 || state.readyState === 3;
      if (state.sent && typeof host.abortXHR === 'function') host.abortXHR(state.requestID);
      state.generation++;
      state.sent = false;
      state.status = 0;
      state.statusText = '';
      state.responseText = '';
      state.response = null;
      state.responseURL = '';
      state.responseHeaders = {};
      if (active) {
        setXHRState(this, 4);
        state.readyState = 0;
        fireXHREvent(this, 'abort');
        fireXHREvent(this, 'loadend');
      } else state.readyState = 0;
    }
  }
  for (const key of [
    'readyState',
    'status',
    'statusText',
    'responseURL',
    'timeout',
    'withCredentials',
    'onreadystatechange',
  ])
    def(XMLHttpRequest.prototype, key, {
      get() {
        return xhrState(this)[key];
      },
      set(value) {
        xhrState(this)[key] = value;
      },
    });
  // XHR and its stable upload object have independent event-handler slots on
  // the shared intermediate interface, not copies of request state.
  for (const key of [
    'onloadstart',
    'onprogress',
    'onload',
    'onerror',
    'onabort',
    'ontimeout',
    'onloadend',
  ])
    def(XMLHttpRequestEventTarget.prototype, key, {
      get() {
        return xhrEventTargetState(this)[key];
      },
      set(value) {
        xhrEventTargetState(this)[key] = value;
      },
    });
  def(XMLHttpRequest.prototype, 'upload', {
    get() {
      return xhrState(this).upload;
    },
  });
  def(XMLHttpRequest.prototype, 'responseType', {
    get() {
      return xhrState(this).responseType;
    },
    set(value) {
      const state = xhrState(this),
        type = String(value);
      if (!['', 'text', 'json', 'arraybuffer', 'blob'].includes(type))
        throw new TypeError('Invalid XMLHttpRequest responseType');
      if (state.readyState === 3 || state.readyState === 4)
        throw platformDOMException("The object's state must be OPENED.", 'InvalidStateError');
      state.responseType = type;
    },
  });
  def(XMLHttpRequest.prototype, 'responseText', {
    get() {
      const state = xhrState(this);
      if (state.responseType !== '' && state.responseType !== 'text')
        throw platformDOMException(
          "The value is only accessible if the object's 'responseType' is '' or 'text'.",
          'InvalidStateError',
        );
      return state.readyState === 3 || state.readyState === 4 ? state.responseText : '';
    },
  });
  def(XMLHttpRequest.prototype, 'response', {
    get() {
      const state = xhrState(this);
      if (state.responseType === '' || state.responseType === 'text')
        return state.readyState === 3 || state.readyState === 4 ? state.responseText : '';
      return state.readyState === 4 ? state.response : null;
    },
  });
  const requestSlots = new WeakMap(),
    responseSlots = new WeakMap();
  class Request {
    constructor(input, init = {}) {
      const previous = input instanceof Request ? requestSlots.get(input) : null,
        method = String(init.method || previous?.method || 'GET').toUpperCase(),
        raw = input instanceof Request ? previous.url : String(input),
        url = host.urlParts(raw).href;
      requestSlots.set(this, {
        url,
        method,
        headers: new Headers(init.headers || previous?.headers),
        mode: String(init.mode || previous?.mode || 'cors'),
        credentials: String(init.credentials || previous?.credentials || 'same-origin'),
        cache: String(init.cache || previous?.cache || 'default'),
        redirect: String(init.redirect || previous?.redirect || 'follow'),
        referrer:
          init.referrer === undefined
            ? previous?.referrer || 'about:client'
            : String(init.referrer),
        body: init.body ?? null,
        bodyUsed: false,
      });
    }
  }
  for (const key of [
    'url',
    'method',
    'headers',
    'mode',
    'credentials',
    'cache',
    'redirect',
    'referrer',
    'body',
    'bodyUsed',
  ])
    def(Request.prototype, key, {
      get() {
        const state = requestSlots.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        return state[key];
      },
    });
  Object.defineProperty(Request.prototype, Symbol.toStringTag, {
    value: 'Request',
    configurable: true,
  });
  class Response {
    constructor(body = null, init = {}) {
      const status = init.status === undefined ? 200 : Number(init.status);
      if (status !== 0 && (status < 200 || status > 599))
        throw new RangeError('Invalid status code');
      responseSlots.set(this, {
        body,
        headers: new Headers(init.headers),
        status,
        statusText: String(init.statusText || ''),
        type: 'default',
        url: '',
        redirected: false,
        bodyUsed: false,
      });
    }
    get ok() {
      const status = responseSlots.get(this).status;
      return status >= 200 && status <= 299;
    }
    text() {
      const state = responseSlots.get(this);
      state.bodyUsed = true;
      return platformPromiseResolve(state.body == null ? '' : String(state.body));
    }
    json() {
      return this.text().then(JSON.parse);
    }
  }
  for (const key of [
    'body',
    'headers',
    'status',
    'statusText',
    'type',
    'url',
    'redirected',
    'bodyUsed',
  ])
    def(Response.prototype, key, {
      get() {
        const state = responseSlots.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        return state[key];
      },
    });
  Object.defineProperty(Response.prototype, Symbol.toStringTag, {
    value: 'Response',
    configurable: true,
  });
  // Legacy named constructors are distinct functions with the element
  // interface prototype, rather than aliases whose function name mutates the
  // corresponding HTML*Element constructor.
  globalThis.Image = function Image(width, height) {
    const element = document.createElement('img');
    if (width !== undefined) element.width = Number(width);
    if (height !== undefined) element.height = Number(height);
    return element;
  };
  globalThis.Audio = function Audio(src) {
    const element = document.createElement('audio');
    element.setAttribute('preload', 'auto');
    if (src !== undefined) element.src = String(src);
    return element;
  };
  globalThis.Option = function Option(text = '', value, defaultSelected = false, selected = false) {
    const element = document.createElement('option');
    element.text = String(text);
    if (value !== undefined) element.value = String(value);
    element.defaultSelected = Boolean(defaultSelected);
    element.selected = Boolean(selected);
    return element;
  };
  Object.defineProperty(globalThis, 'FormData', {
    value: FormData,
    writable: true,
    configurable: true,
  });
  installFileReader();
  const window = globalThis;
  window.addEventListener = (...a) => EventTarget.prototype.addEventListener.apply(window, a);
  window.removeEventListener = (...a) => EventTarget.prototype.removeEventListener.apply(window, a);
  window.dispatchEvent = (...a) => EventTarget.prototype.dispatchEvent.apply(window, a);
  window.onmessage = null;
  window.onerror = null;
  const NodeFilter = {
    NodeFilter() {
      throw new TypeError('Illegal constructor');
    },
  }.NodeFilter;
  for (const [name, value] of Object.entries({
    FILTER_ACCEPT: 1,
    FILTER_REJECT: 2,
    FILTER_SKIP: 3,
    SHOW_ALL: 0xffffffff,
    SHOW_ELEMENT: 1,
    SHOW_ATTRIBUTE: 2,
    SHOW_TEXT: 4,
    SHOW_CDATA_SECTION: 8,
    SHOW_ENTITY_REFERENCE: 16,
    SHOW_ENTITY: 32,
    SHOW_PROCESSING_INSTRUCTION: 64,
    SHOW_COMMENT: 128,
    SHOW_DOCUMENT: 256,
    SHOW_DOCUMENT_TYPE: 512,
    SHOW_DOCUMENT_FRAGMENT: 1024,
    SHOW_NOTATION: 2048,
  }))
    Object.defineProperty(NodeFilter, name, { value, enumerable: true });
  Object.assign(window, {
    window: null,
    self: null,
    top: null,
    parent: null,
    document,
    navigator: nav,
    screen: scr,
    location: loc,
    history: hist,
    localStorage: storage,
    sessionStorage,
    crypto,
    trustedTypes,
    console: consoleObject,
    atob,
    btoa,
    TextEncoder,
    Headers,
    Request,
    Response,
    Event,
    MessageEvent,
    ErrorEvent,
    CloseEvent,
    EventTarget,
    Node,
    DocumentFragment,
    ShadowRoot,
    Element,
    HTMLElement,
    DOMStringMap,
    SVGElement,
    HTMLScriptElement,
    HTMLImageElement,
    HTMLMediaElement,
    HTMLAudioElement,
    HTMLVideoElement,
    HTMLIFrameElement,
    HTMLAnchorElement,
    HTMLCollection,
    NodeList,
    DOMTokenList,
    CSSStyleDeclaration,
    Crypto,
    DOMException,
    PermissionsPolicy,
    FeaturePolicy,
    TrustedHTML,
    TrustedScript,
    TrustedScriptURL,
    TrustedTypePolicy,
    TrustedTypePolicyFactory,
    URL,
    URLSearchParams,
    ReadableStream,
    ReadableStreamDefaultReader,
    ReadableStreamDefaultController,
    WritableStream,
    WritableStreamDefaultWriter,
    TransformStream,
    TransformStreamDefaultController,
    Blob,
    File,
    FileReader,
    Worker,
    Document,
    HTMLDocument,
    Navigator,
    NavigatorUAData,
    Screen,
    Location,
    History,
    Storage,
    XMLHttpRequestEventTarget,
    XMLHttpRequestUpload,
    XMLHttpRequest,
    WebSocket,
    GPU,
    GPUAdapter,
    GPUAdapterInfo,
    GPUSupportedFeatures,
    GPUSupportedLimits,
    GPUDevice,
    RTCPeerConnection,
    RTCSessionDescription,
    RTCIceCandidate,
    PerformanceEntry,
    PerformanceServerTiming,
    PerformanceResourceTiming,
    PerformanceNavigationTiming,
    NodeFilter,
  });
  let security = host.documentSecurity();
  for (const [name, key] of Object.entries({
    isSecureContext: 'secureContext',
    crossOriginIsolated: 'crossOriginIsolated',
    credentialless: 'credentialless',
    originAgentCluster: 'originAgentCluster',
  }))
    Object.defineProperty(window, name, {
      get: () => security[key],
      enumerable: true,
      configurable: true,
    });
  let windowRelations = host.windowRelations();
  window.window = window;
  window.self = window;
  let windowTop, windowParent;
  // Top stays an unforgeable accessor after exposure normalization. Parent is
  // replaceable: assignment creates an own data property, as in Chrome.
  let installedWindowFrameCount = 0;
  const syncWindowFrames = () => {
    const ids = host.windowFrames();
    for (let index = installedWindowFrameCount; index < ids.length; index++)
      Object.defineProperty(window, String(index), {
        get() {
          const current = host.windowFrames();
          return index < current.length ? remoteWindow(current[index]) : undefined;
        },
        enumerable: true,
        configurable: true,
      });
    for (let index = ids.length; index < installedWindowFrameCount; index++)
      delete window[String(index)];
    installedWindowFrameCount = ids.length;
    return ids;
  };
  globalThis.__mimicSyncWindowFrames = syncWindowFrames;
  const replaceableWindowProperty = (name) =>
    function (value) {
      Object.defineProperty(this, name, {
        value,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    };
  const getWindowFrames = () => {
      syncWindowFrames();
      return window;
    },
    getWindowLength = () => syncWindowFrames().length;
  for (const [fn, name, prefix] of [
    [getWindowFrames, 'frames', 'get '],
    [getWindowLength, 'length', 'get '],
  ]) {
    Object.defineProperty(fn, 'name', { value: prefix + name, configurable: true });
    markNative(fn, name, prefix);
  }
  Object.defineProperties(window, {
    frames: {
      get: getWindowFrames,
      set: replaceableWindowProperty('frames'),
      enumerable: true,
      configurable: true,
    },
    length: {
      get: getWindowLength,
      set: replaceableWindowProperty('length'),
      enumerable: true,
      configurable: true,
    },
  });
  // A profile seed may be captured first in a child realm. Keep only its
  // relation IDs during bootstrap: native WindowProxy instances cannot be
  // recorded as JSON or embedded in the pure-JS snapshot. Materialize through
  // the canonical cache when script first reads the live relation.
  const getWindowTop = () =>
    host.documentActive()
      ? windowTop === undefined
        ? (windowTop = remoteWindow(windowRelations.top))
        : windowTop
      : null;
  const getWindowParent = () =>
    host.documentActive()
      ? windowParent === undefined
        ? (windowParent = remoteWindow(windowRelations.parent))
        : windowParent
      : null;
  const setWindowParent = {
    set(value) {
      Object.defineProperty(this, 'parent', {
        value,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    },
  }.set;
  for (const [fn, name, prefix] of [
    [getWindowTop, 'top', 'get '],
    [getWindowParent, 'parent', 'get '],
    [setWindowParent, 'parent', 'set '],
  ]) {
    Object.defineProperty(fn, 'name', { value: prefix + name, configurable: true });
    markNative(fn, name, prefix);
  }
  Object.defineProperties(window, {
    top: { get: getWindowTop, enumerable: true, configurable: true },
    parent: { get: getWindowParent, set: setWindowParent, enumerable: true, configurable: true },
  });
  const originWindowID = (value) => {
    if (value === window) return '';
    for (const [id, proxy] of remoteWindowCache)
      if (value === proxy) {
        bridgeAccess(id);
        return id;
      }
    throw new TypeError('Illegal invocation');
  };
  const originDescriptor = Object.getOwnPropertyDescriptor(
    {
      get origin() {
        return host.windowOrigin(originWindowID(this));
      },
      set origin(value) {
        originWindowID(this);
        Object.defineProperty(this, 'origin', {
          value,
          writable: true,
          enumerable: true,
          configurable: true,
        });
      },
    },
    'origin',
  );
  markNative(originDescriptor.get, 'origin', 'get ');
  markNative(originDescriptor.set, 'origin', 'set ');
  Object.defineProperty(window, 'origin', {
    ...originDescriptor,
    enumerable: true,
    configurable: true,
  });
  window.__receiveFrameMessage = (data, origin, sourceId) =>
    dispatchNative(
      window,
      new MessageEvent('message', {
        data: cloneCodec.decode(data),
        origin,
        source: remoteWindow(sourceId),
      }),
    );
  globalThis.__receiveFrameMessage = window.__receiveFrameMessage;
  Object.assign(window, {
    CharacterData,
    Text,
    Comment,
    SVGSVGElement,
    SVGRect,
    HTMLLinkElement,
    HTMLMetaElement,
    IntersectionObserver,
    IntersectionObserverEntry,
    BroadcastChannel,
    CryptoKey,
    SubtleCrypto,
    PerformanceMark,
    PerformanceTiming,
  });
  window.__receiveFrameMessage = (data, origin, sourceId, portIds = []) =>
    dispatchNative(
      window,
      new MessageEvent('message', {
        data: cloneCodec.decode(data),
        origin,
        source: remoteWindow(sourceId),
        ports: Array.from(portIds, wrapBrowserMessagePort),
      }),
    );
  globalThis.__receiveFrameMessage = window.__receiveFrameMessage;
  Object.assign(window, { MessagePort, MessageChannel, DOMRectReadOnly, DOMRect, DOMMatrix });
  Object.assign(window, { MessagePort: BrowserMessagePort, MessageChannel: BrowserMessageChannel });
  const chromeLoadTimes = function loadTimes() {
    return {
      requestTime: performance.timeOrigin / 1000,
      startLoadTime: performance.timeOrigin / 1000,
      commitLoadTime: performance.timeOrigin / 1000,
      finishDocumentLoadTime: 0,
      finishLoadTime: 0,
      firstPaintTime: 0,
      firstPaintAfterLoadTime: 0,
      navigationType: 'Other',
      wasFetchedViaSpdy: true,
      wasNpnNegotiated: true,
      npnNegotiatedProtocol: 'h2',
      wasAlternateProtocolAvailable: false,
      connectionInfo: 'h2',
    };
  };
  const chromeCSI = function csi() {
    const values = host.legacyChromeCSI();
    return { startE: values[0], onloadT: values[1], pageT: values[2], tran: values[3] };
  };
  const chromeApp = {
    isInstalled: false,
    getDetails: function getDetails() {
      return null;
    },
    getIsInstalled: function getIsInstalled() {
      return false;
    },
    installState: function installState(callback) {
      if (typeof callback === 'function') callback('not_installed');
    },
    runningState: function runningState() {
      return 'cannot_run';
    },
    InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
    RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' },
  };
  window.chrome = { loadTimes: chromeLoadTimes, csi: chromeCSI, app: chromeApp };
  // Browsing-context strings survive same-context document replacement. The
  // WindowProxy reads the owning realm's accessors rather than publication storage.
  for (const name of ['name', 'status'])
    def(window, name, {
      get: () => host.windowState(name),
      set: (value) => host.windowState(name, String(value)),
    });
  const replaceableWindow = (name, get) =>
    def(window, name, {
      get,
      set(value) {
        Object.defineProperty(this, name, {
          value,
          writable: true,
          enumerable: true,
          configurable: true,
        });
      },
    });
  replaceableWindow('closed', () => host.windowState('closed'));
  replaceableWindow('offscreenBuffering', () => true);
  def(window, 'opener', {
    get: () => {
      const id = host.auxiliaryOpener();
      return id ? remoteWindow(id) : null;
    },
    set(value) {
      Object.defineProperty(this, 'opener', {
        value,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    },
  });
  for (const [name, key] of [
    ['screenX', 'screenX'],
    ['screenLeft', 'screenX'],
    ['screenY', 'screenY'],
    ['screenTop', 'screenY'],
  ])
    replaceableWindow(name, () => host.viewport()[key]);
  for (const [name, key] of [
    ['innerWidth', 'width'],
    ['innerHeight', 'height'],
    ['outerWidth', 'outerWidth'],
    ['outerHeight', 'outerHeight'],
  ])
    replaceableWindow(name, () => host.viewport()[key]);
  replaceableWindow('devicePixelRatio', () => host.screen().devicePixelRatio);
  let windowScrollX = 0,
    windowScrollY = 0;
  for (const name of ['scrollX', 'pageXOffset'])
    replaceableWindow(name, () => compatibilityScrolling?.position(null).x ?? windowScrollX);
  for (const name of ['scrollY', 'pageYOffset'])
    replaceableWindow(name, () => compatibilityScrolling?.position(null).y ?? windowScrollY);
  const timerHandler = (handler, name) =>
    typeof handler === 'function'
      ? handler
      : trustedConvert(
          handler,
          'TrustedScript',
          'Window ' + name,
          "Failed to execute '" + name + "' on 'Window': ",
        );
  window.setTimeout = function setTimeout(handler, timeout = 0, ...args) {
    handler = trustedTimerArgument(handler);
    const delay = webIDLNumber(timeout);
    return host.setTimer(
      timerHandler(handler, 'setTimeout'),
      delay,
      false,
      reportWindowException,
      args,
    );
  };
  window.setInterval = function setInterval(handler, timeout = 0, ...args) {
    handler = trustedTimerArgument(handler);
    const delay = webIDLNumber(timeout);
    return host.setTimer(
      timerHandler(handler, 'setInterval'),
      delay,
      true,
      reportWindowException,
      args,
    );
  };
  window.clearTimeout = function clearTimeout(id) {
    return host.clearTimer(webIDLNumber(id));
  };
  window.clearInterval = function clearInterval(id) {
    return host.clearTimer(webIDLNumber(id));
  };
  window.requestAnimationFrame = function requestAnimationFrame(callback) {
    return requestRenderingFrame(callback);
  };
  window.cancelAnimationFrame = function cancelAnimationFrame(id) {
    cancelRenderingFrame(id);
  };
  window.postMessage = bridgeOriginalPostMessage = {
    postMessage(message) {
      const receiver = this == null ? window : this,
        reference = receiver === window ? null : referenceGet(receiver);
      if (receiver !== window && reference?.type !== 'window')
        throw new TypeError('Illegal invocation');
      return postToFrame(receiver === window ? host.selfFrameID() : reference.frame, arguments);
    },
  }.postMessage;
  registerRealmBinding(bridgeOriginalPostMessage, 'WindowPostMessage', {});
  window.queueMicrotask = function queueMicrotask(callback) {
    if (typeof callback !== 'function') throw new TypeError('callback is not a function');
    platformPromiseResolve().then(() => {
      if (host.executionContextActive()) callback();
    });
  };
  window.fetch = function fetch(input, init = {}) {
    const request = input instanceof Request ? new Request(input, init) : new Request(input, init);
    return host
      .fetch(
        request.url,
        request.method,
        Object.fromEntries(request.headers),
        request.body == null ? '' : String(request.body),
      )
      .then((r) => {
        const response = new Response(r.body, { status: r.status, headers: r.headers });
        const state = responseSlots.get(response);
        state.url = r.url;
        return response;
      });
  };
  window.performance = installPerformanceObject(Object.create(Performance.prototype));
  window.DOMStringList = DOMStringList;
  window.getComputedStyle = (e, pseudo = null) => {
    if (elementSlot(e)?.type !== 'element')
      throw new TypeError(
        "Failed to execute 'getComputedStyle' on 'Window': parameter 1 is not of type 'Element'.",
      );
    pseudo = pseudo == null ? '' : String(pseudo);
    if (/^::?(?:before|after)$/.test(pseudo)) pseudo = pseudo.replace(/^::?/, '');
    return cssDeclaration(e, true, pseudo);
  };
  window.matchMedia = (q) => ({
    matches: cssMediaMatches(String(q)),
    media: String(q),
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
  });
  // Constructor discovery is reflection, not a read of every Window getter.
  // In a cold child, reading top/parent here would instantiate native proxies
  // before bootstrap capture ends and could traverse a foreign realm.
  const handwrittenInterfaceConstructors = new Set(
    Object.getOwnPropertyNames(globalThis)
      .map((name) => Object.getOwnPropertyDescriptor(globalThis, name)?.value)
      .filter((value) => typeof value === 'function'),
  );
  const attributeUnsafeInterfaces = globalThis.__mimicAttributeUnsafeInterfaces;
  delete globalThis.__mimicAttributeUnsafeInterfaces;
  let known;
  let pendingCallableExposure;
  // Decode immutable transport into realm-local descriptor data. Legacy object
  // inputs from Surface keep their existing schema.
  const decodeExposure = (exposure) => {
    if (!Array.isArray(exposure)) return exposure;
    const row = (p) => ({
      name: p[0],
      enumerable: !!(p[1] & 1),
      configurable: !!(p[1] & 2),
      writable: p[1] >> 4 === 0 ? null : p[1] >> 4 === 2,
      getter: !!(p[1] & 4),
      setter: !!(p[1] & 8),
      valueType: p[2],
      functionName: p[3],
      functionLength: p[4],
    });
    return {
      propertyOrder: exposure[0],
      prototypeOrder: exposure[3],
      interfaceOrder: exposure[4],
      properties: exposure[1] === null ? null : exposure[1].map(row),
      prototypes:
        exposure[2] === null
          ? null
          : Object.fromEntries(
              Object.entries(exposure[2]).map(([name, rows]) => [
                name,
                rows === null ? null : rows.map(row),
              ]),
            ),
    };
  };
  const applyTargetExposure = (input) => {
    const exposure = decodeExposure(input);
    pendingCallableExposure = exposure;
    for (const [interfaceName, members] of Object.entries(exposure.prototypes || {})) {
      if (!members) continue;
      for (const member of members) {
        const record = generatedLazyOperationIndex.get(interfaceName + '\0' + member.name);
        if (!record || member.valueType !== 'function') continue;
        record.functionName = member.functionName || member.name;
        if (member.functionLength !== null && member.functionLength !== undefined)
          record.length = member.functionLength;
      }
    }
    const properties = exposure.properties || [];
    const publicationMissing = new Set();
    // Publish the frozen profile's static globals in their observed order once,
    // before unforgeable descriptors are installed. Later user additions and
    // delete/reinsert operations retain the engine's ordinary key ordering.
    if (exposure.propertyOrder && exposure.propertyOrder.length) {
      const descriptors = new Map(),
        define = Object.defineProperty;
      let republish = false;
      for (const name of exposure.propertyOrder) {
        if (!engineGlobals.has(name)) republish = true;
        if (!republish) continue;
        const descriptor =
          Object.getOwnPropertyDescriptor(globalThis, name) ||
          (name in globalThis
            ? { value: globalThis[name], writable: true, configurable: true }
            : undefined);
        if (descriptor && !descriptor.configurable)
          throw new Error('non-configurable global publication: ' + name);
        descriptors.set(name, descriptor);
        delete globalThis[name];
      }
      for (const [name, descriptor] of descriptors) {
        if (snapshotPublication && snapshotPublication.lateEngineKeys.has(name)) continue;
        if (!descriptor) publicationMissing.add(name);
        define(
          globalThis,
          name,
          descriptor || { value: undefined, writable: true, configurable: true },
        );
      }
    }
    const expected = new Map(properties.map((property) => [property.name, property]));
    for (const name of Object.getOwnPropertyNames(globalThis)) {
      if (
        name.startsWith('__mimic') ||
        name === '__receiveFrameMessage' ||
        name === '__receiveMessagePort'
      )
        continue;
      if (!expected.has(name)) {
        const descriptor = Object.getOwnPropertyDescriptor(globalThis, name);
        if (descriptor && descriptor.configurable) delete globalThis[name];
      }
    }
    for (const property of properties) {
      if (snapshotPublication && snapshotPublication.lateEngineKeys.has(property.name)) continue;
      const propertyName = property.name;
      const descriptor = { enumerable: property.enumerable, configurable: property.configurable };
      const current = publicationMissing.has(property.name)
        ? undefined
        : Object.getOwnPropertyDescriptor(globalThis, property.name);
      if (property.valueType === 'accessor') {
        let stored = current && 'value' in current ? current.value : undefined;
        descriptor.get =
          current && current.get
            ? current.get
            : function () {
                return stored;
              };
        if (property.setter)
          descriptor.set =
            current && current.set
              ? current.set
              : function (value) {
                  stored = value;
                };
      } else {
        let value;
        const missingFunction =
          property.valueType === 'function' &&
          current &&
          'value' in current &&
          current.value === undefined;
        if (current && 'value' in current && !missingFunction) value = current.value;
        else if (
          !missingFunction &&
          !publicationMissing.has(property.name) &&
          property.name in globalThis
        )
          value = globalThis[property.name];
        else if (property.valueType === 'function') {
          const functionName = property.functionName || property.name;
          value = {
            [functionName]: function () {
              return host.semanticMissingAt('surface.js:615', 'Window.' + propertyName);
            },
          }[functionName];
        } else if (property.valueType === 'boolean') value = false;
        else if (property.valueType === 'number') value = 0;
        else if (property.valueType === 'string') value = '';
        else value = {};
        if (typeof value === 'function') {
          if (property.functionName && value.name !== property.functionName)
            Object.defineProperty(value, 'name', {
              value: property.functionName,
              configurable: true,
            });
          if (property.functionLength !== null && value.length !== property.functionLength)
            Object.defineProperty(value, 'length', {
              value: property.functionLength,
              configurable: true,
            });
        }
        descriptor.value = value;
        descriptor.writable = property.writable !== false;
      }
      if (!current || current.configurable) {
        if (snapshotPublication && !descriptor.configurable) {
          snapshotPublication.descriptors.set(property.name, descriptor);
          Object.defineProperty(
            globalThis,
            property.name,
            Object.assign({}, descriptor, { configurable: true }),
          );
        } else Object.defineProperty(globalThis, property.name, descriptor);
      }
    }
    for (const [interfaceName, members] of Object.entries(exposure.prototypes || {})) {
      // ECMAScript intrinsics already own their engine-defined descriptors.
      // Reapplying the WebIDL prototype pass invalidates V8 species/prototype
      // guards even where the captured descriptor has the same visible shape.
      if (engineGlobals.has(interfaceName)) continue;
      const ctor = globalThis[interfaceName],
        prototype = ctor && ctor.prototype;
      if (!prototype) continue;
      for (const property of members) {
        const propertyName = property.name;
        const current = Object.getOwnPropertyDescriptor(prototype, property.name);
        if (current) {
          if (current.configurable) {
            const data = 'value' in current,
              writable = property.writable !== false;
            if (data) {
              if (typeof current.value === 'function') {
                if (property.functionName && current.value.name !== property.functionName)
                  Object.defineProperty(current.value, 'name', {
                    value: property.functionName,
                    configurable: true,
                  });
                if (
                  property.functionLength !== null &&
                  property.functionLength !== undefined &&
                  current.value.length !== property.functionLength
                )
                  Object.defineProperty(current.value, 'length', {
                    value: property.functionLength,
                    configurable: true,
                  });
              }
            }
            // Updating a function's own metadata does not require reinstalling
            // its containing property. Avoid rewriting thousands of already
            // normalized descriptors on every realm bootstrap.
            if (
              current.enumerable !== property.enumerable ||
              current.configurable !== property.configurable ||
              (data && current.writable !== writable)
            ) {
              const normalized = Object.assign({}, current, {
                enumerable: property.enumerable,
                configurable: property.configurable,
              });
              if (data) normalized.writable = writable;
              Object.defineProperty(prototype, property.name, normalized);
            }
          }
          continue;
        }
        // Handwritten classes still use JS assignment for native internal-slot
        // initialization. Installing a readonly WebIDL accessor here would
        // intercept that assignment; those attributes are added as each class
        // migrates to generated slot-backed semantics.
        if (
          property.valueType === 'accessor' &&
          handwrittenInterfaceConstructors.has(ctor) &&
          attributeUnsafeInterfaces.has(interfaceName)
        )
          continue;
        const descriptor = { enumerable: property.enumerable, configurable: property.configurable };
        const lazyDomain = lazyDomainInterfaces.get(interfaceName);
        if (property.valueType === 'accessor') {
          descriptor.get = property.getter
            ? lazyDomain
              ? function () {
                  return invokeLazyBehavior(
                    interfaceName,
                    propertyName,
                    this,
                    [],
                    undefined,
                    'get',
                  );
                }
              : function () {
                  return host.semanticMissingAt(
                    'surface.js:659',
                    interfaceName + '.' + propertyName,
                  );
                }
            : undefined;
          descriptor.set = property.setter
            ? lazyDomain
              ? function (value) {
                  return invokeLazyBehavior(
                    interfaceName,
                    propertyName,
                    this,
                    [value],
                    undefined,
                    'set',
                  );
                }
              : function () {
                  return host.semanticMissingAt(
                    'surface.js:660',
                    interfaceName + '.' + propertyName + ' setter',
                  );
                }
            : undefined;
          if (lazyDomain) {
            if (descriptor.get)
              lazyBehaviorCells.set(lazyKey(interfaceName, propertyName, 'get'), {});
            if (descriptor.set)
              lazyBehaviorCells.set(lazyKey(interfaceName, propertyName, 'set'), {});
          }
        } else {
          let value;
          if (property.name === 'constructor') value = ctor;
          else if (property.valueType === 'function') {
            const functionName = property.functionName || property.name;
            value = lazyDomain
              ? {
                  [functionName]: function (...args) {
                    return invokeLazyBehavior(
                      interfaceName,
                      propertyName,
                      this,
                      args,
                      undefined,
                      'value',
                    );
                  },
                }[functionName]
              : {
                  [functionName]: function () {
                    return host.semanticMissingAt(
                      'surface.js:666',
                      interfaceName + '.' + propertyName,
                    );
                  },
                }[functionName];
            if (lazyDomain) lazyBehaviorCells.set(lazyKey(interfaceName, propertyName), {});
            if (property.functionLength !== null && property.functionLength !== undefined)
              Object.defineProperty(value, 'length', {
                value: property.functionLength,
                configurable: true,
              });
          } else if (property.valueType === 'boolean') value = false;
          else if (property.valueType === 'number') value = 0;
          else if (property.valueType === 'string') value = '';
          else value = null;
          descriptor.value = value;
          descriptor.writable = property.writable !== false;
        }
        Object.defineProperty(prototype, property.name, descriptor);
      }
    }
    // The captured target exposure, not handwritten class placement, owns the
    // observable prototype shape. Move implemented descriptors to the exact
    // ancestor that owns them in the pinned Chrome revision, then remove IDL
    // members that are not exposed by this target/profile. Private helpers on
    // still-unmigrated implementations remain temporarily hidden by naming;
    // migrated interfaces use WeakMap-backed internal slots and expose none.
    const prototypeTargets = new Map();
    for (const [interfaceName, members] of Object.entries(exposure.prototypes || {})) {
      if (engineGlobals.has(interfaceName)) continue;
      const ctor = globalThis[interfaceName];
      if (ctor && ctor.prototype) {
        prototypeTargets.set(ctor.prototype, new Set(members.map((member) => member.name)));
        markNative(ctor, ctor.name || interfaceName);
        const prototypeDescriptor = Object.getOwnPropertyDescriptor(ctor, 'prototype');
        if (prototypeDescriptor && prototypeDescriptor.writable)
          Object.defineProperty(ctor, 'prototype', { writable: false });
        if (!Object.prototype.hasOwnProperty.call(ctor.prototype, Symbol.toStringTag))
          Object.defineProperty(ctor.prototype, Symbol.toStringTag, {
            value: interfaceName,
            configurable: true,
          });
        for (const name of Object.getOwnPropertyNames(ctor.prototype)) {
          const d = Object.getOwnPropertyDescriptor(ctor.prototype, name);
          if (name !== 'constructor') markNative(d.value, name);
          markNative(d.get, name, 'get ');
          markNative(d.set, name, 'set ');
        }
      }
    }
    for (const [prototype, expectedMembers] of prototypeTargets) {
      for (const name of Object.getOwnPropertyNames(prototype)) {
        if (expectedMembers.has(name)) continue;
        const source = Object.getOwnPropertyDescriptor(prototype, name);
        if (!source || !source.configurable) continue;
        let owner = Object.getPrototypeOf(prototype);
        while (owner && owner !== Object.prototype) {
          const ownerMembers = prototypeTargets.get(owner);
          if (ownerMembers && ownerMembers.has(name)) break;
          owner = Object.getPrototypeOf(owner);
        }
        if (owner && owner !== Object.prototype) {
          Object.defineProperty(owner, name, source);
          delete prototype[name];
        } else if (!name.startsWith('__')) {
          delete prototype[name];
        }
      }
    }
  };
  let navigatorBeaconBindings;
  const finalizeSingletonGetterBindings = () => {
    // These platform objects belong to a Window. Borrowed accessors must use
    // the receiver's owner, including across realms and after prototype edits.
    // Reuse the private owner binding used by Document, Location and Performance.
    for (const [kind, prototype, instance] of [
      ['Navigator', Navigator.prototype, nav],
      ['Screen', Screen.prototype, scr],
      ['History', History.prototype, hist],
      ['Storage', Storage.prototype, storage],
    ]) {
      const getters = new Map(),
        setters = new Map();
      for (const name of Reflect.ownKeys(prototype)) {
        const descriptor = Object.getOwnPropertyDescriptor(prototype, name);
        if (!descriptor.configurable || (!descriptor.get && !descriptor.set)) continue;
        if (descriptor.get) {
          getters.set(name, descriptor.get);
          descriptor.get = Object.getOwnPropertyDescriptor(
            {
              get [name]() {
                const binding = requireRealmBinding(this, kind);
                return callRealmBinding(this, binding, 'get', [name]);
              },
            },
            name,
          ).get;
        }
        if (descriptor.set) {
          setters.set(name, descriptor.set);
          descriptor.set = Object.getOwnPropertyDescriptor(
            {
              set [name](value) {
                const binding = requireRealmBinding(this, kind);
                if (kind === 'History' && name === 'scrollRestoration')
                  value = bindingString(value);
                callRealmBinding(this, binding, 'set', [name, value]);
              },
            },
            name,
          ).set;
        }
        Object.defineProperty(prototype, name, descriptor);
      }
      for (const owner of kind === 'Storage' ? [storage, sessionStorage] : [instance]) {
        const operations = {
          get: (name) => functionSourceApply(getters.get(name), owner, []),
          set: (name, value) => functionSourceApply(setters.get(name), owner, [value]),
        };
        if (kind === 'Navigator' && navigatorBeaconBindings)
          Object.assign(operations, navigatorBeaconBindings);
        if (kind === 'History') {
          operations.go = (n) => {
            requireActiveHistory();
            host.historyGo(n);
          };
          operations.update = (state, url, replace) => {
            const error = replace ? host.historyReplace(url, state) : host.historyPush(url, state);
            if (error) throw platformDOMException(error, 'SecurityError');
          };
        }
        if (kind === 'Storage') {
          const area = storageAreas.get(owner);
          operations.key = (i) => host.storageKey(area, i);
          operations.getItem = (k) => host.storageGet(area, k);
          operations.setItem = (k, v) => host.storageSet(area, k, v);
          operations.removeItem = (k) => host.storageRemove(area, k);
          operations.clear = () => host.storageClear(area);
        }
        registerRealmBinding(owner, kind, operations);
      }
    }
  };
  const finalizeCallableBindings = () => {
    const exposure = pendingCallableExposure;
    pendingCallableExposure = null;
    if (!exposure) return;
    const publishMemberOrder = (target, order) => {
      const current = Object.getOwnPropertyNames(target),
        descriptors = new Map(
          current.map((name) => [name, Object.getOwnPropertyDescriptor(target, name)]),
        );
      const desired = [
        ...order.filter((name) => descriptors.has(name)),
        ...current.filter((name) => !order.includes(name)),
      ];
      if (current.length === desired.length && current.every((name, i) => name === desired[i]))
        return;
      // Nonconfigurable properties cannot be moved. Preserve their entire
      // prefix and reorder only a compatible configurable suffix.
      let anchor = -1;
      for (let i = 0; i < current.length; i++)
        if (!descriptors.get(current[i]).configurable) anchor = i;
      for (let i = 0; i <= anchor; i++) if (current[i] !== desired[i]) return;
      for (let i = anchor + 1; i < current.length; i++) delete target[current[i]];
      for (let i = anchor + 1; i < desired.length; i++)
        Object.defineProperty(target, desired[i], descriptors.get(desired[i]));
    };
    // Late semantic installers replace generated functions. Reapply only the
    // captured callable metadata to existing bindings, without creating missing
    // members, changing prototype ownership or replaying global publication.
    const wrappers = new WeakMap();
    const normalize = (fn, name, length, operation) => {
      if (typeof fn !== 'function') return fn;
      if (operation && Object.prototype.hasOwnProperty.call(fn, 'prototype')) {
        let wrapper = wrappers.get(fn);
        if (!wrapper) {
          const original = fn;
          // Concise methods are callable without [[Construct]]. One wrapper per
          // original preserves aliases; captured apply preserves the receiver,
          // arguments and exception identity even after user mutation.
          wrapper = {
            [name](...args) {
              return functionSourceApply(original, this, args);
            },
          }[name];
          wrappers.set(fn, wrapper);
        }
        fn = wrapper;
      }
      if (name && fn.name !== name)
        Object.defineProperty(fn, 'name', { value: name, configurable: true });
      if (length !== null && length !== undefined && fn.length !== length)
        Object.defineProperty(fn, 'length', { value: length, configurable: true });
      return fn;
    };
    for (const property of exposure.properties || []) {
      if (engineGlobals.has(property.name)) continue;
      const descriptor = Object.getOwnPropertyDescriptor(globalThis, property.name);
      if (descriptor && typeof descriptor.value === 'function')
        normalize(descriptor.value, property.functionName, property.functionLength, false);
    }
    for (const [name, members] of Object.entries(exposure.prototypes || {})) {
      if (engineGlobals.has(name)) continue;
      const ctor = Object.getOwnPropertyDescriptor(globalThis, name)?.value,
        prototype = ctor && ctor.prototype;
      if (!prototype) continue;
      for (const member of members) {
        if (member.name === 'constructor') continue;
        const descriptor = Object.getOwnPropertyDescriptor(prototype, member.name);
        if (!descriptor || !descriptor.configurable) continue;
        const previousValue = descriptor.value,
          previousGet = descriptor.get,
          previousSet = descriptor.set;
        if (typeof descriptor.value === 'function')
          descriptor.value = normalize(
            descriptor.value,
            member.functionName || member.name,
            member.functionLength,
            true,
          );
        if (descriptor.get)
          descriptor.get = normalize(descriptor.get, 'get ' + member.name, 0, true);
        if (descriptor.set)
          descriptor.set = normalize(descriptor.set, 'set ' + member.name, 1, true);
        if (
          descriptor.value !== previousValue ||
          descriptor.get !== previousGet ||
          descriptor.set !== previousSet
        )
          Object.defineProperty(prototype, member.name, descriptor);
      }
    }
    for (const [name, order] of Object.entries(exposure.prototypeOrder || {})) {
      if (engineGlobals.has(name)) continue;
      const prototype = Object.getOwnPropertyDescriptor(globalThis, name)?.value?.prototype;
      if (prototype) publishMemberOrder(prototype, order);
    }
    for (const [name, order] of Object.entries(exposure.interfaceOrder || {})) {
      if (engineGlobals.has(name)) continue;
      const target = Object.getOwnPropertyDescriptor(globalThis, name)?.value;
      if (target) publishMemberOrder(target, order);
    }
  };
  const finalizeNativeBindings = () => {
    // WebIDL bindings are native functions from JavaScript's point of view even
    // when their semantics are implemented in JavaScript underneath. Derive this
    // representation from the installed surface so generated and handwritten
    // interfaces cannot drift apart.
    const markedPrototypes = new Set([Object.prototype, Function.prototype]);
    const operationWrappers = new Map();
    const operation = (method, name) => {
      if (typeof method !== 'function') return method;
      const native = platformOperation(
        method,
        method.name && method.name !== 'value' ? method.name : name,
      );
      if (native !== method) return native;
      let result = method;
      if (
        Object.getOwnPropertyDescriptor(method, 'prototype') ||
        Object.getPrototypeOf(method) !== Function.prototype
      ) {
        result = operationWrappers.get(method);
        if (!result) {
          result = {
            [name](...args) {
              return functionSourceApply(method, this, args);
            },
          }[name];
          Object.defineProperty(result, 'length', { value: method.length, configurable: true });
          operationWrappers.set(method, result);
        }
      }
      if (!result.name || result.name === 'value')
        Object.defineProperty(result, 'name', { value: name, configurable: true });
      return result;
    };
    const markMembers = (owner) => {
      for (const member of Reflect.ownKeys(owner)) {
        if (member === 'constructor' || member === 'prototype') continue;
        const descriptor = Object.getOwnPropertyDescriptor(owner, member);
        if (descriptor?.configurable) {
          for (const [kind, prefix] of [
            ['get', 'get '],
            ['set', 'set '],
          ]) {
            if (typeof descriptor[kind] === 'function') {
              descriptor[kind] = platformOperation(descriptor[kind], prefix + String(member));
            }
          }
          Object.defineProperty(owner, member, descriptor);
        }
        const method = operation(descriptor && descriptor.value, String(member));
        if (method !== descriptor?.value)
          Object.defineProperty(owner, member, { ...descriptor, value: method });
        // Iteration aliases can reuse intrinsic functions such as Array#values.
        // Their identity and original function name survive the alias.
        markNative(method, (typeof method === 'function' && method.name) || String(member));
        markNative(descriptor && descriptor.get, String(member), 'get ');
        markNative(descriptor && descriptor.set, String(member), 'set ');
      }
    };

    // Window operations have no interface prototype; unlike constructors they
    // must reject [[Construct]] and expose no own prototype property.
    for (const name of [
      'setTimeout',
      'setInterval',
      'clearTimeout',
      'clearInterval',
      'fetch',
      'atob',
      'btoa',
      'getComputedStyle',
      'matchMedia',
    ]) {
      const descriptor = Object.getOwnPropertyDescriptor(globalThis, name);
      if (!descriptor || typeof descriptor.value !== 'function') continue;
      const value = operation(descriptor.value, name);
      markNative(value, name);
      Object.defineProperty(globalThis, name, { ...descriptor, value });
    }
    for (const key of Reflect.ownKeys(globalThis)) {
      if (engineGlobals.has(key)) continue;
      const globalDescriptor = Object.getOwnPropertyDescriptor(globalThis, key);
      for (const [kind, prefix] of [
        ['get', 'get '],
        ['set', 'set '],
      ]) {
        const accessor = globalDescriptor?.[kind];
        if (typeof accessor !== 'function') continue;
        Object.defineProperty(accessor, 'name', {
          value: prefix + String(key),
          configurable: true,
        });
        markNative(accessor, String(key), prefix);
      }
      const value = globalDescriptor && globalDescriptor.value;
      if (typeof value !== 'function') continue;
      // A LegacyWindowAlias is another property pointing at the same interface
      // object; it must not rename that function in Function#toString.
      markNative(value, value.name || String(key));
      markMembers(value);
      let prototype = value.prototype;
      while (prototype && prototype !== Object.prototype && !markedPrototypes.has(prototype)) {
        markedPrototypes.add(prototype);
        markMembers(prototype);
        prototype = Object.getPrototypeOf(prototype);
      }
    }
  };
  const finalizeBindings = () => {
    const reflectString = (interfaceName, property, attribute = property, defaultValue = '') => {
      const ctor = globalThis[interfaceName];
      if (!ctor || !ctor.prototype) return;
      Object.defineProperty(ctor.prototype, property, {
        get() {
          const value = intrinsicGetAttribute.call(this, attribute);
          return value === null ? defaultValue : value;
        },
        set(value) {
          intrinsicSetAttribute.call(this, attribute, String(value));
        },
        enumerable: true,
        configurable: true,
      });
    };
    reflectString('HTMLScriptElement', 'type');
    reflectString('HTMLInputElement', 'name');
    if (globalThis.HTMLInputElement && globalThis.HTMLInputElement.prototype)
      Object.defineProperty(globalThis.HTMLInputElement.prototype, 'type', {
        get() {
          const value = (intrinsicGetAttribute.call(this, 'type') || 'text').toLowerCase();
          return new Set([
            'hidden',
            'text',
            'search',
            'tel',
            'url',
            'email',
            'password',
            'date',
            'month',
            'week',
            'time',
            'datetime-local',
            'number',
            'range',
            'color',
            'checkbox',
            'radio',
            'file',
            'submit',
            'image',
            'reset',
            'button',
          ]).has(value)
            ? value
            : 'text';
        },
        set(value) {
          intrinsicSetAttribute.call(this, 'type', String(value));
        },
        enumerable: true,
        configurable: true,
      });
    if (globalThis.HTMLImageElement) {
      globalThis.Image.prototype = globalThis.HTMLImageElement.prototype;
      Object.defineProperty(globalThis.HTMLImageElement.prototype, Symbol.toStringTag, {
        value: 'HTMLImageElement',
        configurable: true,
      });
    }
    if (globalThis.HTMLAudioElement)
      globalThis.Audio.prototype = globalThis.HTMLAudioElement.prototype;
    if (globalThis.HTMLOptionElement)
      globalThis.Option.prototype = globalThis.HTMLOptionElement.prototype;
    globalThis.globalThis = globalThis;
    for (const name of Object.getOwnPropertyNames(globalThis)) {
      if (engineGlobals.has(name)) continue;
      const ctor = Object.getOwnPropertyDescriptor(globalThis, name)?.value;
      if (
        typeof ctor === 'function' &&
        ctor.prototype &&
        !Object.prototype.hasOwnProperty.call(ctor.prototype, Symbol.toStringTag)
      )
        Object.defineProperty(ctor.prototype, Symbol.toStringTag, {
          value: name,
          configurable: true,
        });
    }
    for (const [name, fn] of [
      ['setTimeout', setTimeout],
      ['setInterval', setInterval],
      ['clearTimeout', clearTimeout],
      ['clearInterval', clearInterval],
      ['fetch', fetch],
      ['atob', atob],
      ['btoa', btoa],
      ['getComputedStyle', getComputedStyle],
      ['matchMedia', matchMedia],
    ])
      markNative(fn, name);
    // Window-exposed interface objects are non-enumerable data properties;
    // singleton browser objects are enumerable readonly accessors. Object.assign
    // above is only a convenient installation mechanism, not their final binding
    // descriptor.
    for (const ctor of [
      Event,
      MessageEvent,
      ErrorEvent,
      CloseEvent,
      EventTarget,
      Node,
      DocumentFragment,
      ShadowRoot,
      Element,
      HTMLElement,
      DOMStringMap,
      SVGElement,
      HTMLScriptElement,
      HTMLImageElement,
      HTMLMediaElement,
      HTMLAudioElement,
      HTMLVideoElement,
      HTMLIFrameElement,
      HTMLAnchorElement,
      HTMLCollection,
      NodeList,
      DOMTokenList,
      CSSStyleDeclaration,
      Crypto,
      DOMException,
      PermissionsPolicy,
      TrustedHTML,
      TrustedScript,
      TrustedScriptURL,
      TrustedTypePolicy,
      TrustedTypePolicyFactory,
      URL,
      URLSearchParams,
      ReadableStream,
      ReadableStreamDefaultReader,
      ReadableStreamDefaultController,
      WritableStream,
      WritableStreamDefaultWriter,
      TransformStream,
      TransformStreamDefaultController,
      Blob,
      File,
      FileReader,
      Worker,
      Document,
      HTMLDocument,
      Navigator,
      NavigatorUAData,
      Plugin,
      PluginArray,
      MimeType,
      MimeTypeArray,
      Screen,
      Location,
      History,
      Storage,
      XMLHttpRequestEventTarget,
      XMLHttpRequestUpload,
      XMLHttpRequest,
      WebSocket,
      GPU,
      GPUAdapter,
      GPUAdapterInfo,
      GPUSupportedFeatures,
      GPUSupportedLimits,
      GPUDevice,
      RTCPeerConnection,
      RTCSessionDescription,
      RTCIceCandidate,
      RTCPeerConnectionIceEvent,
      RTCDataChannel,
      Performance,
      PerformanceEntry,
      PerformanceServerTiming,
      PerformanceResourceTiming,
      PerformanceNavigationTiming,
      PerformanceObserverEntryList,
      PerformanceObserver,
    ])
      Object.defineProperty(globalThis, ctor.name, {
        value: ctor,
        writable: true,
        enumerable: false,
        configurable: true,
      });
    for (const ctor of [
      CharacterData,
      Text,
      Comment,
      SVGSVGElement,
      SVGRect,
      HTMLLinkElement,
      HTMLMetaElement,
      IntersectionObserver,
      IntersectionObserverEntry,
      BroadcastChannel,
      CryptoKey,
      SubtleCrypto,
      PerformanceMark,
      PerformanceTiming,
    ])
      Object.defineProperty(globalThis, ctor.name, {
        value: ctor,
        writable: true,
        enumerable: false,
        configurable: true,
      });
    for (const [name, getter] of [
      ['document', () => document],
      ['navigator', () => nav],
      ['screen', () => scr],
      ['location', () => loc],
      ['history', () => hist],
      ['localStorage', () => storage],
      ['sessionStorage', () => sessionStorage],
      ['crypto', () => crypto],
      ['trustedTypes', () => trustedTypes],
      ['performance', () => window.performance],
    ]) {
      const value = getter();
      Object.defineProperty(globalThis, name, {
        get: () => value,
        enumerable: true,
        configurable: true,
      });
      if (globalThis.Window && globalThis.Window.prototype)
        delete globalThis.Window.prototype[name];
    }
    if (globalThis.Window && globalThis.Window.prototype) {
      // Blink's WindowProperties is an unexposed intermediate interface. The IDL
      // generator must not publish its constructor, but Window still inherits
      // EventTarget behavior through it.
      if (!('addEventListener' in globalThis.Window.prototype))
        Object.setPrototypeOf(globalThis.Window.prototype, EventTarget.prototype);
      Object.defineProperty(globalThis.Window.prototype, Symbol.toStringTag, {
        value: 'Window',
        configurable: true,
      });
      Object.setPrototypeOf(globalThis, globalThis.Window.prototype);
      // QuickJS gives its realm global an internal "global" brand which wins
      // over an inherited tag. Blink exposes the local WindowProxy as Window.
      Object.defineProperty(globalThis, Symbol.toStringTag, {
        value: 'Window',
        configurable: true,
      });
    }
    markNative(frameElement, 'frameElement', 'get ');
    Object.defineProperty(globalThis, 'frameElement', {
      get: frameElement,
      enumerable: true,
      configurable: true,
    });
    if (globalThis.Window && globalThis.Window.prototype)
      delete globalThis.Window.prototype.frameElement;
    if (globalThis.Window && globalThis.Window.prototype) {
      const target = window;
      // The realm global is the local WindowProxy identity.  Wrapping it in a
      // second ECMAScript Proxy makes `window === globalThis` appear correct but
      // breaks dynamic code: a sloppy Function receives the engine realm global
      // as `this`, not that wrapper.  Keep one identity at the engine boundary;
      // cross-frame WindowProxy objects remain explicit remoteWindow proxies.
      for (const name of ['window', 'self', 'globalThis'])
        Object.defineProperty(target, name, {
          value: target,
          writable: true,
          enumerable: true,
          configurable: true,
        });
    }
  };
  // Called only on a fresh deserialized, pre-script realm. Seed DOM must be
  // empty: preserving user-created wrappers across a host change is invalid.
  globalThis.__mimicRestoreBootstrap = (freshHost) => {
    host = freshHost;
    hostToken = host.token();
    bridgeRealmID = host.selfRealmID();
    intlEnvironment = host.intlEnvironment();
    security = host.documentSecurity();
    const policy = permissionsPolicySlots.get(
      new PermissionsPolicy(hostToken, host.permissionsPolicy()),
    );
    Object.assign(permissionsPolicySlots.get(documentPolicy), policy);
    remoteWindowCache.clear();
    remoteDocumentCache.clear();
    crossRealmCache.clear();
    crossRealmSymbols.clear();
    crossRealmSymbolReferences.clear();
    localCrossRealmSymbols.clear();
    nextCrossRealmSymbolID = 0;
    elementWrappers.clear();
    documentWrappers.clear();
    frameElementCache = undefined;
    uaData = undefined;
    windowRelations = host.windowRelations();
    windowTop = windowParent = undefined;
    for (const restore of bootstrapRestoreHooks) restore();
    tracedAccesses.clear();
    // Conditional V8 intrinsics were absent in the serializing context and
    // are now installed alongside the published surface.
    known = new Set(Reflect.ownKeys(globalThis));
    for (const [name, callbacks] of bootstrapCallbacks) host[name](...callbacks);
    setTraceActive(host.ready(setTraceActive));
  };
  // Resolution is synchronous and never fetches. Import maps are not yet
  // supported; unprefixed names must fail rather than become relative URLs.
  globalThis.__mimicImportMetaResolveFactory = (base) => {
    const resolve = {
      resolve(specifier) {
        const text = `${specifier}`;
        if (!/^(?:[A-Za-z][A-Za-z0-9+.-]*:|\/|\.\.?\/)/.test(text))
          throw new TypeError('Failed to resolve module specifier ' + JSON.stringify(text));
        try {
          return host.urlParts(text, base).href;
        } catch {
          throw new TypeError('Failed to resolve module specifier ' + JSON.stringify(text));
        }
      },
    }.resolve;
    markNative(resolve, 'resolve');
    return resolve;
  };
  /* dev_preview_install */
  globalThis.__mimicEvalSourceResolver = evalSourceResolver;
  /* compose_surface_tail */
  known = new Set(Reflect.ownKeys(globalThis));
  setTraceActive(host.ready(setTraceActive));
  globalThis.__mimicUnsupportedProbe = (n) => {
    if (!known.has(n)) host.unsupported(String(n));
  };
})(__mimic);
