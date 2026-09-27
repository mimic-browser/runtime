// This expression is embedded after `await` by the oracle tests.
// prettier-ignore
(() => {
  const output = {};
  performance.mark("options-end", { startTime: 12 });
  const cases = {
    empty: {},
    null: null,
    undefined: undefined,
    array: [],
    function: () => {},
    unknown: { unrelated: 1 },
    undefinedMembers: { start: undefined, end: undefined, duration: undefined },
    detail: { detail: { value: 7 } },
    detailNull: { detail: null },
    start: { start: 0 },
    end: { end: 12 },
    duration: { duration: 12 },
  };
  for (const [name, options] of Object.entries(cases)) {
    try {
      const entry = performance.measure(name, options, "options-end");
      output[name] = {
        startTime: entry.startTime,
        duration: entry.duration,
        detail: entry.detail,
      };
    } catch (error) {
      output[name] = { error: error.name };
    }
  }
  const conversions = [];
  const options = {};
  for (const member of ["detail", "duration", "end", "start"]) {
    Object.defineProperty(options, member, {
      get() {
        conversions.push(member);
        return undefined;
      },
    });
  }
  try {
    const entry = performance.measure("conversion", options, {
      toString() {
        conversions.push("third");
        return "options-end";
      },
    });
    output.conversion = {
      order: conversions,
      duration: entry.duration,
    };
  } catch (error) {
    output.conversion = { order: conversions, error: error.name };
  }
  performance.clearMarks("options-end");
  performance.clearMeasures();
  return output;
})()
