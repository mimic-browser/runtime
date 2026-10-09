package browser

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/moreveal/mimic/internal/engine"
)

func (r *Realm) installFrameDocumentBridge(host map[string]any) {
	host["frameTransaction"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.transactFrame(args)
	})
	host["retainWindowReference"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.retainWindowReference(strarg(args, 0))
		return nil, nil
	})
	host["selfRealmID"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.ID), nil })
	host["frameEvalAllowed"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		// Frozen Chrome gates a retained eval function against the current
		// Window's origin before argument handling, unlike ordinary functions.
		return r.val(r.canAccess(r.agent.Page().frame(strarg(args, 0)))), nil
	})
	host["frameCanAccess"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(args, 0), strarg(args, 1))
		return r.val(err == nil && target != nil), nil
	})
	r.installFrameReflection(host)
	if err := r.prepareFrameTransaction(); err != nil {
		r.frameReflection.err = err
	}
	// Describe references in one invocation on their owner. Otherwise every
	// type/identity/shape query makes a separate round trip to the V8 actor.
	// This private function is never exposed to document scripts.
	r.frameValueEncoder = r.val(r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		encoded, err := r.describeCrossRealmValue(args[0])
		if err != nil {
			return nil, err
		}
		r.retainEncoded(encoded)
		return r.val(encoded), nil
	}))
	r.frameValueRetain = r.val(r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.crossValueSeq++
		r.crossValues[r.crossValueSeq] = args[0]
		return r.val(r.crossValueSeq), nil
	}))
	host["installFrameReferenceBridge"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		// A restored snapshot must supply the complete current bridge contract.
		// Reject corrupt bindings before indexing arguments or retaining callbacks,
		// so initialization can discard the snapshot and retry the ordinary path.
		if len(args) != 9 {
			return nil, fmt.Errorf("frame reference bridge requires 9 arguments, got %d", len(args))
		}
		for index, callback := range args[:8] {
			if r.runtime.TypeOf(callback) != "function" {
				return nil, fmt.Errorf("frame reference bridge callback %d is not a function", index)
			}
		}
		if r.runtime.TypeOf(args[8]) != "object" {
			return nil, fmt.Errorf("frame reference bridge requires a parent-node intrinsic holder")
		}
		if r.frameReferenceImport != nil {
			return nil, fmt.Errorf("frame reference bridge already installed")
		}
		r.frameReferenceImport, r.frameReferenceDescribe = args[0], args[1]
		if len(args) > 7 {
			r.frameEventDescribe = args[7]
		}
		if len(args) > 5 {
			r.frameNativeNameDescribe = args[5]
		}
		if len(args) > 6 {
			r.frameSourceDescribe = args[6]
		}
		if len(args) > 4 {
			r.frameBindingDescribe = args[4]
		}
		if len(args) > 3 {
			r.frameGlobalRead = args[3]
		}
		if len(args) > 2 {
			r.frameNodeDescribe = args[2]
		}
		if err := r.installFrameValueEncoder(args[8]); err != nil {
			return nil, err
		}
		return nil, nil
	})
	host["frameReference"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		value := args[0]
		return r.crossFrameResult(r, func(context.Context) (engine.Value, error) { return value, nil })
	})
	host["frameResolve"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if strarg(args, 1) != r.ID {
			return nil, fmt.Errorf("cross-realm object is no longer available")
		}
		value := r.crossValues[int64(numarg(args, 0))]
		if value == nil {
			return nil, fmt.Errorf("cross-realm object is no longer available")
		}
		return value, nil
	})
	host["frameConstruct"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(args, 0), strarg(args, 4))
		if err != nil {
			return nil, err
		}
		function := target.crossValues[int64(numarg(args, 1))]
		if function == nil {
			return nil, fmt.Errorf("cross-realm constructor is no longer available")
		}
		raw, _ := arg(args, 2).([]any)
		rawNewTarget, _ := arg(args, 3).(map[string]any)
		if rawNewTarget["kind"] != "reference" || rawNewTarget["type"] != "function" {
			return nil, fmt.Errorf("NotSupportedError: cross-realm construction requires a remote newTarget")
		}
		return r.crossFrameData(target, func(ctx context.Context) (any, error) {
			newTarget, err := target.decodeFrameArgument(rawNewTarget)
			if err != nil {
				return nil, err
			}
			// Build the argument array in its owning realm; exporting native
			// values here would lose object identity and invoke user getters.
			arguments, err := target.callFrameReflection(ctx, "array", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			for index, encoded := range raw {
				value, err := target.decodeFrameArgument(encoded)
				if err != nil {
					return nil, err
				}
				if _, err := target.callFrameReflection(ctx, "set", arguments, target.val(strconv.Itoa(index)), value); err != nil {
					return nil, err
				}
			}
			restore := r.enterFrameDocumentEntry(target)
			defer restore()
			record, err := target.callFrameReflection(ctx, "construct", function, arguments, newTarget)
			if err != nil {
				return nil, err
			}
			encoded, err := target.encodeReflectedValue(record)
			if err != nil {
				return nil, err
			}
			return map[string]any{"threw": target.runtime.GetProperty(record, "threw").Export(), "value": encoded}, nil
		})
	})
	host["frameGlobalHas"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(args, 0), "")
		if err != nil {
			return nil, err
		}
		rawKey := arg(args, 1)
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			key, err := target.decodeFrameKey(ctx, rawKey)
			if err != nil {
				return nil, err
			}
			return target.callFrameReflection(ctx, "has", target.runtime.Get("globalThis"), key, nil)
		})
	})
	host["frameGlobalSet"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		frame := r.agent.Page().frame(strarg(args, 0))
		if !r.canAccess(frame) {
			return nil, fmt.Errorf("SecurityError: Blocked cross-origin frame access")
		}
		target, err := r.worldForFrame(frame)
		if err != nil {
			return nil, err
		}
		rawKey, rawValue := arg(args, 1), arg(args, 2)
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			key, err := target.decodeFrameKey(ctx, rawKey)
			if err != nil {
				return nil, err
			}
			value, err := target.decodeFrameArgument(rawValue)
			if err != nil {
				return nil, err
			}
			return target.callFrameReflection(ctx, "set", target.runtime.Get("globalThis"), key, value)
		})
	})
	host["frameSet"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(args, 0), strarg(args, 4))
		if err != nil {
			return nil, err
		}
		object := target.crossValues[int64(numarg(args, 1))]
		if object == nil {
			return nil, fmt.Errorf("cross-realm object is no longer available")
		}
		rawKey, rawValue := arg(args, 2), arg(args, 3)
		return r.crossFrameData(target, func(ctx context.Context) (any, error) {
			key, err := target.decodeFrameKey(ctx, rawKey)
			if err != nil {
				return nil, err
			}
			value, err := target.decodeFrameArgument(rawValue)
			if err != nil {
				return nil, err
			}
			result, err := target.callFrameReflection(ctx, "setOutcome", object, key, value)
			if err != nil {
				return nil, err
			}
			encoded, err := target.encodeReflectedValue(result)
			if err != nil {
				return nil, err
			}
			return map[string]any{"threw": target.runtime.GetProperty(result, "threw").Export(), "value": encoded}, nil
		})
	})
}

// Arguments use an envelope created from private proxy slots. User objects are
// carried as values; their properties cannot masquerade as a browser handle.
func (r *Realm) decodeFrameArgument(raw any) (engine.Value, error) {
	argument, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid cross-realm argument")
	}
	switch argument["kind"] {
	case "symbol":
		return r.decodeFrameKey(context.Background(), argument["key"])
	case "bigint":
		return r.callFrameReflection(context.Background(), "bigint", nil, r.val(argument["value"]), nil)
	case "special-number":
		switch argument["value"] {
		case "NaN":
			return r.val(math.NaN()), nil
		case "-0":
			return r.val(math.Copysign(0, -1)), nil
		case "Infinity":
			return r.val(math.Inf(1)), nil
		case "-Infinity":
			return r.val(math.Inf(-1)), nil
		}
		return nil, fmt.Errorf("invalid cross-realm number")
	case "undefined":
		return r.runtime.Get("undefined"), nil
	case "value":
		return r.runtime.Value(argument["value"]), nil
	case "reference":
		frameID, _ := argument["frame"].(string)
		realmID, _ := argument["realm"].(string)
		if argument["type"] == "window" {
			// Window references denote a browsing context and follow navigation.
			if frameID == r.agent.ContextID() {
				return r.runtime.Get("globalThis"), nil
			}
			return r.importFrameReference(map[string]any{"__mimicCrossRealm": "window", "frame": frameID})
		}
		source, err := r.referenceRealm(frameID, realmID)
		if err != nil {
			return nil, err
		}
		value := source.crossValues[int64(numberValue(argument["handle"]))]
		if value == nil {
			return nil, fmt.Errorf("cross-realm reference is no longer available")
		}
		if source == r {
			return value, nil
		}
		r.retainRealm(source)
		encoded, err := source.crossRealmValue(value)
		if err != nil {
			return nil, err
		}
		return r.importFrameReference(encoded)
	default:
		return nil, fmt.Errorf("invalid cross-realm argument kind")
	}
}

func (r *Realm) importFrameReference(encoded map[string]any) (engine.Value, error) {
	if r.frameReferenceImport == nil {
		return nil, fmt.Errorf("frame reference bridge is not initialized")
	}
	argument := r.val(encoded)
	defer releaseRuntimeValues(r.runtime, argument)
	return r.runtime.Call(context.Background(), r.frameReferenceImport, nil, argument)
}

func (r *Realm) callFrameReference(args []engine.Value) (engine.Value, error) {
	target, err := r.referenceRealm(strarg(args, 0), strarg(args, 4))
	if err != nil {
		return nil, err
	}
	function := target.crossValues[int64(numarg(args, 1))]
	if function == nil {
		return nil, fmt.Errorf("cross-realm function is no longer available")
	}
	raw, _ := arg(args, 2).([]any)
	rawReceiver := arg(args, 3)
	return r.crossFrameData(target, func(ctx context.Context) (any, error) {
		receiver, err := target.decodeFrameArgument(rawReceiver)
		if err != nil {
			return nil, err
		}
		restore := r.enterFrameDocumentEntry(target)
		defer restore()
		operation := "applyOutcome"
		var arguments engine.Value
		if arg(args, 5) == true {
			operation = "arrayIteratorOutcome"
		} else {
			arguments, err = target.callFrameReflection(ctx, "array", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			for index, encoded := range raw {
				value, err := target.decodeFrameArgument(encoded)
				if err != nil {
					return nil, err
				}
				if err := target.runtime.SetProperty(arguments, strconv.Itoa(index), value); err != nil {
					return nil, err
				}
			}
		}
		var record engine.Value
		if arg(args, 6) == true {
			event, e := target.decodeFrameArgument(raw[0])
			if e != nil {
				return nil, e
			}
			record, err = target.runtime.Call(ctx, target.eventListenerInvoker, nil, function, receiver, event, target.val(arg(args, 7) == true), target.val(arg(args, 8) == true), target.val(arg(args, 9) == true))
		} else {
			record, err = target.callFrameReflection(ctx, operation, function, receiver, arguments)
		}
		if err != nil {
			return nil, err
		}
		encoded, err := target.encodeReflectedValue(record)
		if err != nil {
			return nil, err
		}
		return map[string]any{"threw": target.runtime.GetProperty(record, "threw").Export(), "value": encoded}, nil
	})
}

func (r *Realm) crossFrameResult(target *Realm, operation func(context.Context) (engine.Value, error)) (engine.Value, error) {
	var encoded map[string]any
	run := func(ctx context.Context) error {
		return target.runOnOwner(ctx, func(ctx context.Context) error {
			restore := r.enterFrameDocumentEntry(target)
			defer restore()
			value, err := operation(ctx)
			if err != nil {
				return err
			}
			encoded, err = target.crossRealmValue(value)
			return err
		})
	}
	var err error
	if target == r {
		// Local argument references already execute on this owner. Suspending
		// its callback and starting a cooperating goroutine adds no ordering.
		err = run(context.Background())
	} else if calls, ok := r.runtime.(engine.RealmCallRuntime); ok {
		err = calls.RunRealmCall(context.Background(), target.runtime, run)
	} else if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
		err = nested.RunNested(context.Background(), run)
	} else {
		err = run(context.Background())
	}
	if err != nil {
		return nil, err
	}
	r.retainEncoded(encoded)
	return r.val(encoded), nil
}

func (r *Realm) runOnOwner(ctx context.Context, operation func(context.Context) error) error {
	if owner, ok := r.runtime.(engine.OwnerRuntime); ok {
		return owner.RunOnOwner(ctx, operation)
	}
	return operation(ctx)
}

// The entry document follows the synchronous cross-realm call stack. Restore
// it before separately scheduled jobs run, so a later child timer is not
// attributed to the parent that previously evaluated code in the child.
func (r *Realm) enterFrameDocumentEntry(target *Realm) func() {
	p := r.agent.Page()
	p.requireCheckpoint(target)
	p.crossRealmDepth++
	entry := r.documentEntry
	if entry == nil {
		entry = r
	}
	previous := target.documentEntry
	target.documentEntry = entry
	return func() {
		target.documentEntry = previous
		p.crossRealmDepth--
	}
}

type frameReflection struct {
	operation     engine.Value
	err           error
	symbolSources map[string]int64
	symbolOrigins map[int64]map[string]any
}

// Capture intrinsics before document scripts can replace Reflect or Symbol.
// Reflection uses the remote canonical object, never exported object copies.
const frameReflectionSource = `(() => {
  const create = Object.create,
    descriptorFields = ['enumerable', 'configurable', 'value', 'writable', 'get', 'set'];
  const arrayIteratorNext = Object.getPrototypeOf([][Symbol.iterator]()).next,
    freshIteratorResults = new WeakSet(),
    freshAdd = WeakSet.prototype.add,
    freshDelete = WeakSet.prototype.delete;
  const ids = new WeakMap(),
    weakGet = WeakMap.prototype.get,
    weakSet = WeakMap.prototype.set,
    P = Proxy,
    isArray = Array.isArray,
    integer = BigInt,
    keys = Reflect.ownKeys,
    descriptor = Reflect.getOwnPropertyDescriptor,
    get = Reflect.get,
    set = Reflect.set,
    define = Reflect.defineProperty,
    remove = Reflect.deleteProperty,
    contains = Reflect.has,
    prototype = Reflect.getPrototypeOf,
    extensible = Reflect.isExtensible,
    preventExtensions = Reflect.preventExtensions,
    construct = Reflect.construct,
    apply = Reflect.apply,
    has = Object.prototype.hasOwnProperty,
    S = Symbol,
    forKey = Symbol.for,
    keyFor = Symbol.keyFor,
    description = descriptor(Symbol.prototype, 'description').get;
  const wellKnown = [],
    names = keys(Symbol);
  for (let i = 0; i < names.length; i++) {
    const name = names[i];
    if (typeof Symbol[name] === 'symbol') wellKnown[wellKnown.length] = [name, Symbol[name]];
  }
  const info = (key) => {
    if (typeof key === 'string') return { kind: 'string', value: key };
    const result = { kind: 'symbol', key, description: apply(description, key, []) };
    for (let i = 0; i < wellKnown.length; i++)
      if (wellKnown[i][1] === key) {
        result.wellKnown = wellKnown[i][0];
        return result;
      }
    const global = keyFor(key);
    if (global !== undefined) result.global = global;
    return result;
  };
  return (op, object, key, value) => {
    if (op === 'iteratorNext') return object === arrayIteratorNext;
    if (op === 'arrayIteratorStep') {
      const result = apply(object, key, []);
      if (object === arrayIteratorNext) apply(freshAdd, freshIteratorResults, [result]);
      return result;
    }
    if (op === 'takeIteratorResult') return apply(freshDelete, freshIteratorResults, [object]);
    if (op === 'lookup') return apply(weakGet, ids, [object]);
    if (op === 'handle') {
      let id = apply(weakGet, ids, [object]);
      if (id === undefined) {
        id = key;
        apply(weakSet, ids, [object, id]);
      }
      return id;
    }
    if (op === 'array') return [];
    if (op === 'object') return create(null);
    if (op === 'shape') {
      if (typeof object !== 'function') return { array: isArray(object) };
      let constructable = true;
      try {
        construct(
          new P(object, {
            construct() {
              return {};
            },
          }),
          [],
        );
      } catch (error) {
        constructable = false;
      }
      return { constructable };
    }
    if (op === 'bigint') return integer(key);
    if (op === 'prototype') return prototype(object);
    if (op === 'extensible') return extensible(object);
    if (op === 'preventExtensionsOutcome') {
      let value,
        threw = false;
      try {
        value = preventExtensions(object);
      } catch (error) {
        value = error;
        threw = true;
      }
      return { threw, value };
    }
    if (op === 'apply') return apply(object, key, value);
    if (op === 'applyOutcome' || op === 'arrayIteratorOutcome') {
      let result,
        threw = false;
      try {
        result = apply(object, key, op === 'arrayIteratorOutcome' ? [] : value);
        if (op === 'arrayIteratorOutcome' && object === arrayIteratorNext)
          apply(freshAdd, freshIteratorResults, [result]);
      } catch (error) {
        result = error;
        threw = true;
      }
      return {
        threw,
        value: result,
        valueType: typeof result,
        symbol: typeof result === 'symbol' ? info(result) : null,
      };
    }
    if (op === 'construct') {
      let result,
        threw = false;
      try {
        result = construct(object, key, value);
      } catch (error) {
        result = error;
        threw = true;
      }
      return {
        threw,
        value: result,
        valueType: typeof result,
        symbol: typeof result === 'symbol' ? info(result) : null,
      };
    }
    if (op === 'key') {
      if (key.wellKnown !== undefined) {
        for (let i = 0; i < wellKnown.length; i++)
          if (wellKnown[i][0] === key.wellKnown) return wellKnown[i][1];
      }
      if (key.global !== undefined) return forKey(key.global);
      return S(key.description);
    }
    if (op === 'symbol') return info(object);
    if (op === 'keys') {
      const source = keys(object),
        out = [];
      for (let i = 0; i < source.length; i++) out[i] = info(source[i]);
      return out;
    }
    if (op === 'get') {
      const value = get(object, key);
      return {
        value,
        valueType: typeof value,
        symbol: typeof value === 'symbol' ? info(value) : null,
      };
    }
    if (op === 'mutateDefine' || op === 'mutateDelete') {
      let result,
        threw = false;
      try {
        if (op === 'mutateDefine') {
          // Materialize presence before passing the descriptor to the engine.
          // goja's descriptor conversion treats a Proxy's missing get/set
          // (undefined) as present, incorrectly mixing data and accessor fields.
          const local = create(null);
          for (let i = 0; i < descriptorFields.length; i++) {
            const field = descriptorFields[i];
            if (contains(value, field)) local[field] = get(value, field);
          }
          result = define(object, key, local);
        } else result = remove(object, key);
      } catch (error) {
        result = error;
        threw = true;
      }
      return {
        threw,
        value: result,
        valueType: typeof result,
        symbol: typeof result === 'symbol' ? info(result) : null,
      };
    }
    if (op === 'setOutcome') {
      let result,
        threw = false;
      try {
        result = set(object, key, value);
      } catch (error) {
        result = error;
        threw = true;
      }
      return {
        threw,
        value: result,
        valueType: typeof result,
        symbol: typeof result === 'symbol' ? info(result) : null,
      };
    }
    if (op === 'set') return set(object, key, value);
    if (op === 'define') return define(object, key, value);
    if (op === 'delete') return remove(object, key);
    if (op === 'has') return contains(object, key);
    const d = descriptor(object, key);
    if (d === undefined) return { exists: false };
    const accessor = !apply(has, d, ['value']);
    return accessor
      ? {
          exists: true,
          accessor: true,
          enumerable: d.enumerable,
          configurable: d.configurable,
          get: d.get,
          set: d.set,
        }
      : {
          exists: true,
          accessor: false,
          enumerable: d.enumerable,
          configurable: d.configurable,
          writable: d.writable,
          value: d.value,
          valueType: typeof d.value,
          symbol: typeof d.value === 'symbol' ? info(d.value) : null,
        };
  };
})();`

// Classify references in JavaScript, where the canonical WeakMap, intrinsics
// and wrappers live. Only the first export of an object needs a host retention
// callback. Repeated reads still inspect live shape (including revoked proxies)
// and never cache arbitrary property values, prototypes or access checks.
// Fresh intrinsic Array iterator results are the sole data-field exception;
// the importer invalidates those fields before mutation or reference escape.
const frameValueEncoderSource = `((describe,node,reflect,retain,symbol,frame,realm,parent,binding,nativeName,functionSource,eventState,intrinsicParentNode)=>{
 const global=globalThis,intrinsicEval=globalThis.eval,intrinsicCall=Function.prototype.call,stringify=JSON.stringify,create=Object.create,keys=Object.keys,event=typeof eventState==='function'?eventState:()=>null;
 const plain=data=>{const out=create(null),names=keys(data);for(let i=0;i<names.length;i++){const key=names[i];out[key]=data[key]}return out};
 const encode=value=>{
  const type=typeof value;
  if(value===undefined)return {__mimicCrossRealm:'undefined'};
  if(value===null)return {__mimicCrossRealm:'null'};
  if(type==='symbol')return symbol(value);
  if(type==='bigint')return {__mimicCrossRealm:'bigint',value:''+value};
  if(type==='number'&&(value!==value||value===Infinity||value===-Infinity||value===0&&1/value===-Infinity))return {__mimicCrossRealm:'special-number',value:value!==value?'NaN':value===0?'-0':value>0?'Infinity':'-Infinity'};
  if(type!=='object'&&type!=='function'&&type!=='undefined')return {__mimicCrossRealm:'value',value};
  const reference=describe(value);
  if(reference!==null&&typeof reference==='object')return reference;
  if(value===global)return {__mimicCrossRealm:'window',frame};
  if(parent&&value===global.parent)return {__mimicCrossRealm:'window',frame:parent};
  let id=reflect('lookup',value);
  if(id===undefined)id=reflect('handle',value,retain(value));
  const out=plain({__mimicCrossRealm:type==='undefined'?'undetectable':type,frame,realm,handle:id});
  const bound=binding(value);if(bound)out.binding=plain({kind:bound.kind,invoke:plain(encode(bound.invoke)),unpreventable:bound.unpreventable});
  if(value===global.document)out.document=true;
  if(value===intrinsicEval)out.eval=true;
  if(type==='object'){const nodeId=node(value);if(nodeId)out.nodeId=nodeId;out.array=reflect('shape',value).array;const state=event(value);if(state)out.eventState=plain(encode(state))}
  else if(type==='function'){out.constructable=reflect('shape',value).constructable;if(value===intrinsicCall)out.intrinsicCall=true;if(value===intrinsicParentNode.getter)out.intrinsicParentNode=true;if(reflect('iteratorNext',value))out.iteratorNext=true;const name=nativeName(value);if(name!==undefined)out.nativeName=name;else if(functionSource)out.functionSource=functionSource(value)}
  if(type==='object'&&reflect('takeIteratorResult',value)){
   out.iteratorResult=create(null);out.iteratorResult.done=plain(encode(value.done));
   // Do not inspect object-valued results early: e.g. a revoked Proxy must
   // not throw during next() merely because its later import would throw.
   const item=value.value,t=typeof item;
   if(item===undefined||item===null||t==='string'||t==='number'||t==='boolean'||t==='bigint')out.iteratorResult.value=plain(encode(item));
  }
  return out;
 };
 return value=>{const data=encode(value),out=create(null);const names=keys(data);for(let i=0;i<names.length;i++){const key=names[i];out[key]=data[key]}return stringify(out)};
})`

func (r *Realm) installFrameValueEncoder(parentNodeGetter engine.Value) error {
	factory, err := evalPlatformExpression(r.runtime, frameValueEncoderSource, "mimic:frame-value-encoder")
	if err != nil {
		return err
	}

	parent := ""
	if frame, ok := r.agent.(*Frame); ok && frame.parent != nil {
		parent = frame.parent.ID
	}
	encoder, err := r.runtime.Call(context.Background(), factory, nil, r.frameReferenceDescribe, r.frameNodeDescribe, r.frameReflection.operation, r.frameValueRetain, r.frameValueEncoder, r.val(r.agent.ContextID()), r.val(r.ID), r.val(parent), r.frameBindingDescribe, r.frameNativeNameDescribe, r.frameSourceDescribe, r.frameEventDescribe, parentNodeGetter)
	if err == nil {
		r.frameValueEncoder = encoder
		r.frameValueEncoderJSON = true
		err = r.installFrameTransaction()
	}
	return err
}

func (r *Realm) installFrameReflection(host map[string]any) {
	operation, err := evalPlatformExpression(r.runtime, frameReflectionSource, "mimic:frame-reflection")
	r.frameReflection = &frameReflection{operation: operation, err: err, symbolSources: make(map[string]int64), symbolOrigins: make(map[int64]map[string]any)}
	host["frameMutateProperty"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(args, 0), strarg(args, 5))
		if err != nil {
			return nil, err
		}
		object := target.crossValues[int64(numarg(args, 1))]
		if object == nil {
			return nil, fmt.Errorf("cross-realm object is no longer available")
		}
		operation := strarg(args, 2)
		if operation != "mutateDefine" && operation != "mutateDelete" {
			return nil, fmt.Errorf("invalid cross-realm property mutation")
		}
		return r.crossFrameData(target, func(ctx context.Context) (any, error) {
			key, err := target.decodeFrameKey(ctx, arg(args, 3))
			if err != nil {
				return nil, err
			}
			value, err := target.decodeFrameArgument(arg(args, 4))
			if err != nil {
				return nil, err
			}
			record, err := target.callFrameReflection(ctx, operation, object, key, value)
			if err != nil {
				return nil, err
			}
			encoded, err := target.encodeReflectedValue(record)
			if err != nil {
				return nil, err
			}
			return map[string]any{"threw": target.runtime.GetProperty(record, "threw").Export(), "value": encoded}, nil
		})
	})
	for _, name := range []string{"frameOwnKeys", "frameDescriptor", "frameHas"} {
		name := name
		host[name] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			realmIndex := 2
			if name != "frameOwnKeys" {
				realmIndex = 3
			}
			target, err := r.referenceRealm(strarg(args, 0), strarg(args, realmIndex))
			if err != nil {
				return nil, err
			}
			object := target.crossValues[int64(numarg(args, 1))]
			if object == nil {
				return nil, fmt.Errorf("cross-realm object is no longer available")
			}
			var rawKey any
			if name != "frameOwnKeys" {
				rawKey = arg(args, 2)
			}
			return r.crossFrameData(target, func(ctx context.Context) (any, error) {
				if name == "frameOwnKeys" {
					keys, err := target.callFrameReflection(ctx, "keys", object, nil, nil)
					if err != nil {
						return nil, err
					}
					length := int(numberValue(target.runtime.GetProperty(keys, "length").Export()))
					result := make([]any, 0, length)
					for index := 0; index < length; index++ {
						result = append(result, target.encodeFrameKey(target.runtime.GetProperty(keys, strconv.Itoa(index))))
					}
					return result, nil
				}
				key, err := target.decodeFrameKey(ctx, rawKey)
				if err != nil {
					return nil, err
				}
				if name == "frameHas" {
					result, err := target.callFrameReflection(ctx, "has", object, key, nil)
					if err != nil {
						return nil, err
					}
					return result.Export(), nil
				}
				desc, err := target.callFrameReflection(ctx, "descriptor", object, key, nil)
				if err != nil {
					return nil, err
				}
				field := func(name string) engine.Value { return target.runtime.GetProperty(desc, name) }
				if field("exists").Export() != true {
					return map[string]any{"exists": false}, nil
				}
				accessor := field("accessor").Export() == true
				out := map[string]any{"exists": true, "accessor": accessor, "enumerable": field("enumerable").Export(), "configurable": field("configurable").Export()}
				if accessor {
					out["get"], err = target.crossRealmValue(field("get"))
					if err != nil {
						return nil, err
					}
					out["set"], err = target.crossRealmValue(field("set"))
				} else {
					out["writable"] = field("writable").Export()
					out["value"], err = target.encodeReflectedValue(desc)
				}
				return out, err
			})
		})
	}
}

func (r *Realm) callFrameReflection(ctx context.Context, operation string, object, key, value engine.Value) (engine.Value, error) {
	// Reflect operations can invoke user accessors or Proxy traps. Their jobs
	// need the same outer checkpoint as an explicit cross-realm function call.
	p := r.agent.Page()
	p.requireCheckpoint(r)
	p.crossRealmDepth++
	defer func() { p.crossRealmDepth-- }()
	if r.frameReflection == nil {
		return nil, fmt.Errorf("frame reflection is not initialized")
	}
	if r.frameReflection.err != nil {
		return nil, r.frameReflection.err
	}
	if object == nil {
		object = r.runtime.Get("undefined")
	}
	if key == nil {
		key = r.runtime.Get("undefined")
	}
	if value == nil {
		value = r.runtime.Get("undefined")
	}
	return r.runtime.Call(ctx, r.frameReflection.operation, nil, r.val(operation), object, key, value)
}

func (r *Realm) encodeFrameKey(info engine.Value) map[string]any {
	field := func(name string) engine.Value { return r.runtime.GetProperty(info, name) }
	if field("kind").String() == "string" {
		return map[string]any{"kind": "string", "value": field("value").Export()}
	}
	key := field("key")
	var handle int64
	for id, existing := range r.crossValues {
		if r.runtime.StrictEqual(key, existing) {
			handle = id
			break
		}
	}
	if handle == 0 {
		r.crossValueSeq++
		handle = r.crossValueSeq
		r.crossValues[handle] = key
	}
	out := map[string]any{"kind": "symbol", "realm": r.ID, "handle": handle}
	for key, value := range r.frameReflection.symbolOrigins[handle] {
		out[key] = value
	}
	for _, name := range []string{"wellKnown", "global", "description"} {
		value := field(name)
		if r.runtime.TypeOf(value) != "undefined" {
			out[name] = value.Export()
		}
	}
	return out
}

func (r *Realm) decodeFrameKey(ctx context.Context, raw any) (engine.Value, error) {
	if key, ok := raw.(string); ok {
		return r.val(key), nil
	}
	key, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid cross-realm property key")
	}
	if key["kind"] == "string" {
		return r.val(key["value"]), nil
	}
	if key["kind"] != "symbol" {
		return nil, fmt.Errorf("invalid cross-realm property key kind")
	}
	if realm, ok := key["realm"].(string); ok && realm != "" {
		if realm == r.ID {
			value := r.crossValues[int64(numberValue(key["handle"]))]
			if value == nil {
				return nil, fmt.Errorf("cross-realm symbol is no longer available")
			}
			return value, nil
		}
		source, err := r.referenceRealm("", realm)
		if err != nil {
			return nil, err
		}
		r.retainRealm(source)
		sourceKey := realm + ":handle:" + strconv.FormatInt(int64(numberValue(key["handle"])), 10)
		if id := r.frameReflection.symbolSources[sourceKey]; id != 0 {
			return r.crossValues[id], nil
		}
		encoded := make(map[string]any, len(key)+1)
		for name, value := range key {
			encoded[name] = value
		}
		encoded["__mimicCrossRealm"] = "symbol"
		// The JS importer owns the symbol cache used by ownKeys and property
		// access. Import arguments through that same cache, not a fresh Symbol.
		value, err := r.importFrameReference(encoded)
		if err != nil {
			return nil, err
		}
		r.crossValueSeq++
		id := r.crossValueSeq
		r.crossValues[id] = value
		r.frameReflection.symbolSources[sourceKey] = id
		r.frameReflection.symbolOrigins[id] = map[string]any{"realm": realm, "handle": key["handle"]}
		return value, nil
	}
	var sourceKey string
	if source, ok := key["sourceRealm"].(string); ok && source != "" {
		sourceKey = source + ":" + strconv.FormatInt(int64(numberValue(key["localID"])), 10)
		if id := r.frameReflection.symbolSources[sourceKey]; id != 0 {
			return r.crossValues[id], nil
		}
	}
	value, err := r.callFrameReflection(ctx, "key", nil, r.val(key), nil)
	if err == nil && sourceKey != "" {
		r.crossValueSeq++
		id := r.crossValueSeq
		r.crossValues[id] = value
		r.frameReflection.symbolSources[sourceKey] = id
		r.frameReflection.symbolOrigins[id] = map[string]any{"sourceRealm": key["sourceRealm"], "localID": key["localID"]}
	}
	return value, err
}

func (r *Realm) encodeReflectedValue(record engine.Value) (map[string]any, error) {
	if r.runtime.GetProperty(record, "valueType").String() == "symbol" {
		encoded := r.encodeFrameKey(r.runtime.GetProperty(record, "symbol"))
		encoded["__mimicCrossRealm"] = "symbol"
		return encoded, nil
	}
	return r.crossRealmValue(r.runtime.GetProperty(record, "value"))
}

func (r *Realm) reflectFrameGet(target *Realm, object engine.Value, rawKey any) (engine.Value, error) {
	return r.crossFrameData(target, func(ctx context.Context) (any, error) {
		key, err := target.decodeFrameKey(ctx, rawKey)
		if err != nil {
			return nil, err
		}
		record, err := target.callFrameReflection(ctx, "get", object, key, nil)
		if err != nil {
			return nil, err
		}
		return target.encodeReflectedValue(record)
	})
}

func (r *Realm) crossFrameData(target *Realm, operation func(context.Context) (any, error)) (engine.Value, error) {
	var data any
	// Keep reflection and descriptor/key encoding on the same owner turn.
	// Returning between individual field reads otherwise adds an actor trip
	// per field (and thousands of trips for Window ownKeys).
	run := func(ctx context.Context) error {
		return target.runOnOwner(ctx, func(ctx context.Context) error {
			var err error
			data, err = operation(ctx)
			return err
		})
	}
	var err error
	if target == r {
		err = run(context.Background())
	} else if calls, ok := r.runtime.(engine.RealmCallRuntime); ok {
		err = calls.RunRealmCall(context.Background(), target.runtime, run)
	} else if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
		err = nested.RunNested(context.Background(), run)
	} else {
		err = run(context.Background())
	}
	if err != nil {
		return nil, err
	}
	r.retainEncoded(data)
	return r.val(data), nil
}
