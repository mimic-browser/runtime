// Stateful HTML form values. DOM attributes remain in the canonical node store;
// dirty value/checked/selected state belongs to the realm's element identity.
{
  const inputs = new WeakMap(),
    textareas = new WeakMap(),
    options = new WeakMap(),
    selects = new WeakMap();
  const nativeStateControls = new Set();
  compatibilityElementState.nativeStateControls = () => nativeStateControls;
  // Native producer admission observes these same private slots, never public
  // getters or rewritten default attributes. Empty textareas remain supported.
  compatibilityElementState.nativeControlContentUnsupported = (element) => {
    const slot = elementSlot(element);
    if (slot?.tagName === 'TEXTAREA') {
      const state = textareas.get(element);
      return (state?.dirty ? state.value : canonicalTextContent(slot.nodeId) || '') !== '';
    }
    if (slot?.tagName === 'INPUT') {
      const state = inputs.get(element);
      const type = (host.getAttribute(slot.nodeId, 'type') || 'text').toLowerCase();
      return (
        !['text', 'search', 'email', 'url', 'tel', 'password'].includes(type) &&
        !!state?.dirty &&
        state.value !== (host.getAttribute(slot.nodeId, 'value') || '')
      );
    }
    return false;
  };
  compatibilityElementState.nativeControlValue = (element) => {
    const slot = elementSlot(element);
    if (slot?.tagName !== 'INPUT') return null;
    const type = (host.getAttribute(slot.nodeId, 'type') || 'text').toLowerCase();
    if (!['text', 'search', 'email', 'url', 'tel', 'password'].includes(type)) return null;
    const state = inputs.get(element);
    return {
      id: slot.nodeId,
      value: state?.dirty ? state.value : host.getAttribute(slot.nodeId, 'value') || '',
    };
  };
  const inputState = (e) => {
    let s = inputs.get(e);
    if (!s) {
      s = {
        dirty: false,
        value: '',
        dirtyChecked: false,
        checked: false,
        indeterminate: false,
        start: 0,
        end: 0,
        direction: 'none',
      };
      inputs.set(e, s);
      nativeStateControls.add(e);
    }
    return s;
  };
  const textareaState = (e) => {
    let s = textareas.get(e);
    if (!s) {
      s = { dirty: false, value: '', start: 0, end: 0, direction: 'none' };
      textareas.set(e, s);
    }
    return s;
  };
  let nextOptionID = 0;
  const optionState = (e) => {
    let s = options.get(e);
    if (!s) {
      s = { dirty: false, selected: false, id: ++nextOptionID };
      options.set(e, s);
      nativeStateControls.add(e);
    }
    return s;
  };
  const selectState = (e) => {
    let s = selects.get(e);
    if (!s) {
      s = { noSelection: false, signature: '' };
      selects.set(e, s);
    }
    return s;
  };
  const controlDescriptors = new Map(),
    parseControlJSON = JSON.parse,
    stringifyControlJSON = JSON.stringify;
  let isolatedControls = host.isIsolatedInputWorld();
  bootstrapRestoreHooks.push(() => {
    isolatedControls = host.isIsolatedInputWorld();
  });
  const sharedControlKeys = new Set([
    'type',
    'value',
    'checked',
    'indeterminate',
    'selected',
    'selectedIndex',
    'selectionStart',
    'selectionEnd',
    'selectionDirection',
    'setSelectionRange',
    'select',
    'reset',
    'setCustomValidity',
    'willValidate',
    'validationMessage',
    'checkValidity',
    'reportValidity',
  ]);
  const define = (name, key, descriptor) => {
    /* dev_preview_form_revision */
    const p = globalThis[name]?.prototype;
    if (!p) return;
    // Reflected attributes already advance the canonical DOM revision. Only
    // dirty JS-owned form state needs an additional invalidation boundary;
    // read-only methods (notably select.item) must not invalidate observations.
    if (sharedControlKeys.has(key) && descriptor.set) {
      const set = descriptor.set;
      descriptor = {
        ...descriptor,
        set(value) {
          host.invalidateStyleObservations();
          try {
            return set.call(this, value);
          } finally {
            host.invalidateStyleObservations();
          }
        },
      };
    }
    if (
      sharedControlKeys.has(key) &&
      descriptor.value &&
      !['checkValidity', 'reportValidity'].includes(key)
    ) {
      const call = descriptor.value;
      descriptor = {
        ...descriptor,
        value: function (...args) {
          host.invalidateStyleObservations();
          try {
            return call.apply(this, args);
          } finally {
            host.invalidateStyleObservations();
          }
        },
      };
    }
    let entries = controlDescriptors.get(p);
    if (!entries) {
      entries = new Map();
      controlDescriptors.set(p, entries);
    }
    entries.set(key, descriptor);
    if (sharedControlKeys.has(key)) {
      const original = descriptor;
      const invoke = (receiver, operation, args) =>
        isolatedControls
          ? parseControlJSON(
              host.mainWorldInput(
                elementSlot(receiver).nodeId,
                'form',
                stringifyControlJSON({ operation, key, args }),
              ),
            ).value
          : Reflect.apply(
              operation === 'get'
                ? original.get
                : operation === 'set'
                  ? original.set
                  : original.value,
              receiver,
              args,
            );
      descriptor = {
        ...descriptor,
        ...(descriptor.get
          ? {
              get() {
                return invoke(this, 'get', []);
              },
            }
          : {}),
        ...(descriptor.set
          ? {
              set(value) {
                invoke(this, 'set', [value]);
              },
            }
          : {}),
        ...(descriptor.value
          ? {
              value: function (...args) {
                return invoke(this, 'call', args);
              },
            }
          : {}),
      };
    }
    Object.defineProperty(p, key, { ...descriptor, enumerable: true, configurable: true });
  };
  compatibilityElementState.formOperation = (element, operation, key, args = []) => {
    if (key === '$validity') return validityFlags(element);
    if (key === '$controlEdited') {
      compatibilityElementState.controlEdited(element, args[0]);
      return null;
    }
    if (key === '$editValue') return compatibilityElementState.controlEditValue(element);
    for (let p = Object.getPrototypeOf(element); p; p = Object.getPrototypeOf(p)) {
      const descriptor = controlDescriptors.get(p)?.get(key);
      if (!descriptor) continue;
      const method =
        operation === 'get'
          ? descriptor.get
          : operation === 'set'
            ? descriptor.set
            : descriptor.value;
      if (typeof method !== 'function') throw new TypeError('Unsupported form control operation');
      return Reflect.apply(method, element, args);
    }
    throw new TypeError('Unsupported form control property ' + key);
  };
  compatibilityElementState.selectorChecked = (element) => {
    const data = elementSlot(element);
    if (!data) return false;
    if (data.tagName === 'INPUT') {
      const type = String(host.getAttribute(data.nodeId, 'type') || 'text').toLowerCase();
      if (type !== 'checkbox' && type !== 'radio') return false;
      if (isolatedControls)
        return !!parseControlJSON(
          host.mainWorldInput(
            data.nodeId,
            'form',
            stringifyControlJSON({ operation: 'get', key: 'checked', args: [] }),
          ),
        ).value;
      const state = inputState(element);
      return state.dirtyChecked
        ? state.checked
        : host.getAttribute(data.nodeId, 'checked') !== null;
    }
    if (data.tagName === 'OPTION')
      return !!compatibilityElementState.formOperation(element, 'get', 'selected');
    return false;
  };
  const string = (name, key, attribute = key.toLowerCase()) =>
    define(name, key, {
      get() {
        return this.getAttribute(attribute) || '';
      },
      set(value) {
        this.setAttribute(attribute, String(value));
      },
    });
  const boolean = (name, key, attribute = key.toLowerCase()) =>
    define(name, key, {
      get() {
        return this.hasAttribute(attribute);
      },
      set(value) {
        if (value) this.setAttribute(attribute, '');
        else this.removeAttribute(attribute);
      },
    });
  const lf = (value) => String(value).replace(/\r\n?/g, '\n');
  const childText = (e) =>
    Array.from(e.childNodes)
      .filter((node) => node.nodeType === 3)
      .map((node) => node.data)
      .join('');
  const valueTypes = new Set([
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
  ]);
  const selectionTypes = new Set(['text', 'search', 'tel', 'url', 'password']);
  const types = new Set([
    ...valueTypes,
    'hidden',
    'checkbox',
    'radio',
    'file',
    'submit',
    'image',
    'reset',
    'button',
  ]);
  const typeOf = (e) => {
    const value = (e.getAttribute('type') || 'text').toLowerCase();
    return types.has(value) ? value : 'text';
  };
  const labelable = (e) =>
    !!e &&
    (['button', 'meter', 'output', 'progress', 'select', 'textarea'].includes(e.localName) ||
      (e.localName === 'input' && typeOf(e) !== 'hidden'));
  const labelControl = (label) => {
    const id = label.getAttribute('for');
    if (id !== null) {
      const candidate = label.getRootNode().getElementById(id);
      return labelable(candidate) ? candidate : null;
    }
    return Array.from(label.querySelectorAll('*')).find(labelable) || null;
  };
  define('HTMLLabelElement', 'control', {
    get() {
      return labelControl(this);
    },
  });
  const labelLists = new WeakMap();
  for (const name of [
    'HTMLButtonElement',
    'HTMLInputElement',
    'HTMLMeterElement',
    'HTMLOutputElement',
    'HTMLProgressElement',
    'HTMLSelectElement',
    'HTMLTextAreaElement',
  ]) {
    define(name, 'labels', {
      get() {
        if (!labelable(this)) return null;
        if (!labelLists.has(this)) {
          const read = () =>
            Array.from(this.getRootNode().querySelectorAll('label')).filter(
              (label) => labelControl(label) === this,
            );
          labelLists.set(
            this,
            nodeListView(
              () => read().length,
              (index) => read()[index],
            ),
          );
        }
        return labelLists.get(this);
      },
    });
  }
  for (const name of ['HTMLFieldSetElement', 'HTMLOutputElement', 'HTMLObjectElement'])
    define(name, 'form', {
      get() {
        return formOwner(this);
      },
    });
  const mode = (e) => {
    const type = typeOf(e);
    return valueTypes.has(type)
      ? 'value'
      : type === 'file'
        ? 'filename'
        : ['checkbox', 'radio'].includes(type)
          ? 'default-on'
          : 'default';
  };
  function sanitize(e, value) {
    const type = typeOf(e);
    value = String(value);
    if (['text', 'search', 'tel', 'password'].includes(type)) return value.replace(/[\r\n]/g, '');
    if (['url', 'email'].includes(type)) {
      value = value.replace(/[\r\n]/g, '').replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '');
      if (type === 'email' && e.multiple)
        value = value
          .split(',')
          .map((x) => x.trim())
          .join(',');
      return value;
    }
    if (type === 'number')
      return /^-?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value) &&
        Number.isFinite(Number(value))
        ? value
        : '';
    if (type === 'color') return /^#[\da-f]{6}$/i.test(value) ? value.toLowerCase() : '#000000';
    if (type === 'range') {
      const min = Number(e.getAttribute('min') ?? 0),
        rawMax = Number(e.getAttribute('max') ?? 100),
        max = Math.max(min, rawMax);
      let number = value !== '' && Number.isFinite(Number(value)) ? Number(value) : (min + max) / 2;
      number = Math.max(min, Math.min(max, number));
      return String(number);
    }
    if (['date', 'time', 'datetime-local'].includes(type)) {
      const validDate = (text) => {
        const match = /^(\d{4,})-(\d{2})-(\d{2})$/.exec(text);
        if (!match || +match[1] === 0) return false;
        const date = new Date(0);
        date.setUTCFullYear(+match[1], +match[2] - 1, +match[3]);
        return (
          date.getUTCFullYear() === +match[1] &&
          date.getUTCMonth() === +match[2] - 1 &&
          date.getUTCDate() === +match[3]
        );
      };
      const validTime = (text) => {
        const match = /^(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/.exec(text);
        return (
          match && +match[1] < 24 && +match[2] < 60 && (match[3] === undefined || +match[3] < 60)
        );
      };
      if (type === 'date') return validDate(value) ? value : '';
      if (type === 'time') return validTime(value) ? value : '';
      const parts = value.split(/[T ]/);
      if (parts.length !== 2 || !validDate(parts[0]) || !validTime(parts[1])) return '';
      let time = parts[1]
        .replace(/(\.\d*?)0+$/, '$1')
        .replace(/\.$/, '')
        .replace(/^(\d{2}:\d{2}):00$/, '$1');
      return parts[0] + 'T' + time;
    }
    if (type === 'month' || type === 'week')
      return Number.isFinite(numericValue(type, value)) ? value : '';
    return value;
  }
  const formOwner = (e) => {
    const id = e.getAttribute('form');
    if (id !== null) {
      const owner = e.ownerDocument.getElementById(id);
      return owner?.localName === 'form' ? owner : null;
    }
    for (let p = e.parentElement; p; p = p.parentElement) if (p.localName === 'form') return p;
    return null;
  };
  const inputValue = (e) => {
    const s = inputState(e),
      m = mode(e);
    if (m === 'filename') return '';
    if (m === 'default-on') return e.getAttribute('value') ?? 'on';
    if (m === 'default') return e.getAttribute('value') ?? '';
    return sanitize(e, s.dirty ? s.value : (e.getAttribute('value') ?? ''));
  };
  define('HTMLInputElement', 'type', {
    get() {
      return typeOf(this);
    },
    set(value) {
      const oldMode = mode(this),
        oldValue = inputValue(this);
      this.setAttribute('type', String(value));
      const next = mode(this),
        s = inputState(this);
      if (oldMode === 'value' && next !== 'value' && oldValue !== '')
        this.setAttribute('value', oldValue);
      if (oldMode !== 'value' && next === 'value') {
        s.dirty = false;
        s.value = '';
      }
      if (next === 'filename') {
        s.dirty = false;
        s.value = '';
      }
    },
  });
  define('HTMLInputElement', 'value', {
    get() {
      return inputValue(this);
    },
    set(value) {
      compatibilityElementState.controlValueAssigned?.(this);
      value = value === null ? '' : String(value);
      const m = mode(this),
        s = inputState(this);
      if (m === 'filename') {
        if (value !== '')
          throw platformDOMException('File input value can only be cleared', 'InvalidStateError');
        s.value = '';
        return;
      }
      if (m !== 'value') {
        this.setAttribute('value', value);
        return;
      }
      s.dirty = true;
      s.value = sanitize(this, value);
      s.start = s.end = s.value.length;
      s.direction = 'none';
    },
  });
  string('HTMLInputElement', 'defaultValue', 'value');
  define('HTMLInputElement', 'checked', {
    get() {
      const s = inputState(this);
      return s.dirtyChecked ? s.checked : this.hasAttribute('checked');
    },
    set(value) {
      const s = inputState(this);
      s.dirtyChecked = true;
      s.checked = Boolean(value);
      if (s.checked && typeOf(this) === 'radio' && this.name) {
        const root = this.getRootNode();
        for (const other of compatibilitySelectors.query(root, 'input'))
          if (
            other !== this &&
            typeOf(other) === 'radio' &&
            other.name === this.name &&
            formOwner(other) === formOwner(this)
          ) {
            const state = inputState(other);
            state.dirtyChecked = true;
            state.checked = false;
          }
      }
    },
  });
  boolean('HTMLInputElement', 'defaultChecked', 'checked');
  define('HTMLInputElement', 'indeterminate', {
    get() {
      return inputState(this).indeterminate;
    },
    set(value) {
      inputState(this).indeterminate = Boolean(value);
    },
  });
  define('HTMLTextAreaElement', 'value', {
    get() {
      const s = textareaState(this);
      return s.dirty ? s.value : lf(childText(this));
    },
    set(value) {
      compatibilityElementState.controlValueAssigned?.(this);
      const s = textareaState(this);
      s.dirty = true;
      s.value = lf(value === null ? '' : value);
      s.start = s.end = s.value.length;
      s.direction = 'none';
    },
  });
  define('HTMLTextAreaElement', 'defaultValue', {
    get() {
      return childText(this);
    },
    set(value) {
      this.textContent = String(value);
    },
  });
  define('HTMLTextAreaElement', 'type', {
    get() {
      return 'textarea';
    },
  });
  define('HTMLTextAreaElement', 'textLength', {
    get() {
      return this.value.length;
    },
  });
  for (const name of ['HTMLInputElement', 'HTMLTextAreaElement']) {
    for (const property of ['selectionStart', 'selectionEnd', 'selectionDirection'])
      define(name, property, {
        get() {
          if (name === 'HTMLInputElement' && !selectionTypes.has(typeOf(this))) return null;
          const s = name === 'HTMLInputElement' ? inputState(this) : textareaState(this);
          return s[
            property === 'selectionStart'
              ? 'start'
              : property === 'selectionEnd'
                ? 'end'
                : 'direction'
          ];
        },
        set(value) {
          const s = name === 'HTMLInputElement' ? inputState(this) : textareaState(this);
          this.setSelectionRange(
            property === 'selectionStart' ? value : s.start,
            property === 'selectionEnd' ? value : s.end,
            property === 'selectionDirection' ? value : s.direction,
          );
        },
      });
    define(name, 'setSelectionRange', {
      value: function (start, end, direction = 'none') {
        if (name === 'HTMLInputElement' && !selectionTypes.has(typeOf(this)))
          throw platformDOMException('Input does not support selection', 'InvalidStateError');
        const s = name === 'HTMLInputElement' ? inputState(this) : textareaState(this);
        s.end = Math.min(Number(end) >>> 0, this.value.length);
        s.start = Math.min(Number(start) >>> 0, s.end);
        s.direction = ['forward', 'backward'].includes(String(direction))
          ? String(direction)
          : 'none';
      },
      writable: true,
    });
    define(name, 'select', {
      value: function () {
        if (name === 'HTMLInputElement' && !selectionTypes.has(typeOf(this))) return;
        this.setSelectionRange(0, this.value.length);
        this.dispatchEvent(new Event('select', { bubbles: true }));
      },
      writable: true,
    });
  }
  const optionList = (select) =>
    Array.from(compatibilitySelectors.query(select, 'option')).filter((option) => {
      for (let p = option.parentElement; p; p = p.parentElement) {
        if (p.localName === 'select') return p === select;
      }
      return false;
    });
  const selectOwner = (option) => {
    for (let p = option.parentElement; p; p = p.parentElement) {
      if (p.localName === 'select') return p;
    }
    return null;
  };
  const optionDisabled = (option) =>
    option.disabled ||
    (option.parentElement?.localName === 'optgroup' &&
      option.parentElement.hasAttribute('disabled'));
  function selection(select) {
    if (isolatedControls) {
      const list = optionList(select);
      return { list, selected: list.filter((option) => option.selected) };
    }
    const list = optionList(select),
      state = selectState(select),
      signature = list
        .map((option) => String(optionState(option).id) + ':' + option.hasAttribute('selected'))
        .join(',');
    if (signature !== state.signature) {
      state.noSelection = false;
      state.signature = signature;
    }
    let selected = list.filter((option) => {
      const s = optionState(option);
      return s.dirty ? s.selected : option.hasAttribute('selected');
    });
    if (!select.multiple) {
      if (selected.length > 1) selected = selected.slice(-1);
      if (!selected.length && !state.noSelection && Number(select.size) <= 1) {
        const first = list.find((option) => !optionDisabled(option));
        if (first) selected = [first];
      }
    }
    return { list, selected };
  }
  function assignSelection(select, chosen) {
    const { list } = selection(select);
    for (const option of list) {
      const s = optionState(option);
      s.dirty = true;
      s.selected = chosen.includes(option);
    }
    selectState(select).noSelection = chosen.length === 0;
  }
  const radioNodeList = (get) => {
    const proxy = nodeListView(
      () => get().length,
      (index) => get()[index],
    );
    if (globalThis.RadioNodeList?.prototype)
      Object.setPrototypeOf(proxy, globalThis.RadioNodeList.prototype);
    Object.defineProperty(proxy, 'value', {
      get() {
        const checked = get().find(
          (e) => e.localName === 'input' && ['radio', 'checkbox'].includes(typeOf(e)) && e.checked,
        );
        return checked?.value ?? '';
      },
      set(value) {
        value = String(value);
        const match = get().find(
          (e) =>
            e.localName === 'input' &&
            ['radio', 'checkbox'].includes(typeOf(e)) &&
            e.value === value,
        );
        if (match) match.checked = true;
      },
      enumerable: true,
      configurable: true,
    });
    return proxy;
  };
  const collection = (get, prototype, formControls = false) => {
    const matches = (name) =>
      name === '' ? [] : get().filter((e) => e.id === name || e.getAttribute('name') === name);
    const namedItem = (name) => {
      const found = matches(name);
      return formControls && found.length > 1
        ? radioNodeList(() => matches(name))
        : (found[0] ?? null);
    };
    const proxy = new Proxy(Object.create(prototype), {
      get(target, key, receiver) {
        if (key === 'length') return get().length;
        if (key === 'item') return HTMLCollection.prototype.item;
        if (key === 'namedItem') return (name) => namedItem(bindingString(name));
        if (collectionIndex(key)) return get()[Number(key)];
        if (typeof key === 'string') {
          const named = namedItem(key);
          if (named !== null) return named;
        }
        return Reflect.get(target, key, receiver);
      },
      has(target, key) {
        return (
          (collectionIndex(key) && Number(key) < get().length) ||
          (typeof key === 'string' && namedItem(key) !== null) ||
          Reflect.has(target, key)
        );
      },
    });
    // Derived form collections also implement the HTMLCollection interface.
    // Register their private live source for borrowed base-interface methods.
    registerRealmBinding(proxy, 'HTMLCollection', {
      length: () => get().length,
      item: (index) => get()[index] ?? null,
      namedItem,
    });
    return proxy;
  };
  define('HTMLOptionElement', 'text', {
    get() {
      return this.textContent.replace(/[\t\n\f\r ]+/g, ' ').trim();
    },
    set(value) {
      this.textContent = String(value);
    },
  });
  define('HTMLOptionElement', 'value', {
    get() {
      return this.getAttribute('value') ?? this.text;
    },
    set(value) {
      this.setAttribute('value', String(value));
    },
  });
  define('HTMLOptionElement', 'label', {
    get() {
      return this.getAttribute('label') ?? this.text;
    },
    set(value) {
      this.setAttribute('label', String(value));
    },
  });
  boolean('HTMLOptionElement', 'defaultSelected', 'selected');
  boolean('HTMLOptionElement', 'disabled');
  define('HTMLOptionElement', 'selected', {
    get() {
      const owner = selectOwner(this);
      if (owner) return selection(owner).selected.includes(this);
      const s = optionState(this);
      return s.dirty ? s.selected : this.hasAttribute('selected');
    },
    set(value) {
      const owner = selectOwner(this),
        s = optionState(this);
      s.dirty = true;
      s.selected = Boolean(value);
      if (owner && s.selected) {
        if (!owner.multiple) assignSelection(owner, [this]);
        else selectState(owner).noSelection = false;
      }
    },
  });
  define('HTMLOptionElement', 'index', {
    get() {
      const owner = selectOwner(this);
      return owner ? optionList(owner).indexOf(this) : 0;
    },
  });
  define('HTMLSelectElement', 'type', {
    get() {
      return this.multiple ? 'select-multiple' : 'select-one';
    },
  });
  define('HTMLSelectElement', 'size', {
    get() {
      const value = Number(this.getAttribute('size'));
      return Number.isInteger(value) && value >= 0 ? value : 0;
    },
    set(value) {
      this.setAttribute('size', String(Number(value) >>> 0));
    },
  });
  define('HTMLSelectElement', 'options', {
    get() {
      const owner = this,
        base = collection(
          () => optionList(owner),
          globalThis.HTMLOptionsCollection?.prototype || globalThis.HTMLCollection.prototype,
        ),
        proxy = new Proxy(base, {
          get(target, key, receiver) {
            if (key === 'selectedIndex') return owner.selectedIndex;
            if (key === 'add') return (...args) => owner.add(...args);
            if (key === 'remove') return (index) => owner.remove(index);
            return Reflect.get(target, key, receiver);
          },
          set(target, key, value) {
            if (key === 'selectedIndex') {
              owner.selectedIndex = value;
              return true;
            }
            if (key === 'length') {
              owner.length = value;
              return true;
            }
            return Reflect.set(target, key, value);
          },
        });
      bindingSet(proxy, bindingGet(base));
      return proxy;
    },
  });
  define('HTMLSelectElement', 'selectedOptions', {
    get() {
      return collection(() => selection(this).selected, globalThis.HTMLCollection.prototype);
    },
  });
  define('HTMLSelectElement', 'length', {
    get() {
      return optionList(this).length;
    },
    set(value) {
      value = Number(value) >>> 0;
      if (value > 100000) throw platformDOMException('Too many options', 'IndexSizeError');
      const list = optionList(this);
      while (list.length > value) list.pop().remove();
      while (list.length < value) {
        const option = document.createElement('option');
        this.appendChild(option);
        list.push(option);
      }
    },
  });
  define('HTMLSelectElement', 'selectedIndex', {
    get() {
      const s = selection(this);
      return s.selected.length ? s.list.indexOf(s.selected[0]) : -1;
    },
    set(value) {
      const list = optionList(this),
        index = Number(value) | 0;
      assignSelection(this, index >= 0 && index < list.length ? [list[index]] : []);
    },
  });
  define('HTMLSelectElement', 'value', {
    get() {
      return selection(this).selected[0]?.value ?? '';
    },
    set(value) {
      const found = optionList(this).find((option) => option.value === String(value));
      assignSelection(this, found ? [found] : []);
    },
  });
  define('HTMLSelectElement', 'item', {
    value: function (index) {
      return optionList(this)[Number(index)] ?? null;
    },
    writable: true,
  });
  define('HTMLSelectElement', 'add', {
    value: function (element, before) {
      if (!['option', 'optgroup'].includes(element?.localName))
        throw new TypeError('Expected option or optgroup');
      if (typeof before === 'number') before = optionList(this)[before] ?? null;
      if (before) before.parentNode.insertBefore(element, before);
      else this.appendChild(element);
    },
    writable: true,
  });
  const removeElement = globalThis.Element.prototype.remove;
  define('HTMLSelectElement', 'remove', {
    value: function (index) {
      if (arguments.length === 0) return removeElement.call(this);
      const option = optionList(this)[Number(index) | 0];
      if (option) option.remove();
    },
    writable: true,
  });
  for (const name of [
    'HTMLInputElement',
    'HTMLTextAreaElement',
    'HTMLSelectElement',
    'HTMLButtonElement',
  ]) {
    string(name, 'name');
    boolean(name, 'disabled');
    boolean(name, 'required');
    define(name, 'form', {
      get() {
        return formOwner(this);
      },
    });
  }
  boolean('HTMLFieldSetElement', 'disabled');
  for (const name of ['HTMLInputElement', 'HTMLTextAreaElement']) {
    string(name, 'placeholder');
    boolean(name, 'readOnly', 'readonly');
    for (const property of ['minLength', 'maxLength'])
      define(name, property, {
        get() {
          const raw = this.getAttribute(property.toLowerCase());
          return raw !== null && /^\d+$/.test(raw) ? Number(raw) : -1;
        },
        set(value) {
          value = Number(value);
          if (!Number.isInteger(value) || value < 0)
            throw platformDOMException('The value must be non-negative.', 'IndexSizeError');
          this.setAttribute(property.toLowerCase(), String(value));
        },
      });
  }
  for (const property of ['min', 'max', 'step', 'pattern']) string('HTMLInputElement', property);
  for (const name of ['HTMLInputElement', 'HTMLSelectElement']) boolean(name, 'multiple');
  const formCandidates = (form, selector) =>
    Array.from(
      compatibilitySelectors.query(form.isConnected ? form.ownerDocument : form, selector),
    );
  const tableRows = new WeakMap();
  define('HTMLTableElement', 'rows', {
    get() {
      if (!tableRows.has(this))
        tableRows.set(
          this,
          collection(
            () =>
              Array.from(compatibilitySelectors.query(this, 'tr')).filter((row) => {
                for (let parent = row.parentElement; parent; parent = parent.parentElement)
                  if (parent.localName === 'table') return parent === this;
                return false;
              }),
            globalThis.HTMLCollection.prototype,
          ),
        );
      return tableRows.get(this);
    },
  });
  const associated = (form) =>
    formCandidates(form, 'button,fieldset,input,object,output,select,textarea').filter(
      (e) => formOwner(e) === form && !(e.localName === 'input' && typeOf(e) === 'image'),
    );
  const submittable = (form) =>
    formCandidates(form, 'button,input,select,textarea').filter((e) => formOwner(e) === form);
  const isSubmitButton = (control) =>
    control?.localName === 'button'
      ? !['reset', 'button'].includes(
          String(control.getAttribute('type') || 'submit').toLowerCase(),
        )
      : control?.localName === 'input' && ['submit', 'image'].includes(typeOf(control));
  const disabledForForm = (control) => {
    if (control.hasAttribute('disabled')) return true;
    for (let ancestor = control.parentElement; ancestor; ancestor = ancestor.parentElement) {
      if (ancestor.localName === 'datalist') return true;
      if (ancestor.localName === 'fieldset' && ancestor.hasAttribute('disabled')) {
        const legend = Array.from(ancestor.children).find((e) => e.localName === 'legend');
        if (!legend || !legend.contains(control)) return true;
      }
    }
    return false;
  };
  /* constraint_validation */
  const formCollections = new WeakMap();
  define('HTMLFormElement', 'elements', {
    get() {
      formCheck(this);
      if (!formCollections.has(this))
        formCollections.set(
          this,
          collection(
            () => associated(this),
            globalThis.HTMLFormControlsCollection?.prototype || globalThis.HTMLCollection.prototype,
            true,
          ),
        );
      return formCollections.get(this);
    },
  });
  define('HTMLFormElement', 'length', {
    get() {
      return associated(this).length;
    },
  });
  const formCheck = (form) => {
    if (!elementSlot(form) || form.localName !== 'form') throw new TypeError('Illegal invocation');
    return form;
  };
  const formEnum = (name, values, fallback) =>
    define('HTMLFormElement', name, {
      get() {
        formCheck(this);
        const value = (this.getAttribute(name) || '').toLowerCase();
        return values.includes(value) ? value : fallback;
      },
      set(value) {
        formCheck(this);
        this.setAttribute(name, String(value));
      },
    });
  formEnum('method', ['get', 'post', 'dialog'], 'get');
  formEnum(
    'enctype',
    ['application/x-www-form-urlencoded', 'multipart/form-data', 'text/plain'],
    'application/x-www-form-urlencoded',
  );
  define('HTMLFormElement', 'encoding', {
    get() {
      return this.enctype;
    },
    set(value) {
      this.enctype = value;
    },
  });
  define('HTMLFormElement', 'action', {
    get() {
      formCheck(this);
      const raw = this.getAttribute('action');
      if (!raw) return document.URL;
      try {
        return new URL(raw, document.baseURI).href;
      } catch {
        return raw;
      }
    },
    set(value) {
      formCheck(this);
      this.setAttribute('action', String(value));
    },
  });
  string('HTMLFormElement', 'name');
  string('HTMLFormElement', 'target');
  string('HTMLFormElement', 'acceptCharset', 'accept-charset');
  string('HTMLButtonElement', 'value');
  const formEntries = (form, submitter = null) => {
    formCheck(form);
    const entries = [],
      crlf = (value) => String(value).replace(/\r\n|\r|\n/g, '\r\n');
    for (const control of submittable(form)) {
      const name = control.name;
      if (
        !name ||
        disabledForForm(control) ||
        (control.localName === 'button' && control !== submitter)
      )
        continue;
      if (control.localName === 'input') {
        const type = typeOf(control);
        if (
          (['submit', 'image', 'reset', 'button'].includes(type) && control !== submitter) ||
          (['checkbox', 'radio'].includes(type) && !control.checked)
        )
          continue;
        if (type === 'file') {
          const files = control.files;
          if (files?.length) for (const file of files) entries.push([name, file]);
          else entries.push([name, new File([], '', { type: 'application/octet-stream' })]);
          continue;
        }
      }
      if (control.localName === 'select') {
        for (const option of optionList(control))
          if (option.selected && !optionDisabled(option))
            entries.push([crlf(name), crlf(option.value)]);
      } else
        entries.push([
          crlf(name),
          crlf(
            control.localName === 'input' && typeOf(control) === 'hidden' && name === '_charset_'
              ? 'UTF-8'
              : control.value,
          ),
        ]);
    }
    return entries;
  };
  const formDataEventSlots = new WeakMap();
  class MimicFormDataEvent extends Event {
    constructor(type, init) {
      super(type, init || {});
      if (!init || !('formData' in init)) throw new TypeError("FormDataEvent requires 'formData'");
      if (!(init.formData instanceof FormData)) throw new TypeError('formData is not a FormData');
      formDataEventSlots.set(this, init.formData);
    }
  }
  Object.defineProperty(MimicFormDataEvent, 'name', { value: 'FormDataEvent' });
  Object.defineProperty(MimicFormDataEvent.prototype, 'formData', {
    get() {
      if (!formDataEventSlots.has(this)) throw new TypeError('Illegal invocation');
      return formDataEventSlots.get(this);
    },
    enumerable: true,
    configurable: true,
  });
  Object.defineProperty(MimicFormDataEvent.prototype, Symbol.toStringTag, {
    value: 'FormDataEvent',
    configurable: true,
  });
  Object.defineProperty(globalThis, 'FormDataEvent', {
    value: MimicFormDataEvent,
    writable: true,
    configurable: true,
  });
  markNative(MimicFormDataEvent, 'FormDataEvent');
  installFormDataConstruction(
    (form, submitter) => {
      formCheck(form);
      if (submitter !== undefined) {
        if (!isSubmitButton(submitter))
          throw new TypeError('The specified element is not a submit button.');
        if (formOwner(submitter) !== form)
          throw platformDOMException(
            'The specified element is not owned by this form element.',
            'NotFoundError',
          );
      } else submitter = null;
      return formEntries(form, submitter);
    },
    (form, data) => {
      form.dispatchEvent(new MimicFormDataEvent('formdata', { formData: data }));
    },
  );
  const submitForm = function (submitter = null) {
    formCheck(this);
    if (!this.isConnected) return;
    const missing = (reason) => {
      host.semanticMissingAt('form_controls.js:submit', 'HTMLFormElement.submit', reason);
      throw platformDOMException(reason, 'NotSupportedError');
    };
    const override = (attribute, fallback) =>
      submitter?.hasAttribute(attribute) ? submitter.getAttribute(attribute) : fallback;
    const target = override('formtarget', this.target).toLowerCase();
    if (target && target !== '_self' && !(target === '_top' && window.top === window))
      return missing('Named, parent, and new browsing-context form targets are not implemented');
    let method = override('formmethod', this.method).toLowerCase();
    if (!['get', 'post', 'dialog'].includes(method)) method = 'get';
    if (method === 'dialog') return missing('Dialog form submission is not implemented');
    const data = new FormData(this, submitter || undefined),
      entries = Array.from(data.entries());
    let action;
    try {
      action = new URL(override('formaction', this.action) || document.URL, document.baseURI);
    } catch {
      return;
    }
    let body = '',
      type = override('formenctype', this.enctype).toLowerCase();
    if (!['application/x-www-form-urlencoded', 'multipart/form-data', 'text/plain'].includes(type))
      type = 'application/x-www-form-urlencoded';
    if (method === 'get') {
      action.search = new URLSearchParams(entries).toString();
    } else if (type === 'application/x-www-form-urlencoded')
      body = new URLSearchParams(entries).toString();
    else if (type === 'text/plain')
      body = entries.map(([name, value]) => name + '=' + value + '\r\n').join('');
    else {
      const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789',
        bytes = crypto.getRandomValues(new Uint8Array(16)),
        boundary =
          '----WebKitFormBoundary' +
          Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
      const quoted = (value) =>
        value.replace(/\r/g, '%0D').replace(/\n/g, '%0A').replace(/"/g, '%22');
      body =
        entries
          .map(
            ([name, value]) =>
              '--' +
              boundary +
              '\r\nContent-Disposition: form-data; name="' +
              quoted(name) +
              '"\r\n\r\n' +
              value +
              '\r\n',
          )
          .join('') +
        '--' +
        boundary +
        '--\r\n';
      type += '; boundary=' + boundary;
    }
    const reason = host.submitForm(action.href, method.toUpperCase(), body, type);
    if (reason) return missing(reason);
  };
  define('HTMLFormElement', 'submit', {
    value: function submit() {
      submitForm.call(this);
    },
    writable: true,
  });
  define('HTMLFormElement', 'requestSubmit', {
    value: function requestSubmit(submitter = undefined) {
      formCheck(this);
      if (submitter !== undefined) {
        if (!isSubmitButton(submitter))
          throw new TypeError('The specified element is not a submit button.');
        if (formOwner(submitter) !== this)
          throw platformDOMException(
            'The specified element is not owned by this form element.',
            'NotFoundError',
          );
      } else submitter = null;
      if (submittingForms.has(this)) return;
      submittingForms.add(this);
      try {
        if (
          !this.hasAttribute('novalidate') &&
          !submitter?.hasAttribute('formnovalidate') &&
          !validateForm(this)
        )
          return;
        const event = new SubmitEvent('submit', {
          bubbles: true,
          cancelable: true,
          submitter,
        });
        if (this.dispatchEvent(event) && this.isConnected) submitForm.call(this, submitter);
      } finally {
        submittingForms.delete(this);
      }
    },
    writable: true,
  });
  const submittingForms = new WeakSet();
  compatibilityElementState.implicitlySubmitForm = (control, activateSubmitter, dispatchSubmit) => {
    if (control?.localName !== 'input' || !control.isConnected || control.disabled) return;
    const form = formOwner(control);
    if (!form || submittingForms.has(form)) return;
    const submitter = associated(form).find(isSubmitButton);
    if (submitter) {
      if (!disabledForForm(submitter)) activateSubmitter(submitter);
      return;
    }
    const blockingTypes = new Set([
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
    ]);
    if (
      !blockingTypes.has(typeOf(control)) ||
      associated(form).filter(
        (candidate) => candidate.localName === 'input' && blockingTypes.has(typeOf(candidate)),
      ).length > 1
    )
      return;
    submittingForms.add(form);
    try {
      if (!form.hasAttribute('novalidate') && !validateForm(form)) return;
      if (dispatchSubmit(form) && form.isConnected) submitForm.call(form);
    } finally {
      submittingForms.delete(form);
    }
  };
  compatibilityElementState.activateFormControl = (control, dispatchSubmit) => {
    if (!control?.isConnected || control.disabled) return;
    const type =
      control.localName === 'button'
        ? String(control.getAttribute('type') || 'submit').toLowerCase()
        : typeOf(control);
    if (type === 'reset') {
      formOwner(control)?.reset();
      return;
    }
    if ((control.localName === 'button' && type === 'button') || !isSubmitButton(control)) return;
    if (!['button', 'input'].includes(control.localName)) return;
    const form = formOwner(control);
    if (!form || submittingForms.has(form)) return;
    submittingForms.add(form);
    try {
      if (
        !form.hasAttribute('novalidate') &&
        !control.hasAttribute('formnovalidate') &&
        !validateForm(form)
      )
        return;
      if (dispatchSubmit(form, control) && form.isConnected) submitForm.call(form, control);
    } finally {
      submittingForms.delete(form);
    }
  };
  define('HTMLFormElement', 'reset', {
    value: function () {
      formCheck(this);
      if (!this.dispatchEvent(new Event('reset', { bubbles: true, cancelable: true }))) return;
      for (const control of associated(this)) {
        compatibilityElementState.controlValueAssigned?.(control);
        if (control.localName === 'input') {
          const s = inputState(control);
          s.dirty = false;
          s.value = '';
          s.dirtyChecked = false;
          s.checked = false;
        }
        if (control.localName === 'textarea') {
          const s = textareaState(control);
          s.dirty = false;
          s.value = '';
        }
        if (control.localName === 'select') {
          for (const option of optionList(control)) {
            const s = optionState(option);
            s.dirty = false;
            s.selected = false;
          }
          selectState(control).noSelection = false;
        }
      }
    },
    writable: true,
  });
  // Export is an immutable projection of these same dirty-state slots. Never
  // rewrite default attributes or call public value/checked setters to take it.
  registerBootstrapCallback('registerFormSnapshot', () => {
    const result = [];
    for (const element of elementWrappers.values()) {
      const slot = elementSlot(element);
      if (!slot) continue;
      const record = { nodeID: slot.nodeId },
        input = inputs.get(element),
        textarea = textareas.get(element),
        option = options.get(element);
      if (input) {
        if (input.dirty && mode(element) === 'value') record.value = inputValue(element);
        if (input.dirtyChecked) record.checked = input.checked;
      }
      if (textarea?.dirty) record.value = textarea.value;
      if (option?.dirty) {
        const owner = selectOwner(element);
        record.selected = owner ? selection(owner).selected.includes(element) : option.selected;
      }
      if (Object.keys(record).length > 1) result.push(record);
    }
    return JSON.stringify(result);
  });
}
