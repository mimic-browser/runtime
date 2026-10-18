// Constraint validation reads the existing live control state. No cached flags:
// attributes, group membership, fieldsets and dirty values can change independently.
const validationSlots = new WeakMap();
const validityNames = [
  'badInput',
  'customError',
  'patternMismatch',
  'rangeOverflow',
  'rangeUnderflow',
  'stepMismatch',
  'tooLong',
  'tooShort',
  'typeMismatch',
  'valid',
  'valueMissing',
];
const validationState = (e) => {
  let s = validationSlots.get(e);
  if (!s) {
    s = { custom: '', userEdited: false, badInput: false, raw: null };
    validationSlots.set(e, s);
  }
  return s;
};
const validationControl = (e) => {
  const slot = elementSlot(e);
  if (
    !slot ||
    !['INPUT', 'TEXTAREA', 'SELECT', 'BUTTON', 'FIELDSET', 'OUTPUT'].includes(slot.tagName)
  )
    throw new TypeError('Illegal invocation');
  return e;
};
const disabledControl = (e) => {
  if (e.hasAttribute('disabled')) return true;
  for (let p = e.parentElement; p; p = p.parentElement)
    if (p.localName === 'fieldset' && p.hasAttribute('disabled')) {
      const legend = Array.from(p.children).find((x) => x.localName === 'legend');
      if (!legend || !legend.contains(e)) return true;
    }
  return false;
};
const validationCandidate = (e) => {
  validationControl(e);
  if (
    disabledControl(e) ||
    e.hasAttribute('readonly') ||
    ['fieldset', 'output'].includes(e.localName)
  )
    return false;
  for (let p = e.parentElement; p; p = p.parentElement)
    if (p.localName === 'datalist') return false;
  const type =
    e.localName === 'input'
      ? typeOf(e)
      : e.localName === 'button'
        ? buttonTypeOf(e)
        : '';
  return !['hidden', 'reset', 'button'].includes(type);
};
const textValidationTypes = new Set(['text', 'search', 'tel', 'url', 'email', 'password']);
const numericValidationTypes = new Set([
  'number',
  'range',
  'date',
  'month',
  'week',
  'time',
  'datetime-local',
]);
const numericValue = (type, value) => {
  if (!value) return NaN;
  if (type === 'number' || type === 'range')
    return /^-?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value) ? Number(value) : NaN;
  if (type === 'month') {
    const m = /^(\d{4,})-(\d{2})$/.exec(value);
    return m && +m[1] > 0 && +m[2] > 0 && +m[2] <= 12 ? (+m[1] - 1970) * 12 + (+m[2] - 1) : NaN;
  }
  if (type === 'time') {
    const m = /^(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/.exec(value);
    return m && +m[1] < 24 && +m[2] < 60 && +(m[3] || 0) < 60
      ? ((+m[1] * 60 + +m[2]) * 60 + +(m[3] || 0)) * 1000 + Number((m[4] || '').padEnd(3, '0'))
      : NaN;
  }
  if (type === 'week') {
    const m = /^(\d{4,})-W(\d{2})$/.exec(value);
    if (!m || +m[1] < 1 || +m[2] < 1 || +m[2] > 53) return NaN;
    const start = (y) => {
      const d = new Date(0);
      d.setUTCFullYear(y, 0, 4);
      return +d - ((d.getUTCDay() + 6) % 7) * 86400000;
    };
    const n = start(+m[1]) + (+m[2] - 1) * 604800000;
    return n < start(+m[1] + 1) ? n : NaN;
  }
  const parts = value.split(/[T ]/),
    m = /^(\d{4,})-(\d{2})-(\d{2})$/.exec(parts[0]);
  if (!m || +m[1] < 1) return NaN;
  const date = new Date(0);
  date.setUTCFullYear(+m[1], +m[2] - 1, +m[3]);
  if (
    date.getUTCFullYear() !== +m[1] ||
    date.getUTCMonth() !== +m[2] - 1 ||
    date.getUTCDate() !== +m[3]
  )
    return NaN;
  return type === 'date' && parts.length === 1
    ? +date
    : type === 'datetime-local' && parts.length === 2
      ? +date + numericValue('time', parts[1])
      : NaN;
};
const numericConstraints = (e, type) => {
  const min = numericValue(type, e.getAttribute('min')),
    max = numericValue(type, e.getAttribute('max')),
    attr = numericValue(type, e.getAttribute('value')),
    scale =
      type === 'month'
        ? 1
        : type === 'week'
          ? 604800000
          : type === 'date'
            ? 86400000
            : ['time', 'datetime-local'].includes(type)
              ? 1000
              : 1;
  const raw = e.getAttribute('step'),
    step =
      raw === 'any'
        ? NaN
        : (Number(raw) > 0 ? Number(raw) : ['time', 'datetime-local'].includes(type) ? 60 : 1) *
          scale;
  return {
    min,
    max,
    step,
    base: Number.isFinite(min)
      ? min
      : Number.isFinite(attr)
        ? attr
        : type === 'week'
          ? -259200000
          : 0,
  };
};
const validityFlags = (e) => {
  if (isolatedControls)
    return parseControlJSON(
      host.mainWorldInput(
        elementSlot(e).nodeId,
        'form',
        stringifyControlJSON({ operation: 'get', key: '$validity', args: [] }),
      ),
    ).value;
  validationControl(e);
  const s = validationState(e),
    tag = e.localName,
    type = tag === 'input' ? typeOf(e) : tag,
    value = String(e.value ?? '');
  const f = {
    badInput: s.badInput,
    customError: s.custom !== '',
    patternMismatch: false,
    rangeOverflow: false,
    rangeUnderflow: false,
    stepMismatch: false,
    tooLong: false,
    tooShort: false,
    typeMismatch: false,
    valid: true,
    valueMissing: false,
  };
  if (tag === 'input' && type === 'checkbox') f.valueMissing = e.required && !e.checked;
  else if (tag === 'input' && type === 'radio' && e.name) {
    const group = compatibilitySelectors
      .query(e.getRootNode(), 'input')
      .filter((x) => typeOf(x) === 'radio' && x.name === e.name && formOwner(x) === formOwner(e));
    f.valueMissing = group.some((x) => x.required) && !group.some((x) => x.checked);
  } else if (e.required && validationCandidate(e)) {
    if (tag === 'select') {
      const { list, selected } = selection(e);
      const placeholder =
        !e.multiple && Number(e.size) <= 1 && list[0]?.parentElement === e && list[0]?.value === '';
      f.valueMissing =
        !selected.length || (!!placeholder && selected.length === 1 && selected[0] === list[0]);
    } else if (
      tag === 'textarea' ||
      (tag === 'input' &&
        !['radio', 'range', 'color', 'hidden', 'submit', 'reset', 'image', 'button'].includes(type))
    )
      f.valueMissing = value === '';
  }
  if (value && type === 'email') {
    const email =
      /^[a-zA-Z0-9.!#$%&'*+\/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
    f.typeMismatch = (e.multiple ? value.split(',') : [value]).some((x) => !email.test(x.trim()));
  }
  if (value && type === 'url')
    try {
      new URL(value);
    } catch {
      f.typeMismatch = true;
    }
  if (value && tag === 'input' && textValidationTypes.has(type) && e.hasAttribute('pattern'))
    try {
      const source = '^(?:' + e.getAttribute('pattern') + ')$';
      let expression;
      try {
        expression = new RegExp(source, 'v');
      } catch {
        expression = new RegExp(source, 'u');
      }
      f.patternMismatch = !expression.test(value);
    } catch {}
  if (value && s.userEdited && (tag === 'textarea' || textValidationTypes.has(type))) {
    const max = e.getAttribute('maxlength'),
      min = e.getAttribute('minlength');
    f.tooLong = max !== null && /^\d+$/.test(max) && value.length > +max;
    f.tooShort = min !== null && /^\d+$/.test(min) && value.length < +min;
  }
  if (tag === 'input' && numericValidationTypes.has(type) && value) {
    const n = numericValue(type, value),
      c = numericConstraints(e, type);
    f.rangeUnderflow = n < c.min;
    f.rangeOverflow = n > c.max;
    if (type === 'time' && c.min > c.max) {
      f.rangeUnderflow = n < c.min && n > c.max;
      f.rangeOverflow = f.rangeUnderflow;
    }
    const delta = (n - c.base) / c.step;
    f.stepMismatch = Number.isFinite(delta) && Math.abs(delta - Math.round(delta)) > 1e-7;
  }
  f.valid = !Object.entries(f).some(([k, v]) => k !== 'valid' && v);
  return f;
};
const validationMessage = (e) => {
  if (!validationCandidate(e)) return '';
  const f = validityFlags(e);
  if (f.valid) return '';
  const s = validationState(e),
    ru = String(navigator.language).startsWith('ru'),
    type = e.localName === 'input' ? typeOf(e) : e.localName;
  if (f.customError) return s.custom;
  if (f.badInput) return ru ? 'Введите число.' : 'Please enter a number.';
  if (f.valueMissing)
    return type === 'checkbox'
      ? ru
        ? 'Чтобы продолжить, установите этот флажок.'
        : 'Please check this box if you want to proceed.'
      : type === 'radio'
        ? ru
          ? 'Выберите один из этих вариантов.'
          : 'Please select one of these options.'
        : type === 'select'
          ? ru
            ? 'Выберите один из пунктов списка.'
            : 'Please select an item in the list.'
          : ru
            ? 'Заполните это поле.'
            : 'Please fill out this field.';
  if (f.typeMismatch) {
    if (type === 'url') return ru ? 'Введите URL.' : 'Please enter a URL.';
    const value = String(e.value);
    if (!value.includes('@'))
      return ru
        ? 'Адрес электронной почты должен содержать символ "@". В адресе "' +
            value +
            '" отсутствует символ "@".'
        : "Please include an '@' in the email address. '" + value + "' is missing an '@'.";
    return ru ? 'Введите адрес электронной почты.' : 'Please enter an email address.';
  }
  if (f.patternMismatch)
    return ru ? 'Введите данные в указанном формате.' : 'Please match the requested format.';
  if (f.rangeUnderflow)
    return ru
      ? 'Значение должно быть больше или равно ' + e.getAttribute('min') + '.'
      : 'Value must be greater than or equal to ' + e.getAttribute('min') + '.';
  if (f.rangeOverflow)
    return ru
      ? 'Значение должно быть меньше или равно ' + e.getAttribute('max') + '.'
      : 'Value must be less than or equal to ' + e.getAttribute('max') + '.';
  if (f.stepMismatch && ['number', 'range'].includes(type)) {
    const c = numericConstraints(e, type),
      n = Number(e.value),
      low = c.base + Math.floor((n - c.base) / c.step) * c.step,
      high = low + c.step;
    return ru
      ? 'Введите допустимое значение. Ближайшие допустимые значения: ' + low + ' и ' + high + '.'
      : 'Please enter a valid value. The two nearest valid values are ' +
          low +
          ' and ' +
          high +
          '.';
  }
  // Locale-specific native UI wording outside the measured Russian/English
  // subset remains a documented limitation; flags and validity never depend on it.
  return ru ? 'Введите допустимое значение.' : 'Please enter a valid value.';
};
const validityPrototype = globalThis.ValidityState?.prototype;
if (validityPrototype) {
  for (const flag of validityNames)
    Object.defineProperty(validityPrototype, flag, {
      get() {
        const binding = requireRealmBinding(this, 'ValidityState');
        return callRealmBinding(this, binding, flag, []);
      },
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(validityPrototype, Symbol.toStringTag, {
    value: 'ValidityState',
    configurable: true,
  });
}
const validityObject = (e) => {
  validationControl(e);
  const s = validationState(e);
  if (!s.object) {
    s.object = Object.create(validityPrototype);
    const operations = {};
    for (const key of validityNames) operations[key] = () => validityFlags(e)[key];
    registerRealmBinding(s.object, 'ValidityState', operations);
  }
  return s.object;
};
const validateControl = (e) => {
  validationControl(e);
  if (!validationCandidate(e) || validityFlags(e).valid) return true;
  dispatchEventCore(e, new Event('invalid', { cancelable: true }), true, true);
  return false;
};
const validateForm = (form) => {
  formCheck(form);
  let valid = true;
  for (const e of associated(form)) if (!validateControl(e)) valid = false;
  return valid;
};
for (const name of [
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'HTMLSelectElement',
  'HTMLButtonElement',
  'HTMLFieldSetElement',
  'HTMLOutputElement',
]) {
  if (validityPrototype)
    define(name, 'validity', {
      get() {
        return validityObject(this);
      },
    });
  define(name, 'willValidate', {
    get() {
      return validationCandidate(this);
    },
  });
  define(name, 'validationMessage', {
    get() {
      validationControl(this);
      return validationMessage(this);
    },
  });
  define(name, 'setCustomValidity', {
    value: function (message) {
      validationControl(this);
      if (!arguments.length) throw new TypeError('1 argument required');
      validationState(this).custom = bindingString(message);
    },
    writable: true,
  });
  define(name, 'checkValidity', {
    value: function () {
      return validateControl(this);
    },
    writable: true,
  });
  define(name, 'reportValidity', {
    value: function () {
      return validateControl(this);
    },
    writable: true,
  });
}
for (const method of ['checkValidity', 'reportValidity'])
  define('HTMLFormElement', method, {
    value: function () {
      return validateForm(this);
    },
    writable: true,
  });
compatibilityElementState.controlEdited = (e, raw) => {
  if (isolatedControls) {
    host.mainWorldInput(
      elementSlot(e).nodeId,
      'form',
      stringifyControlJSON({ operation: 'call', key: '$controlEdited', args: [raw] }),
    );
    return;
  }
  const s = validationState(e);
  s.userEdited = true;
  s.badInput =
    e.localName === 'input' && typeOf(e) === 'number' && raw !== '' && sanitize(e, raw) === '';
  s.raw = raw;
};
compatibilityElementState.controlValueAssigned = (e) => {
  const s = validationState(e);
  s.userEdited = false;
  s.badInput = false;
  s.raw = null;
};
compatibilityElementState.controlEditValue = (e) =>
  isolatedControls
    ? parseControlJSON(
        host.mainWorldInput(
          elementSlot(e).nodeId,
          'form',
          stringifyControlJSON({ operation: 'get', key: '$editValue', args: [] }),
        ),
      ).value
    : e.localName === 'input' && typeOf(e) === 'number'
      ? validationState(e).raw
      : null;
