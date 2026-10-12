// Bounded arithmetic evaluator for readback observations. It does not execute
// native shaders, allocate a GPU, or present images. Unsupported language
// features remain explicit; parsing a source is not blanket GLSL compatibility.
const glslObservations = (() => {
  const unsupported = (message) => {
    const e = new Error(message);
    e.unsupported = true;
    throw e;
  };
  const f = Math.fround,
    vector = (v) => Array.isArray(v),
    map = (v, fn) => (vector(v) ? v.map(fn) : fn(v));
  const zip = (a, b, fn) =>
    vector(a) || vector(b)
      ? Array.from({ length: vector(a) ? a.length : b.length }, (_, i) =>
          fn(vector(a) ? a[i] : a, vector(b) ? b[i] : b),
        )
      : fn(a, b);
  const types = new Set([
    'float',
    'int',
    'bool',
    'vec2',
    'vec3',
    'vec4',
    'ivec2',
    'ivec3',
    'ivec4',
    'sampler2D',
  ]);
  const builtins = {
    sin: Math.sin,
    cos: Math.cos,
    tan: Math.tan,
    asin: Math.asin,
    acos: Math.acos,
    sqrt: Math.sqrt,
    inversesqrt: (x) => 1 / Math.sqrt(x),
    abs: Math.abs,
    floor: Math.floor,
    ceil: Math.ceil,
    fract: (x) => x - Math.floor(x),
    exp: Math.exp,
    log: Math.log,
    exp2: (x) => 2 ** x,
    log2: Math.log2,
    sign: Math.sign,
  };
  function compile(source, shaderType) {
    if (source.length > 65536) unsupported('shader source limit');
    source = source
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/\/\/[^\n]*/g, '')
      .replace(/^\s*#version\s+(100|300 es)\s*$/gm, '');
    const tokens = [];
    const scan =
      /\s+|(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?[fF]?|[A-Za-z_]\w*|\+\+|--|<=|>=|==|!=|\+=|-=|\*=|\/=|&&|\|\||[{}()[\];,.?:+*\/%!<>=-]/gy;
    let at = 0;
    while (at < source.length) {
      scan.lastIndex = at;
      const m = scan.exec(source);
      if (!m) unsupported('unsupported shader token');
      at = scan.lastIndex;
      if (!/^\s+$/.test(m[0])) tokens.push(m[0]);
    }
    let index = 0;
    const peek = () => tokens[index],
      take = () => tokens[index++],
      expect = (t) => {
        if (take() !== t) throw new SyntaxError('Expected ' + t);
      };
    const precedence = {
      '||': 1,
      '&&': 2,
      '==': 3,
      '!=': 3,
      '<': 4,
      '>': 4,
      '<=': 4,
      '>=': 4,
      '+': 5,
      '-': 5,
      '*': 6,
      '/': 6,
      '%': 6,
    };
    const knownCall = (name) =>
      types.has(name) ||
      Object.hasOwn(builtins, name) ||
      [
        'min',
        'max',
        'pow',
        'mod',
        'clamp',
        'mix',
        'step',
        'smoothstep',
        'dot',
        'length',
        'normalize',
        'texture',
        'texture2D',
      ].includes(name);
    function expression(min = 0) {
      let node,
        t = take();
      if (t === undefined) throw new SyntaxError('Missing expression');
      if (t === '(') {
        node = expression();
        expect(')');
      } else if (['+', '-', '!'].includes(t)) node = { op: 'unary', kind: t, value: expression(7) };
      else if (/^(\d|\.)/.test(t))
        node = {
          op: 'literal',
          value: f(Number(t.replace(/[fF]$/, ''))),
          type: /[.eEfF]/.test(t) ? 'float' : 'int',
        };
      else if (t === 'true' || t === 'false') node = { op: 'literal', value: t === 'true' };
      else if (/^[A-Za-z_]\w*$/.test(t)) {
        if (peek() === '(') {
          if (!knownCall(t)) unsupported('unsupported shader function ' + t);
          take();
          const args = [];
          if (peek() !== ')') {
            do {
              args.push(expression());
              if (peek() !== ',') break;
              take();
            } while (true);
          }
          expect(')');
          node = { op: 'call', name: t, args };
        } else node = { op: 'variable', name: t };
      } else throw new SyntaxError('Invalid expression');
      while (peek() === '.') {
        take();
        const fields = take();
        if (!/^[xyzwrgba]{1,4}$/.test(fields)) unsupported('unsupported shader member');
        node = { op: 'swizzle', value: node, fields };
      }
      while (Object.hasOwn(precedence, peek()) && precedence[peek()] >= min) {
        const operator = take();
        node = {
          op: 'binary',
          kind: operator,
          left: node,
          right: expression(precedence[operator] + 1),
        };
      }
      if (min === 0 && peek() === '?') {
        take();
        const yes = expression();
        expect(':');
        node = { op: 'conditional', test: node, yes, no: expression() };
      }
      return node;
    }
    function statement(semicolon = true) {
      if (peek() === '{') {
        take();
        const body = [];
        while (peek() !== '}') {
          if (peek() === undefined) throw new SyntaxError('Unclosed block');
          body.push(statement());
        }
        take();
        return { op: 'block', body };
      }
      if (peek() === 'for') {
        take();
        expect('(');
        const init = statement(),
          test = expression();
        expect(';');
        const step = statement(false);
        expect(')');
        return { op: 'for', init, test, step, body: statement() };
      }
      if (peek() === 'if') {
        take();
        expect('(');
        const test = expression();
        expect(')');
        const yes = statement();
        let no;
        if (peek() === 'else') {
          take();
          no = statement();
        }
        return { op: 'if', test, yes, no };
      }
      if (peek() === 'discard') {
        take();
        expect(';');
        return { op: 'discard' };
      }
      if (peek() === 'return') {
        take();
        expect(';');
        return { op: 'return' };
      }
      let constant = false;
      if (peek() === 'const') {
        take();
        constant = true;
      }
      let type;
      if (types.has(peek())) type = take();
      else if (constant) unsupported('unsupported declaration');
      const name = take();
      if (!/^[A-Za-z_]\w*$/.test(name || '')) throw new SyntaxError('Missing variable');
      let kind = take(),
        value;
      if (kind === '++' || kind === '--') value = { op: 'literal', value: 1 };
      else if (['=', '+=', '-=', '*=', '/='].includes(kind)) value = expression();
      else if (type && kind === ';') {
        index--;
        kind = '=';
        value = { op: 'literal', value: initial(type), type };
      } else unsupported('unsupported shader statement');
      if (semicolon) expect(';');
      return { op: type ? 'declare' : 'assign', name, type, kind, value, constant };
    }
    const globals = [];
    let main = null;
    while (index < tokens.length) {
      if (peek() === 'precision') {
        while (take() !== ';') {
          if (index >= tokens.length) throw new SyntaxError('Incomplete precision declaration');
        }
        continue;
      }
      if (peek() === 'void') {
        take();
        if (take() !== 'main') unsupported('user shader functions');
        expect('(');
        expect(')');
        if (main) throw new SyntaxError('Duplicate main');
        main = statement();
        continue;
      }
      let qualifier = '';
      if (['attribute', 'uniform', 'varying', 'in', 'out', 'const'].includes(peek()))
        qualifier = take();
      if (['lowp', 'mediump', 'highp'].includes(peek())) take();
      const type = take(),
        name = take();
      if (!types.has(type) || !/^[A-Za-z_]\w*$/.test(name || ''))
        unsupported('unsupported shader declaration');
      let value;
      if (peek() === '=') {
        take();
        value = expression();
      }
      expect(';');
      globals.push({ name, type, qualifier, value });
    }
    if (!main) throw new SyntaxError('Missing main');
    const output =
      shaderType === 35632
        ? globals.find((g) => g.qualifier === 'out')?.name || 'gl_FragColor'
        : 'gl_Position';
    const root = Object.create(null),
      used = new Set(),
      width = (type) => (type.includes('vec') ? Number(type.slice(-1)) : 1);
    const invalid = (message) => {
      throw new SyntaxError(message);
    };
    Object.assign(
      root,
      shaderType === 35632
        ? {
            gl_FragColor: { type: 'vec4' },
            gl_FragCoord: { type: 'vec4', readonly: true },
            gl_FrontFacing: { type: 'bool', readonly: true },
          }
        : {
            gl_Position: { type: 'vec4' },
            gl_PointSize: { type: 'float' },
            gl_VertexID: { type: 'int', readonly: true },
            gl_InstanceID: { type: 'int', readonly: true },
          },
    );
    for (const g of globals) {
      if (Object.hasOwn(root, g.name)) invalid('Duplicate global');
      root[g.name] = {
        type: g.type,
        global: true,
        readonly: ['uniform', 'attribute', 'in', 'const'].includes(g.qualifier),
      };
    }
    function infer(n, scope) {
      switch (n.op) {
        case 'literal':
          return n.type || (typeof n.value === 'boolean' ? 'bool' : 'int');
        case 'variable': {
          const d = scope[n.name];
          if (!d) invalid('Undeclared variable ' + n.name);
          if (d.global) used.add(n.name);
          return d.type;
        }
        case 'swizzle': {
          const t = infer(n.value, scope);
          if (width(t) === 1) unsupported('scalar swizzle');
          const alphabet = /^[xyzw]+$/.test(n.fields)
            ? 'xyzw'
            : /^[rgba]+$/.test(n.fields)
              ? 'rgba'
              : null;
          if (
            !alphabet ||
            Array.from(n.fields, (c) => alphabet.indexOf(c)).some((i) => i >= width(t))
          )
            invalid('Invalid swizzle');
          return n.fields.length === 1
            ? t.startsWith('i')
              ? 'int'
              : 'float'
            : (t.startsWith('i') ? 'ivec' : 'vec') + n.fields.length;
        }
        case 'unary':
          return infer(n.value, scope);
        case 'binary': {
          const a = infer(n.left, scope),
            b = infer(n.right, scope);
          if (width(a) > 1 && width(b) > 1 && width(a) !== width(b))
            invalid('Vector widths differ');
          if (['<', '>', '<=', '>=', '==', '!=', '&&', '||'].includes(n.kind)) {
            if (width(a) > 1 || width(b) > 1) unsupported('vector comparison');
            return 'bool';
          }
          return width(a) > width(b)
            ? a
            : width(b) > width(a)
              ? b
              : a === 'float' || b === 'float'
                ? 'float'
                : a;
        }
        case 'conditional': {
          infer(n.test, scope);
          const a = infer(n.yes, scope),
            b = infer(n.no, scope);
          if (a !== b) invalid('Conditional types differ');
          return a;
        }
        case 'call': {
          const args = n.args.map((a) => infer(a, scope));
          if (['texture', 'texture2D'].includes(n.name)) {
            if (args.length !== 2 || args[0] !== 'sampler2D' || args[1] !== 'vec2')
              invalid('Texture arguments');
            return 'vec4';
          }
          if (types.has(n.name)) {
            if (!args.length) invalid('Empty constructor');
            const size = width(n.name),
              total = args.reduce((sum, a) => sum + width(a), 0);
            if (
              (size === 1 && args.length !== 1) ||
              (size > 1 &&
                total !== 1 &&
                (total < size || total - width(args[args.length - 1]) >= size))
            )
              invalid('Constructor components');
            return n.name;
          }
          const count = ['clamp', 'mix', 'smoothstep'].includes(n.name)
            ? 3
            : ['min', 'max', 'pow', 'mod', 'step', 'dot'].includes(n.name)
              ? 2
              : 1;
          if (args.length !== count) invalid('Builtin argument count');
          return ['length', 'dot'].includes(n.name)
            ? 'float'
            : args.reduce((a, b) => (width(a) >= width(b) ? a : b));
        }
      }
    }
    function validate(n, scope) {
      switch (n.op) {
        case 'block': {
          const local = Object.create(scope);
          for (const child of n.body) validate(child, local);
          break;
        }
        case 'declare': {
          if (Object.hasOwn(scope, n.name)) invalid('Duplicate local');
          const t = infer(n.value, scope);
          if (width(t) !== width(n.type)) invalid('Declaration type');
          scope[n.name] = { type: n.type, readonly: n.constant };
          break;
        }
        case 'assign': {
          const d = scope[n.name];
          if (!d) invalid('Undeclared assignment');
          if (d.readonly) invalid('Read-only assignment');
          if (width(infer(n.value, scope)) !== width(d.type)) invalid('Assignment type');
          break;
        }
        case 'for': {
          const local = Object.create(scope);
          validate(n.init, local);
          infer(n.test, local);
          validate(n.step, local);
          validate(n.body, local);
          break;
        }
        case 'if':
          infer(n.test, scope);
          validate(n.yes, scope);
          if (n.no) validate(n.no, scope);
          break;
        case 'discard':
          if (shaderType !== 35632) invalid('Vertex discard');
          break;
      }
    }
    for (const g of globals) if (g.value) infer(g.value, root);
    validate(main, root);
    // A single vector output assembled from inputs and constants needs no
    // per-fragment interpreter scope. Keep the parsed shader as the fallback.
    let directColor = null;
    if (shaderType === 35632 && main.op === 'block' && main.body.length === 1) {
      const assignment = main.body[0];
      if (
        assignment.op === 'assign' &&
        assignment.kind === '=' &&
        assignment.name === output &&
        assignment.value.op === 'call' &&
        assignment.value.name === 'vec4' &&
        assignment.value.args.every((arg) => arg.op === 'literal' || arg.op === 'variable')
      )
        directColor = assignment.value.args;
    }
    return { globals, main, shaderType, output, used, directColor };
  }
  const initial = (type) =>
    type.includes('vec') ? Array(Number(type.slice(-1))).fill(0) : type === 'bool' ? false : 0;
  function executeColor(shader, inputs, output) {
    if (shader.directColor) {
      let count = 0;
      let available = true;
      for (const arg of shader.directColor) {
        if (arg.op === 'variable' && !Object.hasOwn(inputs, arg.name)) {
          available = false;
          break;
        }
        const value = arg.op === 'literal' ? arg.value : inputs[arg.name];
        if (Array.isArray(value)) {
          for (const component of value) {
            if (count < 4) output[count] = f(component);
            count++;
          }
        } else {
          if (count < 4) output[count] = f(value);
          count++;
        }
      }
      if (available && count >= 4) return output;
    }
    return execute(shader, inputs);
  }
  function execute(shader, inputs, captureNames = null) {
    if (captureNames === null && shader.directColor) {
      const values = [];
      let available = true;
      for (const arg of shader.directColor) {
        if (arg.op === 'variable' && !Object.hasOwn(inputs, arg.name)) {
          available = false;
          break;
        }
        const value = arg.op === 'literal' ? arg.value : inputs[arg.name];
        if (Array.isArray(value)) values.push(...value);
        else values.push(value);
      }
      if (available && values.length >= 4) return values.slice(0, 4).map(f);
    }
    let budget = 4096;
    const root = Object.assign(Object.create(null), inputs);
    root[shader.output] = undefined;
    const tick = () => {
      if (--budget < 0) unsupported('shader observation instruction limit');
    };
    function evaluate(node, scope) {
      tick();
      switch (node.op) {
        case 'literal':
          return node.value;
        case 'variable':
          if (!(node.name in scope)) unsupported('unbound shader variable ' + node.name);
          return scope[node.name];
        case 'array':
          return node.values.map((n) => evaluate(n, scope));
        case 'index': {
          const a = evaluate(node.value, scope),
            i = evaluate(node.index, scope);
          if (!Number.isInteger(i) || i < 0 || i >= a.length) unsupported('shader array bounds');
          return a[i];
        }
        case 'swizzle': {
          const v = evaluate(node.value, scope);
          if (!vector(v)) unsupported('scalar swizzle');
          const values = Array.from(
            node.fields,
            (c) => v['xyzw'.includes(c) ? 'xyzw'.indexOf(c) : 'rgba'.indexOf(c)],
          );
          if (values.some((v) => v === undefined)) throw new Error('Invalid vector component');
          return values.length === 1 ? values[0] : values;
        }
        case 'unary':
          return map(evaluate(node.value, scope), (v) =>
            node.kind === '-' ? f(-v) : node.kind === '!' ? !v : v,
          );
        case 'conditional':
          return evaluate(evaluate(node.test, scope) ? node.yes : node.no, scope);
        case 'binary': {
          const a = evaluate(node.left, scope);
          if (node.kind === '&&' && !a) return false;
          if (node.kind === '||' && a) return true;
          const b = evaluate(node.right, scope);
          return zip(a, b, (x, y) => {
            switch (node.kind) {
              case '+':
                return f(x + y);
              case '-':
                return f(x - y);
              case '*':
                return f(x * y);
              case '/':
                return f(x / y);
              case '%':
                return f(x % y);
              case '<':
                return x < y;
              case '>':
                return x > y;
              case '<=':
                return x <= y;
              case '>=':
                return x >= y;
              case '==':
                return x === y;
              case '!=':
                return x !== y;
              case '&&':
                return !!x && !!y;
              case '||':
                return !!x || !!y;
            }
          });
        }
        case 'call': {
          const a = node.args.map((n) => evaluate(n, scope)),
            name = node.name;
          if (['texture', 'texture2D'].includes(name)) {
            if (typeof root.__sampleTexture !== 'function') unsupported('texture sampling');
            return root.__sampleTexture(a[0], a[1]);
          }
          if (name === 'float') return f(a[0]);
          if (name === 'int') return Math.trunc(a[0]);
          if (name === 'bool') return !!a[0];
          if (/^(i?vec)[234]$/.test(name)) {
            const size = Number(name.slice(-1)),
              values = a.flat();
            if (values.length === 1)
              return Array(size).fill(name[0] === 'i' ? Math.trunc(values[0]) : f(values[0]));
            if (values.length < size) throw new Error('Invalid vector constructor');
            return values.slice(0, size).map(name[0] === 'i' ? Math.trunc : f);
          }
          if (Object.hasOwn(builtins, name)) return map(a[0], (x) => f(builtins[name](x)));
          if (name === 'dot') return f(a[0].reduce((s, x, i) => f(s + f(x * a[1][i])), 0));
          if (name === 'length') return f(Math.hypot(...(vector(a[0]) ? a[0] : [a[0]])));
          if (name === 'normalize') {
            const length = Math.hypot(...a[0]);
            return a[0].map((x) => f(x / length));
          }
          if (
            name === 'min' ||
            name === 'max' ||
            name === 'pow' ||
            name === 'mod' ||
            name === 'step'
          )
            return zip(a[0], a[1], (x, y) =>
              f(
                name === 'min'
                  ? Math.min(x, y)
                  : name === 'max'
                    ? Math.max(x, y)
                    : name === 'pow'
                      ? x ** y
                      : name === 'mod'
                        ? x - y * Math.floor(x / y)
                        : y < x
                          ? 0
                          : 1,
              ),
            );
          if (name === 'clamp')
            return zip(zip(a[0], a[1], Math.max), a[2], (x, y) => f(Math.min(x, y)));
          if (name === 'mix')
            return zip(
              a[0],
              zip(
                zip(a[1], a[0], (x, y) => x - y),
                a[2],
                (x, y) => x * y,
              ),
              (x, y) => f(x + y),
            );
          if (name === 'smoothstep') {
            const t = zip(
              zip(a[2], a[0], (x, y) => x - y),
              zip(a[1], a[0], (x, y) => x - y),
              (x, y) => Math.min(1, Math.max(0, x / y)),
            );
            return map(t, (x) => f(x * x * (3 - 2 * x)));
          }
          unsupported('unsupported builtin');
        }
      }
    }
    function run(node, scope) {
      tick();
      switch (node.op) {
        case 'block': {
          const local = Object.create(scope);
          for (const child of node.body) {
            const result = run(child, local);
            if (result) return result;
          }
          break;
        }
        case 'declare':
          scope[node.name] = node.value ? evaluate(node.value, scope) : initial(node.type);
          break;
        case 'assign': {
          let owner = scope;
          while (owner && !Object.hasOwn(owner, node.name)) owner = Object.getPrototypeOf(owner);
          if (!owner) unsupported('undeclared assignment');
          const v = evaluate(node.value, scope),
            old = owner[node.name];
          owner[node.name] =
            node.kind === '='
              ? v
              : zip(old, v, (x, y) =>
                  f(
                    node.kind === '+=' || node.kind === '++'
                      ? x + y
                      : node.kind === '-=' || node.kind === '--'
                        ? x - y
                        : node.kind === '*='
                          ? x * y
                          : x / y,
                  ),
                );
          break;
        }
        case 'for': {
          const local = Object.create(scope);
          run(node.init, local);
          let iterations = 0;
          while (evaluate(node.test, local)) {
            if (++iterations > 1024) unsupported('shader loop limit');
            const result = run(node.body, local);
            if (result) return result;
            run(node.step, local);
          }
          break;
        }
        case 'if':
          return evaluate(node.test, scope)
            ? run(node.yes, scope)
            : node.no
              ? run(node.no, scope)
              : undefined;
        case 'discard':
          return 'discard';
        case 'return':
          return 'return';
        case 'returnValue':
          root[shader.output] = evaluate(node.value, scope);
          return 'return';
      }
    }
    for (const g of shader.globals)
      if (!Object.hasOwn(root, g.name))
        root[g.name] = g.value ? evaluate(g.value, root) : initial(g.type);
    const result = run(shader.main, root);
    const output = result === 'discard' ? null : root[shader.output];
    if (captureNames === null) return output;
    const captured = Object.create(null);
    for (const name of captureNames) {
      const value = root[name];
      captured[name] = Array.isArray(value) ? value.slice() : value;
    }
    return { output, captured };
  }
  return { compile, execute, executeColor, initial };
})();
