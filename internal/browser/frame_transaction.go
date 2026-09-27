package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
)

// Reflection, argument materialization and result classification are one owner
// operation. The wire contains private reference records, never author objects.
// Keeping it encoded between owners avoids exporting intermediate JS records
// through Go and reentering V8 for each argument and result field.
const frameTransactionSource = `(decodeArgument, decodeKey, encodeKey) => (reflect, encode) => {
  const parse = JSON.parse,
    stringify = JSON.stringify,
    create = Object.create,
    keys = Object.keys,
    setPrototype = Object.setPrototypeOf,
    isArray = Array.isArray,
    integer = BigInt;
  const plain = (value) => {
    if (value === null || typeof value !== 'object') return value;
    const out = isArray(value) ? setPrototype([], null) : create(null),
      names = keys(value);
    for (let i = 0; i < names.length; i++) {
      const key = names[i];
      out[key] = plain(value[key]);
    }
    return out;
  };
  const json = (value) => stringify(plain(value));
  const argument = (a) => {
    if (a.kind === 'value') return a.value;
    if (a.kind === 'undefined') return undefined;
    if (a.kind === 'bigint') return integer(a.value);
    if (a.kind === 'special-number')
      return a.value === 'NaN'
        ? NaN
        : a.value === '-0'
          ? -0
          : a.value === 'Infinity'
            ? Infinity
            : -Infinity;
    return decodeArgument(json(a));
  };
  const key = (k) => (k.kind === 'string' ? k.value : decodeKey(json(k)));
  const argumentsOf = (encoded) => {
    const out = setPrototype([], null);
    for (let i = 0; i < encoded.length; i++) out[i] = argument(encoded[i]);
    return out;
  };
  const outcome = (record) =>
    '{' + '"threw":' + (record.threw ? 'true' : 'false') + ',"value":' + encode(record.value) + '}';
  return (op, object, wire) => {
    const data = parse(wire);
    if (op === 'get') return encode(reflect('get', object, key(data)).value);
    if (op === 'has') return reflect('has', object, key(data)) ? 'true' : 'false';
    if (op === 'prototype') return encode(reflect('prototype', object));
    if (op === 'extensible') return reflect('extensible', object) ? 'true' : 'false';
    if (op === 'preventExtensions') return outcome(reflect('preventExtensionsOutcome', object));
    if (op === 'keys') {
      const source = reflect('keys', object),
        out = [];
      for (let i = 0; i < source.length; i++)
        out[i] =
          source[i].kind === 'string'
            ? { kind: 'string', value: source[i].value }
            : encodeKey(source[i]);
      return json(out);
    }
    if (op === 'descriptor') {
      const d = reflect('descriptor', object, key(data));
      if (!d.exists) return '{"exists":false}';
      const out = create(null);
      out.exists = true;
      out.accessor = d.accessor;
      out.enumerable = d.enumerable;
      out.configurable = d.configurable;
      if (d.accessor) {
        out.get = parse(encode(d.get));
        out.set = parse(encode(d.set));
      } else {
        out.writable = d.writable;
        out.value = parse(encode(d.value));
      }
      return json(out);
    }
    if (op === 'apply')
      return outcome(
        reflect(
          data[2] ? 'arrayIteratorOutcome' : 'applyOutcome',
          object,
          argument(data[0]),
          data[2] ? undefined : argumentsOf(data[1]),
        ),
      );
    if (op === 'construct')
      return outcome(reflect('construct', object, argumentsOf(data[1]), argument(data[0])));
    if (op === 'set')
      return outcome(reflect('setOutcome', object, key(data[0]), argument(data[1])));
    if (op === 'mutateDefine' || op === 'mutateDelete')
      return outcome(reflect(op, object, key(data[0]), argument(data[1])));
    throw new Error('Invalid frame transaction');
  };
};`

func (r *Realm) prepareFrameTransaction() error {
	factory, err := evalPlatformExpression(r.runtime, frameTransactionSource, "mimic:frame-transaction")
	if err != nil {
		return err
	}
	decodeArgument := r.val(r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		var argument any
		if err := json.Unmarshal([]byte(strarg(args, 0)), &argument); err != nil {
			return nil, err
		}
		return r.decodeFrameArgument(argument)
	}))
	decodeKey := r.val(r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		var key any
		if err := json.Unmarshal([]byte(strarg(args, 0)), &key); err != nil {
			return nil, err
		}
		return r.decodeFrameKey(context.Background(), key)
	}))
	encodeKey := r.val(r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(r.encodeFrameKey(args[0])), nil
	}))
	r.frameTransaction, err = r.runtime.Call(context.Background(), factory, nil, decodeArgument, decodeKey, encodeKey)
	return err
}

func (r *Realm) installFrameTransaction() error {
	if r.frameReflection.err != nil {
		return r.frameReflection.err
	}
	operation, err := r.runtime.Call(context.Background(), r.frameTransaction, nil, r.frameReflection.operation, r.frameValueEncoder)
	if err == nil {
		r.frameTransaction = operation
	}
	return err
}

func (r *Realm) transactFrame(args []engine.Value) (engine.Value, error) {
	target, err := r.referenceRealm(strarg(args, 0), strarg(args, 1))
	if err != nil {
		return nil, err
	}
	object := target.crossValues[int64(numarg(args, 2))]
	if object == nil || target.frameTransaction == nil {
		return nil, fmt.Errorf("cross-realm object is no longer available")
	}
	op, wire := strarg(args, 3), strarg(args, 4)
	var encoded string
	run := func(ctx context.Context) error {
		return target.runOnOwner(ctx, func(ctx context.Context) error {
			restore := r.enterFrameDocumentEntry(target)
			defer restore()
			if runtime, ok := target.runtime.(engine.StringCallRuntime); ok {
				var err error
				encoded, err = runtime.CallString(ctx, target.frameTransaction, op, object, wire)
				return err
			}
			operation, payload := target.val(op), target.val(wire)
			value, err := target.runtime.Call(ctx, target.frameTransaction, nil, operation, object, payload)
			if err == nil {
				encoded = value.String()
			}
			// All three values are private, fresh strings. Their native roots
			// must not accumulate over repeated property reads or calls.
			if releaser, ok := target.runtime.(engine.ValueReleaser); ok {
				releaser.ReleaseValue(operation)
				releaser.ReleaseValue(payload)
				releaser.ReleaseValue(value)
			}
			return err
		})
	}
	if target == r {
		err = run(context.Background())
	} else if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
		err = nested.RunNested(context.Background(), run)
	} else {
		err = run(context.Background())
	}
	if err != nil {
		return nil, err
	}
	// Primitive outcomes do not retain an owner. Reference outcomes use the
	// same lifetime graph as the ordinary bridge, including third-realm values.
	// The scan is only a parsing optimization: false positives are harmless.
	if strings.Contains(encoded, `"realm"`) || strings.Contains(encoded, `"frame"`) {
		var data any
		if err := json.Unmarshal([]byte(encoded), &data); err != nil {
			return nil, fmt.Errorf("invalid frame transaction result: %w", err)
		}
		r.retainEncoded(data)
	}
	return r.val(encoded), nil
}
