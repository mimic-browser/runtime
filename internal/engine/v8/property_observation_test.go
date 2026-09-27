//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"
)

func TestPropertyObservationSnapshot(t *testing.T) {
	ordinary := (Factory{}).New()
	defer ordinary.Close()
	if err := ordinary.Set("factory", ordinary.(*adapter).PropertyObservationFactory()); err != nil {
		t.Fatal(err)
	}
	before, err := ordinary.Eval(context.Background(), `
globalThis.reads=[];globalThis.observed=factory((object,key,write)=>reads.push([key,write])); observed.answer=42; void observed.answer; void observed.missing; JSON.stringify(reads)`, "before")
	if err != nil || before.Export() != `[["answer",true],["answer",false],["missing",false]]` {
		t.Fatalf("ordinary observation: %v %v", before, err)
	}
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `

const factory=globalThis.__mimicPropertyObservationFactory;
globalThis.reads=[];
globalThis.observed=factory((object,key,write)=>reads.push([key,write]));
observed.answer=42;
delete globalThis.__mimicPropertyObservationFactory;
`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for index := 0; index < 2; index++ {
		runtime, err := snapshot.NewRuntime()
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtime.Eval(context.Background(), `
observed.answer===42 && reads.length===2 && reads[1][0]==='answer' && reads[1][1]===false`, "observation-test")
		if err != nil || value.Export() != true {
			diagnostic, diagnosticErr := runtime.Eval(context.Background(), `
JSON.stringify({answer:observed.answer,reads})`, "diagnostic")
			t.Fatalf("restored observation: %v %v; %v %v", value, err, diagnostic, diagnosticErr)
		}
		if err := runtime.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPropertyObservationObjectSemantics(t *testing.T) {
	runtime := (Factory{}).New()
	defer runtime.Close()
	if err := runtime.Set("factory", runtime.(*adapter).PropertyObservationFactory()); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.Eval(context.Background(), `

const assert = (condition, label) => { if (!condition) throw Error(label); };
for (const operation of ['preventExtensions', 'seal', 'freeze']) {
  const object = factory(() => {});
  const prototype = { inherited: 3 };
  Object.setPrototypeOf(object, prototype);
  object.value = 1;
  object[0] = 4;
  object[Symbol.for('probe')] = 5;
  Object[operation](object);
  assert(!Object.isExtensible(object), operation + ': extensible');
  assert(object.value === 1 && object.inherited === 3 && object[0] === 4, operation + ': values');
  assert(!Reflect.set(object, 'newProperty', 2), operation + ': new property');
  assert(!Reflect.defineProperty(object, 'newProperty', { value: 2 }), operation + ': define');
  assert(Object.keys(object).join(',') === '0,value', operation + ': keys');
  assert(Object.getPrototypeOf(object) === prototype, operation + ': prototype');
  const frozen = operation === 'freeze';
  const sealed = operation !== 'preventExtensions';
  assert(Object.isFrozen(object) === frozen && Object.isSealed(object) === sealed, operation + ': integrity');
  assert(Reflect.set(object, 'value', 6) === !frozen, operation + ': existing property');
  assert(Reflect.deleteProperty(object, '0') === !sealed, operation + ': deletion');
  assert(Object.getOwnPropertyDescriptor(object, Symbol.for('probe')).value === 5, operation + ': symbol');
}

const traps = [];
const prototype = new Proxy({}, {
  get(object, property, receiver) { traps.push('get:' + String(property)); return 7; },
  has() { traps.push('has'); return true; },
  getOwnPropertyDescriptor() { traps.push('descriptor'); return undefined; },
});
const inherited = factory(() => {});
Object.setPrototypeOf(inherited, prototype);
assert(inherited.answer === 7, 'prototype Proxy result');
assert(traps.join(',') === 'get:answer', 'observation must not trigger additional author traps');
const receiver = {};
const object = factory(() => {});
Object.defineProperty(object, 'accessor', {
  get() { return this; },
  set(value) { this.received = value; },
  configurable: true,
});
assert(Reflect.get(object, 'accessor', receiver) === receiver, 'getter receiver');
assert(Reflect.set(object, 'accessor', 42, receiver) && receiver.received === 42, 'setter receiver');
const sentinel = {};
Object.defineProperty(object, 'failure', { get() { throw sentinel; } });
try { object.failure; throw Error('missing exception'); } catch (error) { assert(error === sentinel, 'thrown identity'); }
`, "observation-reflection")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPropertyObservationStatusAndExceptions(t *testing.T) {
	runtime := (Factory{}).New()
	defer runtime.Close()
	if err := runtime.Set("factory", runtime.(*adapter).PropertyObservationFactory()); err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), `

const reads = [];
const object = factory((holder, key, write, supported) => reads.push([key, write, supported]));
Object.setPrototypeOf(object, { inherited: 9 });
Object.defineProperty(object, 'present', { value: 7 });
Object.defineProperty(object, '3', { value: 11 });
reads.length = 0;
void object.present;
void object.inherited;
void object[3];
void object.absent;
const expected = JSON.stringify([
  ['present', false, true],
  ['inherited', false, true],
  ['3', false, true],
  ['absent', false, false],
]);
if (JSON.stringify(reads) !== expected) throw Error('property status: ' + JSON.stringify(reads));
const before = reads.length;
Reflect.has(object, 'present');
Reflect.has(object, 'absent');
Object.getOwnPropertyDescriptors(object);
Reflect.ownKeys(object);
if (reads.length !== before) throw Error('reflection performed observable reads');
const sentinel = {};
const failure = factory(() => { throw sentinel; });
try { failure.value; throw Error('missing observer failure'); }
catch (error) { if (error !== sentinel) throw Error('observer failure identity'); }
true;
`, "observation-status")
	if err != nil || value.Export() != true {
		t.Fatalf("observation status: %v %v", value, err)
	}
}
