// Bind semantic domains to the already-selected Blink exposure. Constructor
// objects and descriptors come from generated data; wrappers carry only slots.
const installNavigatorCapabilities = () => {
  globalThis.__mimicDispatchInput = (nodeID, type) =>
    dispatchTrusted(
      wrap(nodeID) || document.body || globalThis,
      new Event(type, { bubbles: true, cancelable: true }),
    );
  const members = globalThis.__mimicNavigatorMembers || [];
  delete globalThis.__mimicNavigatorMembers;
  const names = new Set(
    'vendorSub productSub appCodeName doNotTrack userActivation scheduling geolocation bluetooth clipboard credentials ink devicePosture hid keyboard managed mediaCapabilities mediaSession mediaDevices permissions presentation serial serviceWorker virtualKeyboard wakeLock usb windowControlsOverlay xr storageBuckets locks connection storage webkitTemporaryStorage webkitPersistentStorage login protectedAudience deprecatedRunAdAuctionEnforcesKAnonymity'.split(
      ' ',
    ),
  );
  const slots = new WeakMap(),
    singletons = new Map();
  const native = (fn, name, prefix = '') => {
    Object.defineProperty(fn, 'name', { value: prefix + name, configurable: true });
    markNative(fn, name, prefix);
    return fn;
  };
  const state = (domain, ...args) => host.capabilityState(domain, ...args);
  const secureContext = host.documentSecurity().secureContext;
  const conditions = globalThis.__mimicCapabilityConditions || {};
  delete globalThis.__mimicCapabilityConditions;
  const disabled = (condition) =>
    [condition?.RuntimeEnabled, condition?.ExposureFeatures?.Window].some(
      (flag) => typeof flag === 'string' && !state('feature', flag),
    );
  for (const [type, condition] of Object.entries(conditions))
    if (disabled(condition)) delete globalThis[type];
  const check = (object, type) => {
    const slot = slots.get(object);
    if (!slot || slot.type !== type) throw new TypeError('Illegal invocation');
    return slot;
  };
  const create = (type, data = {}) => {
    const ctor = globalThis[type];
    if (!ctor || !ctor.prototype) throw new TypeError('Unavailable interface ' + type);
    if (!Object.hasOwn(ctor.prototype, Symbol.toStringTag))
      Object.defineProperty(ctor.prototype, Symbol.toStringTag, {
        value: type,
        configurable: true,
      });
    const object = Object.create(ctor.prototype);
    slots.set(object, { type, ...data });
    return object;
  };
  const error = (name, message) => platformDOMException(message, name);
  const deny = (name, message) => platformPromiseReject(error(name, message));
  const method = (type, name, implementation, async = false) => {
    const proto = globalThis[type]?.prototype,
      d = proto && Object.getOwnPropertyDescriptor(proto, name);
    const staged = generatedLazyOperationIndex.get(type + '\0' + name);
    if (!d || (typeof d.value !== 'function' && !(d.value === undefined && staged))) return;
    // Unmaterialized generated operations still carry the captured IDL arity.
    // Installing semantics must not require reading a fallback callable first.
    const requiredLength = typeof d.value === 'function' ? d.value.length : staged.length;
    const fn = {
      [name](...args) {
        const run = () => {
          const slot = check(this, type);
          if (args.length < requiredLength)
            throw new TypeError(
              `Failed to execute '${name}' on '${type}': ${requiredLength} argument${requiredLength === 1 ? '' : 's'} required, but only ${args.length} present.`,
            );
          return implementation.call(this, slot, ...args);
        };
        if (async === 'immediate') {
          try {
            return platformPromiseResolve(run());
          } catch (e) {
            return platformPromiseReject(e);
          }
        }
        return async ? platformPromiseResolve().then(run) : run();
      },
    }[name];
    Object.defineProperty(fn, 'length', { value: requiredLength, configurable: true });
    native(fn, name);
    Object.defineProperty(proto, name, { ...d, value: fn });
  };
  const attribute = (type, name, get, set) => {
    const proto = globalThis[type]?.prototype,
      d = proto && Object.getOwnPropertyDescriptor(proto, name);
    if (!d || !d.get) return;
    const getter = {
      [name]() {
        return get(check(this, type), this);
      },
    }[name];
    native(getter, name, 'get ');
    const descriptor = { ...d, get: getter };
    if (d.set && set) {
      descriptor.set = {
        [name](value) {
          set(check(this, type), value, this);
        },
      }[name];
      native(descriptor.set, name, 'set ');
    }
    Object.defineProperty(proto, name, descriptor);
  };
  const scalar = () => state('identity');
  // These interfaces are exposed even without a model service. Keep their
  // static entry points coherent: convert dictionaries before resolving
  // availability or rejecting creation, and never fabricate model objects.
  const aiSchemas = {
    Summarizer: {
      policy: 'summarizer',
      core: [
        'expectedContextLanguages',
        'expectedInputLanguages',
        'format',
        'length',
        'outputLanguage',
        'type',
      ],
      create: ['monitor', 'sharedContext', 'signal'],
    },
    LanguageModel: {
      policy: 'language-model',
      core: ['expectedInputs', 'expectedOutputs', 'temperature', 'topK'],
      create: ['initialPrompts', 'monitor', 'signal'],
    },
    Translator: {
      policy: 'translator',
      core: ['sourceLanguage', 'targetLanguage'],
      create: ['monitor', 'signal'],
      required: true,
    },
    LanguageDetector: {
      policy: 'language-detector',
      core: ['expectedInputLanguages'],
      create: ['monitor', 'signal'],
    },
  };
  const aiEnums = {
    format: ['SummarizerFormat', ['plain-text', 'markdown']],
    length: ['SummarizerLength', ['short', 'medium', 'long']],
    type: ['SummarizerType', ['tl;dr', 'key-points', 'teaser', 'headline']],
  };
  const aiConversionErrors = new WeakSet(),
    aiTypeError = (message) => {
      const error = new TypeError(message);
      aiConversionErrors.add(error);
      return error;
    };
  // Keep author exceptions intact; only locally produced WebIDL conversion
  // failures acquire dictionary/member context. Coercion callbacks can throw
  // a TypeError too, and its identity belongs to the author.
  const aiPrimitive = (value, hint) => {
    if (value === null || !['object', 'function'].includes(typeof value)) return value;
    const primitive = value[Symbol.toPrimitive];
    if (primitive != null) {
      if (typeof primitive !== 'function')
        throw aiTypeError('Cannot convert object to primitive value');
      const result = Reflect.apply(primitive, value, [hint]);
      if (Object(result) === result) throw aiTypeError('Cannot convert object to primitive value');
      return result;
    }
    for (const name of hint === 'string' ? ['toString', 'valueOf'] : ['valueOf', 'toString']) {
      const method = value[name];
      if (typeof method === 'function') {
        const result = Reflect.apply(method, value, []);
        if (Object(result) !== result) return result;
      }
    }
    throw aiTypeError('Cannot convert object to primitive value');
  };
  const aiString = (value) => {
    value = aiPrimitive(value, 'string');
    if (typeof value === 'symbol') throw aiTypeError('Cannot convert a Symbol value to a string');
    return String(value);
  };
  const aiNumber = (value) => {
    value = aiPrimitive(value, 'number');
    if (typeof value === 'bigint') throw aiTypeError('Cannot convert a BigInt value to a number');
    if (typeof value === 'symbol') throw aiTypeError('Cannot convert a Symbol value to a number');
    return +value;
  };
  const aiSequence = (value, convert) => {
    if (value === null || !['object', 'function'].includes(typeof value))
      throw aiTypeError('The provided value cannot be converted to a sequence.');
    const method = value[Symbol.iterator];
    if (typeof method !== 'function')
      throw aiTypeError('The object must have a callable @@iterator property.');
    // An iterator record reads @@iterator and next exactly once.
    const iterator = Reflect.apply(method, value, []);
    if (Object(iterator) !== iterator) throw aiTypeError('Iterator object must be an object.');
    const next = iterator.next,
      out = [];
    for (;;) {
      const step = Reflect.apply(next, iterator, []);
      if (Object(step) !== step) throw aiTypeError('Expected iterator.next() to return an Object.');
      if (step.done) return out;
      out.push(convert(step.value));
    }
  };
  const aiConvert = (context, convert, value) => {
    try {
      return convert(value);
    } catch (error) {
      if (aiConversionErrors.has(error)) throw aiTypeError(context + error.message);
      throw error;
    }
  };
  const aiMember = (options, key, dictionary, convert, required = false) => {
    const value = options[key];
    if (value === undefined) {
      if (required)
        throw aiTypeError(
          `Failed to read the '${key}' property from '${dictionary}': Required member is undefined.`,
        );
      return undefined;
    }
    return aiConvert(`Failed to read the '${key}' property from '${dictionary}': `, convert, value);
  };
  const aiEnum = (name, values) => (value) => {
    const text = aiString(value);
    if (!values.includes(text))
      throw aiTypeError(`The provided value '${text}' is not a valid enum value of type ${name}.`);
    return text;
  };
  const aiExpected = (value) => {
    if (value != null && !['object', 'function'].includes(typeof value))
      throw aiTypeError("The provided value is not of type 'LanguageModelExpected'.");
    const options = value || {},
      languages = aiMember(options, 'languages', 'LanguageModelExpected', (v) =>
        aiSequence(v, aiString),
      );
    const type = aiMember(
      options,
      'type',
      'LanguageModelExpected',
      aiEnum('LanguageModelMessageType', ['text', 'image', 'audio', 'tool-call', 'tool-response']),
      true,
    );
    return { languages, type };
  };
  const aiMessage = (value) => {
    if (value != null && !['object', 'function'].includes(typeof value))
      throw aiTypeError("The provided value is not of type 'LanguageModelMessage'.");
    const options = value || {},
      content = aiMember(
        options,
        'content',
        'LanguageModelMessage',
        (v) => {
          if (v !== null && typeof v === 'object')
            throw platformDOMException(
              'Structured model prompt content is unsupported.',
              'NotSupportedError',
            );
          return aiString(v);
        },
        true,
      ),
      prefix = aiMember(options, 'prefix', 'LanguageModelMessage', Boolean);
    const role = aiMember(
      options,
      'role',
      'LanguageModelMessage',
      aiEnum('LanguageModelMessageRole', ['system', 'user', 'assistant']),
      true,
    );
    return { content, prefix, role };
  };
  const aiOptions = (type, operation, args) => {
    const schema = aiSchemas[type],
      dictionary = type + (operation === 'create' ? 'CreateOptions' : 'CreateCoreOptions'),
      core = type + 'CreateCoreOptions',
      prefix = `Failed to execute '${operation}' on '${type}': `;
    const fail = (message) => {
      throw aiTypeError(prefix + message);
    };
    if (schema.required && !args.length) fail('1 argument required, but only 0 present.');
    const input = args[0];
    if (
      (input != null && !['object', 'function'].includes(typeof input)) ||
      (schema.required && input == null)
    )
      fail(`The provided value is not of type '${dictionary}'.`);
    const converted = {},
      options = input || {};
    for (const key of schema.core.concat(operation === 'create' ? schema.create : [])) {
      const value = options[key],
        owner = schema.core.includes(key) ? core : dictionary;
      if (value === undefined) {
        if (schema.required && schema.core.includes(key))
          fail(
            `Failed to read the '${key}' property from '${owner}': Required member is undefined.`,
          );
        continue;
      }
      let convert = (v) => v;
      if (type === 'Summarizer' && aiEnums[key]) convert = aiEnum(...aiEnums[key]);
      else if (
        ['sourceLanguage', 'targetLanguage', 'outputLanguage', 'sharedContext'].includes(key)
      )
        convert = aiString;
      else if (['expectedInputLanguages', 'expectedContextLanguages'].includes(key))
        convert = (v) => aiSequence(v, aiString);
      else if (['expectedInputs', 'expectedOutputs'].includes(key))
        convert = (v) => aiSequence(v, aiExpected);
      else if (key === 'initialPrompts') convert = (v) => aiSequence(v, aiMessage);
      else if (key === 'monitor')
        convert = (v) => {
          if (typeof v !== 'function') throw aiTypeError('The given value is not a function.');
          return v;
        };
      else if (key === 'signal')
        convert = (v) => {
          try {
            Object.getOwnPropertyDescriptor(AbortSignal.prototype, 'aborted').get.call(v);
          } catch {
            throw aiTypeError("Failed to convert value to 'AbortSignal'.");
          }
          return v;
        };
      else if (key === 'temperature' || key === 'topK') convert = aiNumber;
      converted[key] = aiConvert(
        prefix + `Failed to read the '${key}' property from '${owner}': `,
        convert,
        value,
      );
    }
    return converted;
  };
  for (const [type, schema] of Object.entries(aiSchemas)) {
    const constructor = globalThis[type];
    if (typeof constructor !== 'function') continue;
    // The frozen profile no longer exposes LanguageModel.params. Static
    // IDL fallback order is not authoritative for these implemented entries.
    if (type === 'LanguageModel') delete constructor.params;
    for (const operation of ['availability', 'create']) delete constructor[operation];
    for (const operation of ['availability', 'create']) {
      const implementation = {
        [operation](...args) {
          try {
            const options = aiOptions(type, operation, args),
              capability = state('ai', schema.policy);
            if (operation === 'availability') return platformPromiseResolve('unavailable');
            if (options.signal?.aborted) return platformPromiseReject(options.signal.reason);
            if (!capability.policyAllowed)
              return deny(
                'NotAllowedError',
                'Access denied because the Permission Policy is not enabled.',
              );
            return deny('NotSupportedError', `${type} backend is unavailable.`);
          } catch (error) {
            aiConversionErrors.delete(error);
            return platformPromiseReject(error);
          }
        },
      }[operation];
      Object.defineProperty(implementation, 'length', {
        value: schema.required ? 1 : 0,
        configurable: true,
      });
      native(implementation, operation);
      Object.defineProperty(constructor, operation, {
        value: implementation,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    }
  }
  slots.set(nav, { type: 'Navigator' });
  // A machine without battery/gamepad backends has observable, empty device
  // state. Battery promises and the manager are stable within this Navigator.
  let batteryPromise;
  if (typeof BatteryManager === 'function') {
    const readBattery = () => state('battery');
    for (const key of ['charging', 'level'])
      attribute('BatteryManager', key, () => readBattery()[key]);
    for (const key of ['chargingTime', 'dischargingTime'])
      attribute('BatteryManager', key, () => readBattery()[key] ?? Infinity);
    for (const key of [
      'onchargingchange',
      'onchargingtimechange',
      'ondischargingtimechange',
      'onlevelchange',
    ])
      attribute(
        'BatteryManager',
        key,
        (_s, target) => eventHandlerRecord(target, key.slice(2)).value,
        (_s, value, target) => setEventHandlerValue(target, key.slice(2), value),
      );
    const getBattery = {
      getBattery() {
        try {
          check(this, 'Navigator');
          if (!batteryPromise) batteryPromise = platformPromiseResolve(create('BatteryManager'));
          return batteryPromise;
        } catch (e) {
          return platformPromiseReject(e);
        }
      },
    }.getBattery;
    native(getBattery, 'getBattery');
    const descriptor = Object.getOwnPropertyDescriptor(Navigator.prototype, 'getBattery');
    if (descriptor)
      Object.defineProperty(Navigator.prototype, 'getBattery', {
        ...descriptor,
        value: getBattery,
      });
  }
  method('Navigator', 'getGamepads', () => {
    if (!documentPolicy.allowsFeature('gamepad'))
      throw error('SecurityError', 'Access to gamepads is disallowed by permissions policy.');
    return [null, null, null, null];
  });
  const domainSources = new Set(
    members.filter((m) => names.has(m.name)).map((m) => m.origin?.source),
  );
  // Close the declaring Navigator domain too (not only its object-valued
  // entry point). In particular auction operations must not inherit a
  // successful null from the generic surface when there is no auction backend.
  for (const member of members)
    if (member.kind === 'operation' && domainSources.has(member.origin?.source)) {
      method(
        'Navigator',
        member.name,
        () => {
          throw error('NotSupportedError', `Navigator.${member.name} backend is unavailable.`);
        },
        String(member.returnType).startsWith('Promise'),
      );
    }
  method('Navigator', 'javaEnabled', () => false);
  const capabilityInterfaces = globalThis.__mimicCapabilityInterfaces || {};
  delete globalThis.__mimicCapabilityInterfaces;
  const boundTypes = new Set(members.filter((m) => names.has(m.name)).map((m) => m.type));
  for (const type of [
    'PermissionStatus',
    'StorageBucket',
    'Lock',
    'MediaDeviceInfo',
    'DelegatedInkTrailPresenter',
    'KeyboardLayoutMap',
  ])
    boundTypes.add(type);
  // A deliberately absent subsystem fails explicitly, with the WebIDL return
  // convention, rather than inheriting semanticMissing's successful null.
  for (const type of boundTypes) {
    if (typeof lazyDomainInterfaces !== 'undefined' && lazyDomainInterfaces.has(type)) continue;
    for (const member of capabilityInterfaces[type] || []) {
      const unsupported = () => {
        throw error('NotSupportedError', `${type}.${member.name} backend is unavailable.`);
      };
      if (member.kind === 'operation')
        method(type, member.name, unsupported, String(member.returnType).startsWith('Promise'));
      if (member.kind === 'attribute') attribute(type, member.name, unsupported, unsupported);
    }
  }
  const quotaPrototype = {};
  Object.defineProperty(quotaPrototype, Symbol.toStringTag, {
    value: 'DeprecatedStorageQuota',
    configurable: true,
  });
  for (const [name, length, fn] of [
    [
      'queryUsageAndQuota',
      1,
      function (success, failure) {
        check(this, 'DeprecatedStorageQuota');
        if (typeof success !== 'function')
          throw new TypeError('The callback provided as parameter 1 is not a function.');
        setTimeout(() => {
          const q = state('quota');
          if (q === null) {
            if (typeof failure === 'function')
              failure(
                error(
                  'NotSupportedError',
                  'The implementation did not support the requested type of object or operation.',
                ),
              );
            return;
          }
          success(q.usage, q.quota);
        }, 0);
      },
    ],
    [
      'requestQuota',
      1,
      function (bytes, success, failure) {
        check(this, 'DeprecatedStorageQuota');
        if (success !== undefined && typeof success !== 'function')
          throw new TypeError('The callback provided as parameter 2 is not a function.');
        setTimeout(() => {
          const q = state('quota');
          if (q === null) {
            if (typeof failure === 'function')
              failure(
                error(
                  'NotSupportedError',
                  'The implementation did not support the requested type of object or operation.',
                ),
              );
            return;
          }
          if (success) success(Math.min(Math.max(0, Number(bytes) || 0), q.quota));
        }, 0);
      },
    ],
  ]) {
    const wrapper = {
      [name](...args) {
        if (!args.length)
          throw new TypeError(
            `Failed to execute '${name}' on 'DeprecatedStorageQuota': 1 argument required, but only 0 present.`,
          );
        return fn.apply(this, args);
      },
    }[name];
    Object.defineProperty(wrapper, 'length', { value: length, configurable: true });
    native(wrapper, name);
    Object.defineProperty(quotaPrototype, name, {
      value: wrapper,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  for (const member of members) {
    if (!names.has(member.name)) continue;
    const d = Object.getOwnPropertyDescriptor(Navigator.prototype, member.name);
    if (!d) continue;
    const flags = [member.origin?.extended?.RuntimeEnabled, member.extended?.RuntimeEnabled].filter(
      (f) => typeof f === 'string',
    );
    if (flags.some((flag) => !state('feature', flag)) || disabled(conditions[member.type])) {
      delete Navigator.prototype[member.name];
      continue;
    }
    const getter = {
      [member.name]() {
        if (this !== nav) throw new TypeError('Illegal invocation');
        if (member.name === 'gpu' && typeof requireLazySingleton === 'function')
          return requireLazySingleton('webgpu', 'gpu');
        if (['vendorSub', 'productSub', 'appCodeName', 'doNotTrack'].includes(member.name))
          return scalar()[member.name];
        if (member.name === 'deprecatedRunAdAuctionEnforcesKAnonymity') return false;
        const key = member.type === 'DeprecatedStorageQuota' ? member.type : member.name;
        if (!singletons.has(key)) {
          let value;
          if (member.type === 'DeprecatedStorageQuota') {
            value = Object.create(quotaPrototype);
            slots.set(value, { type: member.type });
          } else value = create(member.type);
          singletons.set(key, value);
        }
        return singletons.get(key);
      },
    }[member.name];
    native(getter, member.name, 'get ');
    Object.defineProperty(Navigator.prototype, member.name, { ...d, get: getter });
  }
  // Event-handler attributes use private slots and retain EventTarget's shared
  // listener machinery. No own fields leak through singleton objects.
  for (const type of new Set(members.filter((m) => names.has(m.name)).map((m) => m.type))) {
    const proto = globalThis[type]?.prototype;
    if (!proto) continue;
    for (const name of Object.getOwnPropertyNames(proto))
      if (name.startsWith('on'))
        attribute(
          type,
          name,
          (s) => s[name] || null,
          (s, v) => (s[name] = typeof v === 'function' ? v : null),
        );
  }
  for (const name of ['isActive', 'hasBeenActive'])
    attribute('UserActivation', name, () => state('activation')[name]);
  method('Scheduling', 'isInputPending', () => state('activation').pending);
  for (const name of ['effectiveType', 'downlink', 'rtt', 'saveData'])
    attribute('NetworkInformation', name, () => state('network')[name]);
  attribute('DevicePosture', 'type', () => state('devices').posture || 'continuous');

  // One permission source feeds query, location, clipboard and media.
  const permissionNames = new Set(globalThis.__mimicPermissionNames || []);
  delete globalThis.__mimicPermissionNames;
  const permission = (name) =>
    documentPolicy.allowsFeature(name) ? state('permission', name) : 'denied';
  const permissionStatuses = new Set();
  const permissionStatusNames = {
    camera: 'video_capture',
    microphone: 'audio_capture',
    accelerometer: 'sensors',
    gyroscope: 'sensors',
    magnetometer: 'sensors',
    'ambient-light-sensor': 'sensors',
    'persistent-storage': 'durable_storage',
    push: 'notifications',
    'clipboard-read': 'clipboard_read',
    'clipboard-write': 'clipboard_write',
    'background-sync': 'background_sync',
    'background-fetch': 'background_fetch',
    'periodic-background-sync': 'periodic_background_sync',
    'display-capture': 'display_capture',
    'screen-wake-lock': 'screen_wake_lock',
    'system-wake-lock': 'system_wake_lock',
    'payment-handler': 'payment_handler',
  };
  globalThis.__mimicPermissionChanged = (name) => {
    if (name === 'geolocation') refreshLocations();
    for (const status of permissionStatuses) {
      const s = slots.get(status);
      if (s.name === name && s.lastState !== permission(name)) {
        s.lastState = permission(name);
        dispatchTrusted(status, new Event('change'));
      }
    }
  };
  method(
    'Permissions',
    'query',
    (_s, descriptor) => {
      if (descriptor === null || !['object', 'function'].includes(typeof descriptor))
        throw new TypeError(
          "Failed to execute 'query' on 'Permissions': parameter 1 is not of type 'object'.",
        );
      const readName = (dictionary) => {
        const prefix =
          "Failed to execute 'query' on 'Permissions': Failed to read the 'name' property from '" +
          dictionary +
          "': ";
        const value = descriptor.name;
        if (value === undefined) throw new TypeError(prefix + 'Required member is undefined.');
        let text;
        try {
          text = aiString(value);
        } catch (e) {
          if (aiConversionErrors.has(e)) throw new TypeError(prefix + e.message.replace(/\.$/, ''));
          throw e;
        }
        if (!permissionNames.has(text))
          throw new TypeError(
            prefix +
              "The provided value '" +
              text +
              "' is not a valid enum value of type PermissionName.",
          );
        return text;
      };
      const name = readName('PermissionDescriptor');
      const disabled = {
        'ambient-light-sensor': [
          'GenericSensorExtraClasses',
          'GenericSensorExtraClasses flag is not enabled.',
        ],
        nfc: ['WebNFC', 'Web NFC is not enabled.'],
        'system-wake-lock': ['SystemWakeLock', 'System Wake Lock is not enabled.'],
        'speaker-selection': ['SpeakerSelection', 'The Speaker Selection API is not enabled.'],
        'web-app-installation': ['WebAppInstallation', 'The Web App Install API is not enabled.'],
        'geolocation-approximate': [
          'ApproximateGeolocationPermission',
          'Permission API support for approximate geolocation is not enabled.',
        ],
      }[name];
      if (disabled && !state('feature', disabled[0]))
        throw new TypeError("Failed to execute 'query' on 'Permissions': " + disabled[1]);
      // Blink first selects the descriptor type, then converts that dictionary.
      // Both reads are observable; conversion runs before query returns its Promise.
      const schema = {
        camera: ['CameraDevicePermissionDescriptor', 'panTiltZoom'],
        midi: ['MidiPermissionDescriptor', 'sysex'],
        push: ['PushPermissionDescriptor', 'userVisibleOnly'],
        'clipboard-read': [
          'ClipboardPermissionDescriptor',
          'allowWithoutGesture',
          'allowWithoutSanitization',
        ],
        'clipboard-write': [
          'ClipboardPermissionDescriptor',
          'allowWithoutGesture',
          'allowWithoutSanitization',
        ],
        fullscreen: ['FullscreenPermissionDescriptor', 'allowWithoutGesture'],
        'top-level-storage-access': [
          'TopLevelStorageAccessPermissionDescriptor',
          'requestedOrigin',
        ],
      }[name];
      const options = {};
      if (schema) {
        readName(schema[0]);
        for (const key of schema.slice(1)) options[key] = descriptor[key];
      }
      if (name === 'push' && !options.userVisibleOnly)
        throw error(
          'NotSupportedError',
          "Failed to execute 'query' on 'Permissions': Push Permission without userVisibleOnly:true isn't supported yet.",
        );
      if (name === 'fullscreen' && !options.allowWithoutGesture)
        throw new TypeError(
          "Failed to execute 'query' on 'Permissions': Fullscreen Permission only supports allowWithoutGesture:true.",
        );
      if (name === 'top-level-storage-access' && !options.requestedOrigin)
        throw new TypeError(
          "Failed to execute 'query' on 'Permissions': The requested origin is invalid.",
        );
      const stateName =
        name === 'push'
          ? 'notifications'
          : name === 'clipboard-write' &&
              (options.allowWithoutGesture || options.allowWithoutSanitization)
            ? 'clipboard-read'
            : name;
      // Descriptor conversion and rejection occur before returning, but a
      // successful service response belongs to a later event-loop task.
      // Use the private host scheduler so author timer overrides cannot make
      // fulfillment synchronous. Ordering against other task sources is not
      // determined by the permission service's transport latency.
      return new platformPromise((resolve, reject) => {
        host.setTimer(
          () => {
            try {
              const status = create('PermissionStatus', {
                name: stateName,
                statusName: permissionStatusNames[stateName] || stateName,
                lastState: permission(stateName),
              });
              permissionStatuses.add(status);
              resolve(status);
            } catch (e) {
              reject(e);
            }
          },
          0,
          false,
        );
      });
    },
    'immediate',
  );
  attribute('PermissionStatus', 'state', (s) => permission(s.name));
  attribute('PermissionStatus', 'name', (s) => s.statusName);
  attribute(
    'PermissionStatus',
    'onchange',
    (s) => s.onchange || null,
    (s, v) => (s.onchange = typeof v === 'function' ? v : null),
  );
  const permissionPrompt = () =>
    platformPromiseReject(
      error('NotAllowedError', 'Permission requires an explicit automation decision'),
    );
  for (const [name, message] of [
    ['get', 'No credential type was specified in the request.'],
    [
      'create',
      "Only exactly one of 'password', 'federated', and 'publicKey' credential types are currently supported.",
    ],
  ])
    method(
      'CredentialsContainer',
      name,
      (_s, options = {}) => {
        if (options.signal?.aborted) throw options.signal.reason;
        if (name === 'get' && options.password && options.mediation === 'silent') return null;
        throw error('NotSupportedError', message);
      },
      true,
    );
  method(
    'CredentialsContainer',
    'preventSilentAccess',
    () => {
      state('preventSilent');
    },
    true,
  );
  method(
    'CredentialsContainer',
    'store',
    () => {
      throw error('NotSupportedError', 'Credential storage is unavailable.');
    },
    true,
  );
  for (const name of ['read', 'readText'])
    method(
      'Clipboard',
      name,
      () => {
        if (permission('clipboard-read') === 'prompt' && state('presentation').mode === 'headful')
          return permissionPrompt();
        if (permission('clipboard-read') !== 'granted') {
          throw error(
            'NotAllowedError',
            `Failed to execute '${name}' on 'Clipboard': Read permission denied.`,
          );
        }
        if (name === 'readText') return state('clipboard');
        throw error('NotSupportedError', 'Clipboard item transport is unavailable.');
      },
      true,
    );
  method(
    'Clipboard',
    'writeText',
    (_s, value) => {
      if (
        permission('clipboard-write') === 'denied' ||
        (permission('clipboard-write') !== 'granted' && !state('activation').isActive)
      )
        throw error(
          'NotAllowedError',
          "Failed to execute 'writeText' on 'Clipboard': Write permission denied.",
        );
      state('clipboardWrite', String(value));
    },
    true,
  );
  method(
    'Clipboard',
    'write',
    () => {
      throw error('NotSupportedError', 'Clipboard item transport is unavailable.');
    },
    true,
  );
  let watchID = 0;
  const locations = new Map();
  let cachedPosition = null;
  const positionFrom = (provider) => {
    const coords = create('GeolocationCoordinates', {
      latitude: provider.latitude,
      longitude: provider.longitude,
      accuracy: provider.accuracy,
      altitude: provider.altitude ?? null,
      altitudeAccuracy: provider.altitudeAccuracy ?? null,
      heading: provider.heading ?? null,
      speed: provider.speed ?? null,
    });
    return create('GeolocationPosition', { coords, timestamp: Date.now() });
  };
  const finishLocation = (request, value, failed) => {
    if (!locations.has(request.id)) return;
    if (request.timer !== null) clearTimeout(request.timer);
    request.timer = null;
    if (!request.watch) locations.delete(request.id);
    const callback = failed ? request.failure : request.success;
    if (typeof callback === 'function') callback(value);
  };
  const deliverLocation = (request) => {
    if (!locations.has(request.id)) return;
    const status = permission('geolocation');
    // Blink reports insecure-origin denial before Permissions Policy denial.
    if (secureContext && !state('geolocationPolicy')) {
      finishLocation(
        request,
        create('GeolocationPositionError', {
          code: 1,
          message: 'Geolocation has been disabled in this document by permissions policy.',
        }),
        true,
      );
      locations.delete(request.id);
      return;
    }
    if (secureContext && status === 'prompt' && state('presentation').mode === 'headful') return;
    if (!secureContext || status !== 'granted') {
      finishLocation(
        request,
        create('GeolocationPositionError', {
          code: 1,
          message: !secureContext
            ? 'Only secure origins are allowed (see: https://goo.gl/Y0ZkNV).'
            : 'User denied Geolocation',
        }),
        true,
      );
      locations.delete(request.id);
      return;
    }
    if (
      cachedPosition &&
      request.maximumAge > 0 &&
      Date.now() - slots.get(cachedPosition).timestamp <= request.maximumAge
    ) {
      finishLocation(request, cachedPosition, false);
      return;
    }
    if (request.timeout === 0) {
      finishLocation(
        request,
        create('GeolocationPositionError', { code: 3, message: 'Timeout expired' }),
        true,
      );
      return;
    }
    const provider = state('geolocation');
    if (
      provider &&
      provider.latitude !== null &&
      provider.longitude !== null &&
      provider.accuracy !== null
    ) {
      cachedPosition = positionFrom(provider);
      finishLocation(request, cachedPosition, false);
    } else {
      finishLocation(
        request,
        create('GeolocationPositionError', {
          code: 2,
          message: provider ? '' : 'Position unavailable',
        }),
        true,
      );
    }
  };
  const refreshLocations = () => {
    cachedPosition = null;
    for (const request of locations.values()) setTimeout(() => deliverLocation(request), 0);
  };
  globalThis.__mimicGeolocationChanged = refreshLocations;
  const locate = (success, failure, options, watch) => {
    if (typeof success !== 'function')
      throw new TypeError('The callback provided as parameter 1 is not a function.');
    if (failure != null && typeof failure !== 'function')
      throw new TypeError('The callback provided as parameter 2 is not a function.');
    options = options ?? {};
    const bounded = (value, fallback) =>
      value === undefined
        ? fallback
        : Math.min(4294967295, Math.max(0, Math.trunc(Number(value) || 0)));
    const enableHighAccuracy = !!options.enableHighAccuracy;
    const maximumAge = bounded(options.maximumAge, 0);
    const timeout = bounded(options.timeout, 4294967295);
    const request = {
      id: ++watchID,
      success,
      failure,
      watch,
      enableHighAccuracy,
      maximumAge,
      timeout,
      timer: null,
    };
    locations.set(request.id, request);
    setTimeout(() => deliverLocation(request), 0);
    return request.id;
  };
  method('Geolocation', 'getCurrentPosition', (_s, success, failure, options) => {
    locate(success, failure, options, false);
  });
  method('Geolocation', 'watchPosition', (_s, success, failure, options) =>
    locate(success, failure, options, true),
  );
  method('Geolocation', 'clearWatch', (_s, id) => {
    id = Number(id) >> 0;
    const request = locations.get(id);
    if (request && request.timer !== null) clearTimeout(request.timer);
    locations.delete(id);
  });
  attribute('GeolocationPosition', 'coords', (s) => s.coords);
  attribute('GeolocationPosition', 'timestamp', (s) => s.timestamp);
  for (const name of [
    'latitude',
    'longitude',
    'accuracy',
    'altitude',
    'altitudeAccuracy',
    'heading',
    'speed',
  ])
    attribute('GeolocationCoordinates', name, (s) => s[name]);
  const coordinatesJSON = (s) =>
    Object.fromEntries(
      ['accuracy', 'latitude', 'longitude', 'altitude', 'altitudeAccuracy', 'heading', 'speed'].map(
        (name) => [name, s[name]],
      ),
    );
  method('GeolocationCoordinates', 'toJSON', coordinatesJSON);
  method('GeolocationPosition', 'toJSON', (s) => ({
    coords: coordinatesJSON(slots.get(s.coords)),
    timestamp: s.timestamp,
  }));
  attribute('GeolocationPositionError', 'code', (s) => s.code);
  attribute('GeolocationPositionError', 'message', (s) => s.message);

  // Hardware enumeration reflects the absence of granted transports. Chooser
  // requests enforce activation and never manufacture successful devices.
  method('Bluetooth', 'getAvailability', () => state('devices').bluetooth, true);
  for (const [type, name] of [
    ['HID', 'getDevices'],
    ['USB', 'getDevices'],
    ['Serial', 'getPorts'],
  ])
    method(type, name, () => state('deviceList', type), true);
  for (const [type, name] of [
    ['Bluetooth', 'requestDevice'],
    ['HID', 'requestDevice'],
    ['USB', 'requestDevice'],
    ['Serial', 'requestPort'],
  ])
    method(
      type,
      name,
      () => {
        if (!state('activation').isActive)
          throw error(
            'SecurityError',
            `Failed to execute '${name}' on '${type}': Must be handling a user gesture to show a permission request.`,
          );
        if (type === 'HID') return [];
        throw error('NotFoundError', 'No device selected.');
      },
      true,
    );

  method('StorageManager', 'estimate', () => state('quota'), true);
  method('StorageManager', 'persisted', () => permission('persistent-storage') === 'granted', true);
  method('StorageManager', 'persist', () => permission('persistent-storage') === 'granted', true);
  const bucketName = (value) => {
    const name = String(value);
    if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(name))
      throw new TypeError('The bucket name is not valid.');
    return name;
  };
  const bucket = (s, operation = 'get', value) => {
    const b = state('bucket', operation, s.name, value);
    if (b === null) throw error('InvalidStateError', 'The storage bucket has been deleted.');
    return b;
  };
  method('StorageBucketManager', 'keys', () => state('bucket', 'keys'), true);
  method(
    'StorageBucketManager',
    'open',
    (_s, name, options = {}) => {
      name = bucketName(name);
      if (options.durability !== undefined && !['strict', 'relaxed'].includes(options.durability))
        throw new TypeError('Invalid StorageBucketDurability');
      if (options.quota !== undefined && !(Number(options.quota) > 0))
        throw new TypeError('The quota must be greater than zero.');
      if (options.expires !== undefined && !(Number(options.expires) > Date.now()))
        throw new TypeError('The expiration must be in the future.');
      state('bucket', 'open', name, JSON.stringify(options));
      return create('StorageBucket', { name });
    },
    true,
  );
  method(
    'StorageBucketManager',
    'delete',
    (_s, name) => {
      state('bucket', 'delete', bucketName(name));
    },
    true,
  );
  attribute('StorageBucket', 'name', (s) => s.name);
  for (const name of ['persisted', 'durability', 'expires'])
    method('StorageBucket', name, (s) => bucket(s)[name], true);
  method('StorageBucket', 'persist', (s) => bucket(s, 'persist').persisted, true);
  method(
    'StorageBucket',
    'estimate',
    (s) => {
      const b = bucket(s);
      return { usage: b.usage, quota: b.quota };
    },
    true,
  );
  method(
    'StorageBucket',
    'setExpires',
    (s, value) => {
      if (!(Number(value) > Date.now()))
        throw new TypeError('The expiration must be in the future.');
      bucket(s, 'setExpires', Number(value));
    },
    true,
  );
  method('LockManager', 'query', () => state('lock', 'query'), true);
  method(
    'LockManager',
    'request',
    (_s, name, options, callback) => {
      name = String(name);
      if (typeof options === 'function') {
        callback = options;
        options = {};
      }
      options = options || {};
      if (typeof callback !== 'function') throw new TypeError('The callback is not a function.');
      if (name.startsWith('-'))
        throw error('NotSupportedError', "Names starting with '-' are reserved");
      const mode = options.mode === undefined ? 'exclusive' : String(options.mode);
      if (!['exclusive', 'shared'].includes(mode)) throw new TypeError('Invalid LockMode');
      if (options.steal)
        throw error('NotSupportedError', 'Stealing an active lock is not supported.');
      if (options.signal && options.ifAvailable)
        throw error('NotSupportedError', 'The signal option cannot be used with ifAvailable.');
      if (options.signal?.aborted) throw options.signal.reason;
      const ticket = state('lock', 'enqueue', name, mode);
      return new platformPromise((resolve, reject) => {
        let finished = false,
          timer;
        const release = () => {
          if (finished) return;
          finished = true;
          if (timer !== undefined) clearTimeout(timer);
          state('lock', 'release', ticket);
          options.signal?.removeEventListener('abort', abort);
        };
        const abort = () => {
          release();
          reject(options.signal.reason);
        };
        options.signal?.addEventListener('abort', abort, { once: true });
        const attempt = () => {
          if (finished) return;
          if (!state('lock', 'acquire', ticket)) {
            if (!options.ifAvailable) {
              timer = setTimeout(attempt, 1);
              return;
            }
            release();
            platformPromiseResolve()
              .then(() => callback(null))
              .then(resolve, reject);
            return;
          }
          options.signal?.removeEventListener('abort', abort);
          platformPromiseResolve()
            .then(() => callback(create('Lock', { name, mode })))
            .then(
              (v) => {
                release();
                resolve(v);
              },
              (e) => {
                release();
                reject(e);
              },
            );
        };
        attempt();
      });
    },
    true,
  );
  attribute('Lock', 'name', (s) => s.name);
  attribute('Lock', 'mode', (s) => s.mode);

  method(
    'MediaDevices',
    'enumerateDevices',
    () =>
      host.cameraDevices().then((result) => {
        if (result.error) throw error(result.error, result.message);
        return result.devices.map((device) => create('MediaDeviceInfo', device));
      }),
    true,
  );
  method('MediaDevices', 'getSupportedConstraints', () =>
    Object.fromEntries((state('media').constraints || []).map((name) => [name, true])),
  );
  for (const name of ['deviceId', 'kind', 'label', 'groupId'])
    attribute('MediaDeviceInfo', name, (s) => s[name]);
  method('MediaDeviceInfo', 'toJSON', (s) => ({
    deviceId: s.deviceId,
    kind: s.kind,
    label: s.label,
    groupId: s.groupId,
  }));
  method(
    'MediaDevices',
    'getUserMedia',
    (_s, constraints = {}) => {
      if (!constraints.audio && !constraints.video)
        throw new TypeError(
          "Failed to execute 'getUserMedia' on 'MediaDevices': At least one of audio and video must be requested",
        );
      if (
        (constraints.video && permission('camera') !== 'granted') ||
        (constraints.audio && permission('microphone') !== 'granted')
      )
        throw error(
          'NotAllowedError',
          'Permission denied; grant access through Browser.setPermission or Browser.grantPermissions',
        );
      return cameraCaptureModel.open(constraints);
    },
    true,
  );
  method(
    'MediaDevices',
    'getDisplayMedia',
    () => {
      throw error('NotAllowedError', 'Permission denied');
    },
    true,
  );
  for (const [type, role] of [
    ['RTCRtpSender', 'sender'],
    ['RTCRtpReceiver', 'receiver'],
  ]) {
    const C = globalThis[type];
    if (!C) continue;
    const getCapabilities = {
      getCapabilities(kind) {
        if (arguments.length === 0)
          throw new TypeError(
            `Failed to execute 'getCapabilities' on '${type}': 1 argument required, but only 0 present.`,
          );
        if (typeof kind === 'symbol')
          throw new TypeError('Cannot convert a Symbol value to a string');
        return host.rtpCapabilities(String(kind), role);
      },
    }.getCapabilities;
    native(getCapabilities, 'getCapabilities');
    Object.defineProperty(C, 'getCapabilities', {
      value: getCapabilities,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  }
  // A selected bundle may omit optional media interfaces. Install behavior
  // only on interfaces supplied by that bundle, as with method/attribute.
  const mediaString = (value) => {
    if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    return String(value);
  };
  const canPlayType = {
    canPlayType(type) {
      const element = elementSlot(this);
      if (
        !element ||
        !['audio', 'video'].includes(element.localName || element.tagName?.toLowerCase())
      )
        throw new TypeError('Illegal invocation');
      if (!arguments.length)
        throw new TypeError(
          "Failed to execute 'canPlayType' on 'HTMLMediaElement': 1 argument required, but only 0 present.",
        );
      return host.mediaTypeSupport(mediaString(type)).play;
    },
  }.canPlayType;
  native(canPlayType, 'canPlayType');
  if (globalThis.HTMLMediaElement?.prototype)
    Object.defineProperty(HTMLMediaElement.prototype, 'canPlayType', {
      value: canPlayType,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  const isTypeSupported = {
    isTypeSupported(type) {
      if (!arguments.length)
        throw new TypeError(
          "Failed to execute 'isTypeSupported' on 'MediaSource': 1 argument required, but only 0 present.",
        );
      return host.mediaTypeSupport(mediaString(type)).mediaSource;
    },
  }.isTypeSupported;
  native(isTypeSupported, 'isTypeSupported');
  if (typeof globalThis.MediaSource === 'function')
    Object.defineProperty(MediaSource, 'isTypeSupported', {
      value: isTypeSupported,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  for (const name of ['decodingInfo', 'encodingInfo'])
    method(
      'MediaCapabilities',
      name,
      (_s, ...args) => {
        const prefix = `Failed to execute '${name}' on 'MediaCapabilities': `,
          fail = (message) => {
            throw new TypeError(prefix + message);
          },
          decode = name === 'decodingInfo';
        if (!args.length) fail('1 argument required, but only 0 present.');
        const configuration = args[0] ?? {};
        const tracks = {};
        for (const kind of ['audio', 'video']) {
          const raw = configuration[kind];
          if (raw === undefined) continue;
          const value = {};
          const keys =
            kind === 'video'
              ? ['bitrate', 'contentType', 'framerate', 'height', 'width']
              : ['bitrate', 'channels', 'contentType', 'samplerate'];
          for (const key of keys) {
            const v = raw?.[key];
            if (v === undefined) {
              if (kind === 'video' || key === 'contentType')
                fail(
                  `Failed to read the '${kind}' property from 'MediaConfiguration': Failed to read the '${key}' property from '${kind === 'video' ? 'Video' : 'Audio'}Configuration': Required member is undefined.`,
                );
              continue;
            }
            value[key] =
              key === 'contentType' || key === 'channels'
                ? mediaString(v)
                : key === 'width' || key === 'height'
                  ? Number(v) >>> 0
                  : Number(v);
          }
          tracks[kind] = value;
        }
        const rawType = configuration.type;
        if (rawType === undefined)
          fail(
            `Failed to read the 'type' property from '${decode ? 'MediaDecodingConfiguration' : 'MediaEncodingConfiguration'}': Required member is undefined.`,
          );
        const type = mediaString(rawType),
          allowed = decode ? ['file', 'media-source', 'webrtc'] : ['webrtc'];
        if (!allowed.includes(type))
          fail(
            (decode
              ? "Failed to read the 'type' property from 'MediaDecodingConfiguration': "
              : '') +
              `The provided value '${type}' is not a valid enum value of type ${decode ? 'MediaDecodingType' : 'MediaEncodingType'}.`,
          );
        if (!tracks.audio && !tracks.video)
          fail(
            'The configuration dictionary has neither |video| nor |audio| specified and needs at least one of them.',
          );
        let supported = true,
          powerEfficient = true;
        for (const [kind, track] of Object.entries(tracks)) {
          const parsed = host.mediaTypeSupport(track.contentType);
          if (
            !track.contentType.toLowerCase().startsWith(kind + '/') ||
            (kind === 'video' && !(track.framerate > 0))
          )
            fail(`The ${kind} configuration dictionary is not valid.`);
          if (type === 'webrtc') {
            const caps = host.rtpCapabilities(kind, decode ? 'receiver' : 'sender');
            const mime = track.contentType.split(';')[0].trim().toLowerCase();
            const has = caps.codecs.some((c) => c.mimeType.toLowerCase() === mime);
            supported &&= has;
            powerEfficient &&= has && kind === 'audio';
          } else {
            supported &&= parsed.precise && (type !== 'media-source' || parsed.mediaSource);
            powerEfficient &&= parsed.efficient;
          }
        }
        // Blink accepts unsigned zero/wrapped image dimensions; they cannot form
        // a positive signed decoder extent, but do not make the codec unsupported.
        if (
          tracks.video &&
          (!(tracks.video.width > 0) ||
            !(tracks.video.height > 0) ||
            tracks.video.width > 0x7fffffff ||
            tracks.video.height > 0x7fffffff)
        )
          powerEfficient = false;
        const result = {
          powerEfficient: supported && powerEfficient,
          smooth: supported,
          supported,
          keySystemAccess: null,
        };
        // Supported file video asks the decoder capability service for its answer.
        // Keep this completion outside the caller's microtask checkpoint; audio,
        // unsupported configurations and RTP capabilities have no decoder query.
        if (decode && tracks.video && supported && type !== 'webrtc')
          return new platformPromise((resolve) => host.setTimer(() => resolve(result), 0, false));
        return result;
      },
      true,
    );
  attribute(
    'MediaSession',
    'metadata',
    (s) => s.metadata || null,
    (s, v) => {
      if (v !== null && !(v instanceof MediaMetadata)) throw new TypeError('Invalid MediaMetadata');
      s.metadata = v;
    },
  );
  attribute(
    'MediaSession',
    'playbackState',
    (s) => s.playbackState || 'none',
    (s, v) => {
      if (!['none', 'paused', 'playing'].includes(String(v)))
        throw new TypeError('Invalid MediaSessionPlaybackState');
      s.playbackState = String(v);
    },
  );
  method('MediaSession', 'setActionHandler', (s, name, handler) => {
    (s.actions || (s.actions = new Map())).set(String(name), handler);
  });
  method('MediaSession', 'setPositionState', (s, value) => {
    if (
      value &&
      (!(value.duration >= 0) ||
        (value.position || 0) > value.duration ||
        (value.playbackRate ?? 1) === 0)
    )
      throw new TypeError('Invalid position state');
    s.position = value;
  });
  attribute(
    'Presentation',
    'defaultRequest',
    (s) => s.defaultRequest || null,
    (s, v) => {
      if (v !== null && !(v instanceof PresentationRequest))
        throw new TypeError('Invalid PresentationRequest');
      s.defaultRequest = v;
    },
  );
  attribute('Presentation', 'receiver', () => null);

  attribute('ServiceWorkerContainer', 'controller', () => null);
  attribute(
    'ServiceWorkerContainer',
    'ready',
    (s) => s.ready || (s.ready = new platformPromise(() => {})),
  );
  method('ServiceWorkerContainer', 'getRegistrations', () => [], true);
  method('ServiceWorkerContainer', 'getRegistration', () => undefined, true);
  method(
    'ServiceWorkerContainer',
    'register',
    () => {
      throw error('NotSupportedError', 'Service Worker execution backend is unavailable.');
    },
    true,
  );
  method('ServiceWorkerContainer', 'startMessages', () => {});
  method(
    'XRSystem',
    'isSessionSupported',
    (_s, mode) => {
      if (!['inline', 'immersive-vr', 'immersive-ar'].includes(mode))
        throw new TypeError('Invalid XRSessionMode');
      return mode === 'inline';
    },
    true,
  );
  method(
    'XRSystem',
    'requestSession',
    (_s, mode) => {
      if (mode !== 'inline' && !state('activation').isActive)
        throw error(
          'SecurityError',
          "Failed to execute 'requestSession' on 'XRSystem': The requested session requires user activation.",
        );
      throw error('NotSupportedError', 'The specified session configuration is not supported.');
    },
    true,
  );
  method(
    'NavigatorManagedData',
    'getManagedConfiguration',
    () => {
      throw error(
        'NotAllowedError',
        'Managed configuration is empty. This API is available only for managed apps.',
      );
    },
    true,
  );
  method(
    'NavigatorLogin',
    'setStatus',
    (_s, status) => {
      if (!['logged-in', 'logged-out'].includes(status))
        throw new TypeError('Invalid login status');
      state('login', status);
    },
    true,
  );
  method(
    'WakeLock',
    'request',
    () => {
      throw error('NotAllowedError', 'Wake Lock permission request denied');
    },
    true,
  );
  method('ProtectedAudience', 'queryFeatureSupport', () => undefined);
  const rect = () => new DOMRect(0, 0, 0, 0);
  attribute('WindowControlsOverlay', 'visible', () => false);
  method('WindowControlsOverlay', 'getTitlebarAreaRect', rect);
  attribute('VirtualKeyboard', 'boundingRect', (s) => s.rect || (s.rect = rect()));
  attribute(
    'VirtualKeyboard',
    'overlaysContent',
    (s) => !!s.overlaysContent,
    (s, v) => (s.overlaysContent = !!v),
  );
  method('VirtualKeyboard', 'show', () => {});
  method('VirtualKeyboard', 'hide', () => {});
  method('Keyboard', 'unlock', () => {});
  method(
    'Keyboard',
    'lock',
    () => {
      if (permission('keyboard-lock') === 'prompt' && state('presentation').mode === 'headful')
        return permissionPrompt();
      throw error('InvalidStateError', 'lock() request could not be registered.');
    },
    true,
  );
  method(
    'Keyboard',
    'getLayoutMap',
    () => create('KeyboardLayoutMap', { map: new Map(Object.entries(state('keyboard') || {})) }),
    true,
  );
  attribute('KeyboardLayoutMap', 'size', (s) => s.map.size);
  for (const name of ['get', 'has', 'keys', 'values', 'entries'])
    method('KeyboardLayoutMap', name, (s, ...args) => s.map[name](...args));
  method('KeyboardLayoutMap', 'forEach', function (s, callback, thisArg) {
    if (typeof callback !== 'function') throw new TypeError('The callback is not a function.');
    s.map.forEach((v, k) => callback.call(thisArg, v, k, this));
  });
  if (globalThis.KeyboardLayoutMap) {
    const iterator = function () {
      return check(this, 'KeyboardLayoutMap').map.entries();
    };
    markNative(iterator, 'entries');
    Object.defineProperty(KeyboardLayoutMap.prototype, Symbol.iterator, {
      value: iterator,
      writable: true,
      configurable: true,
    });
  }
  method(
    'Ink',
    'requestPresenter',
    (_s, options = {}) =>
      create('DelegatedInkTrailPresenter', { presentationArea: options.presentationArea || null }),
    true,
  );
  attribute('DelegatedInkTrailPresenter', 'presentationArea', (s) => s.presentationArea);
  attribute('DelegatedInkTrailPresenter', 'expectedImprovement', () => 0);
  method('DelegatedInkTrailPresenter', 'updateInkTrailStartPoint', () => {
    throw error('NotSupportedError', 'Ink rendering backend is unavailable.');
  });
};
