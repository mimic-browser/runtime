// This expression is embedded after `await` by the oracle test.
// prettier-ignore
(() => {
  const results = {};
  const methods = [
    'toString',
    'toDateString',
    'toTimeString',
    'toLocaleString',
    'toLocaleDateString',
    'toLocaleTimeString',
    'getFullYear',
    'getMonth',
    'getDate',
    'getDay',
    'getHours',
    'getMinutes',
    'getSeconds',
    'getMilliseconds',
    'getYear',
    'getTimezoneOffset',
    'setFullYear',
    'setMonth',
    'setDate',
    'setHours',
    'setMinutes',
    'setSeconds',
    'setMilliseconds',
    'setYear',
  ];
  const receivers = {
    undefined: undefined,
    null: null,
    object: {},
    array: [],
    number: 0,
    string: 'x',
    boolean: true,
    symbol: Symbol('x'),
    bigint: 1n,
    datePrototype: Date.prototype,
    proxiedDate: new Proxy(new Date(0), {}),
  };
  for (const name of methods) {
    const rows = {};
    for (const [label, receiver] of Object.entries(receivers)) {
      let conversions = 0;
      const argument = {
        valueOf() {
          conversions++;
          return 0;
        },
      };
      try {
        rows[label] = { value: Reflect.apply(Date.prototype[name], receiver, [argument]) };
      } catch (error) {
        rows[label] = { name: error.name, message: error.message };
      }
      rows[label].conversions = conversions;
    }
    results[name] = rows;
  }
  const sentinel = new Error('argument conversion');
  const date = new Date(123);
  try {
    date.setHours({
      valueOf() {
        throw sentinel;
      },
    });
  } catch (error) {
    results.validReceiverFailure = { sameError: error === sentinel, value: date.getTime() };
  }
  for (const Constructor of [Number, BigInt]) {
    const rows = {};
    for (const [label, receiver] of Object.entries(receivers)) {
      try {
        rows[label] = { value: Constructor.prototype.toLocaleString.call(receiver) };
      } catch (error) {
        rows[label] = { name: error.name, message: error.message };
      }
    }
    results[Constructor.name + 'Locale'] = rows;
  }
  results.stringLocale = {};
  for (const [label, receiver] of [
    ['undefined', undefined],
    ['null', null],
  ]) {
    try {
      String.prototype.localeCompare.call(receiver, 'x');
    } catch (error) {
      results.stringLocale[label] = { name: error.name, message: error.message };
    }
  }
  return results;
})()
