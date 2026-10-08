// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"fmt"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// mutationObserverReg is one live `new MutationObserver(cb)` instance. It
// tracks at most ONE active observation (the target/options from the most
// recent observe() call) — real pages overwhelmingly use one observer per
// target (see unit.js's own sentinel-div pattern, the case that surfaced
// this gap), and supporting several independently-tracked targets per
// observer is deliberately left for a future round if ever needed live.
type mutationObserverReg struct {
	target            *dom.Node
	subtree           bool
	attributes        bool
	childList         bool
	attributeOldValue bool
	// attributeFilter restricts attribute records to these names when
	// non-nil (a nil slice means "not given" — every attribute matches, per
	// spec's own "options[attributeFilter] does not exist" branch).
	attributeFilter []string
	callback        goja.Callable
	self            *goja.Object
	pending         []goja.Value
	scheduled       bool
}

// installMutationObserver wires a real MutationObserver constructor onto g,
// replacing the inert shared stub every Observer constructor previously used
// (see installConstructors): `observe`'s callback was a no-op, so ANY script
// depending on being notified of a DOM change — the sentinel-div/"is this
// element still there" idiom real sites use for sticky headers, lazy
// widgets, and hydration gates — silently never ran at all. Confirmed via a
// from-scratch, two-line repro (a trivial setAttribute + appendChild) that
// never produced a single record even in the simplest possible case.
//
// IntersectionObserver, ResizeObserver and PerformanceObserver are left as
// the pre-existing no-op stub: unlike MutationObserver, honouring them
// correctly needs real layout geometry/paint-timing integration, a
// substantially larger, separate piece of work.
func (b *binder) installMutationObserver(g *goja.Object) {
	g.Set("MutationObserver", func(call goja.ConstructorCall) *goja.Object {
		cb, _ := goja.AssertFunction(call.Argument(0))
		reg := &mutationObserverReg{callback: cb}
		o := b.vm.NewObject()
		reg.self = o

		o.Set("observe", func(fc goja.FunctionCall) goja.Value {
			target := b.node(fc.Argument(0))
			if target == nil {
				return goja.Undefined()
			}
			opts, _ := fc.Argument(1).(*goja.Object)
			reg.target = target
			reg.subtree = optBool(opts, "subtree")
			reg.childList = optBool(opts, "childList")
			reg.attributeOldValue = optBool(opts, "attributeOldValue")
			reg.attributeFilter = optStringList(opts, "attributeFilter")
			// The DOM standard's own observe(target, options) steps (§4.3.1):
			// "If either options[attributeOldValue] or options[attributeFilter]
			// exists, and options[attributes] does not exist, then set
			// options[attributes] to true" — the common real-world idiom
			// `observe(el, {attributeFilter: ['class']})`, with no explicit
			// `attributes: true`, must still turn attribute observation on.
			// Found by reading the spec's actual algorithm text rather than
			// reasoning from general MutationObserver knowledge (this session's
			// own "bibliography before" discipline) — a measurement-only check
			// would never have surfaced this, since it only breaks a caller
			// that omits the (redundant-seeming) explicit flag.
			reg.attributes = optBool(opts, "attributes") || optPresent(opts, "attributeOldValue") || optPresent(opts, "attributeFilter")
			b.mutationObservers = append(b.mutationObservers, reg)
			return goja.Undefined()
		})
		o.Set("unobserve", func(fc goja.FunctionCall) goja.Value {
			target := b.node(fc.Argument(0))
			b.removeMutationObserver(reg, target)
			return goja.Undefined()
		})
		o.Set("disconnect", func(goja.FunctionCall) goja.Value {
			b.removeMutationObserver(reg, nil)
			reg.pending = nil
			return goja.Undefined()
		})
		o.Set("takeRecords", func(goja.FunctionCall) goja.Value {
			recs := reg.pending
			reg.pending = nil
			return b.recordsArray(recs)
		})
		return o
	})
}

// removeMutationObserver drops reg from the live list, scoped to a specific
// target when given (unobserve), or unconditionally (disconnect).
func (b *binder) removeMutationObserver(reg *mutationObserverReg, target *dom.Node) {
	for i := len(b.mutationObservers) - 1; i >= 0; i-- {
		if b.mutationObservers[i] != reg {
			continue
		}
		if target != nil && reg.target != target {
			continue
		}
		b.mutationObservers = append(b.mutationObservers[:i], b.mutationObservers[i+1:]...)
	}
}

// optBool reads a boolean option, defaulting to false when opts is nil or
// the key is absent — matching MutationObserverInit's own all-optional shape.
func optBool(opts *goja.Object, name string) bool {
	if opts == nil {
		return false
	}
	v := opts.Get(name)
	return v != nil && v.ToBoolean()
}

// optPresent reports whether name is a key of opts at all, distinct from
// optBool's true/false — the DOM standard's implicit-attributes:true rule
// (see observe's own comment above) triggers on the OPTION BEING GIVEN, not
// on its value, so "present but false" must be told apart from "absent".
func optPresent(opts *goja.Object, name string) bool {
	if opts == nil {
		return false
	}
	v := opts.Get(name)
	return v != nil && !goja.IsUndefined(v)
}

// optStringList reads a string-array option (attributeFilter), or nil when
// opts is nil, the key is absent, or the value isn't array-shaped.
func optStringList(opts *goja.Object, name string) []string {
	if opts == nil {
		return nil
	}
	v := opts.Get(name)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	arr, ok := v.Export().([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

// attributeFilterAllows reports whether name passes reg's attributeFilter —
// vacuously true when no filter was given (spec: "options[attributeFilter]
// does not exist").
func attributeFilterAllows(reg *mutationObserverReg, name string) bool {
	if reg.attributeFilter == nil {
		return true
	}
	for _, f := range reg.attributeFilter {
		if f == name {
			return true
		}
	}
	return false
}

// recordChildListMutation notifies every observer whose scope covers parent
// (itself, or an ancestor with subtree set) of a childList change. Called
// once per structural mutation call (appendChild, removeChild, …) — a single
// multi-argument append()/prepend() call fires one record per node rather
// than one batched record, a deliberate simplification over the spec's exact
// batching (see mutationObserverReg's own doc comment for the same trade-off).
func (b *binder) recordChildListMutation(parent *dom.Node, added, removed []*dom.Node) {
	if len(b.mutationObservers) == 0 {
		return
	}
	added, removed = compactNodes(added), compactNodes(removed)
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	for _, reg := range b.mutationObservers {
		if reg.target == nil || !reg.childList {
			continue
		}
		if parent != reg.target && !(reg.subtree && contains(reg.target, parent)) {
			continue
		}
		reg.pending = append(reg.pending, b.newMutationRecord("childList", parent, added, removed, "", nil))
		b.scheduleObserverCallback(reg)
	}
}

// recordAttributeMutation notifies every observer whose scope covers target
// of an attribute change, called from setAttr/removeAttr — the two functions
// every attribute-touching JS binding (setAttribute, className, id, hidden,
// type, …) already funnels through, so hooking them here covers all of it
// for free.
func (b *binder) recordAttributeMutation(target *dom.Node, name, oldVal string, hadOld bool) {
	if len(b.mutationObservers) == 0 {
		return
	}
	for _, reg := range b.mutationObservers {
		if reg.target == nil || !reg.attributes {
			continue
		}
		if target != reg.target && !(reg.subtree && contains(reg.target, target)) {
			continue
		}
		if !attributeFilterAllows(reg, name) {
			continue
		}
		var oldPtr *string
		if reg.attributeOldValue && hadOld {
			v := oldVal
			oldPtr = &v
		}
		reg.pending = append(reg.pending, b.newMutationRecord("attributes", target, nil, nil, name, oldPtr))
		b.scheduleObserverCallback(reg)
	}
}

// scheduleObserverCallback queues reg's callback to run once, as a drained
// job (the closest bounded-event-loop equivalent to a real microtask this
// engine has — see drainTimers), batching every record recorded before the
// job actually runs into one callback invocation. A second mutation before
// the job runs just appends to the same pending list rather than scheduling
// a second call, matching the spec's own per-observer batching.
func (b *binder) scheduleObserverCallback(reg *mutationObserverReg) {
	if reg.scheduled {
		return
	}
	reg.scheduled = true
	b.jobs = append(b.jobs, timerJob{fn: b.wrapJob(func() {
		reg.scheduled = false
		recs := reg.pending
		reg.pending = nil
		if len(recs) == 0 || reg.callback == nil {
			return
		}
		b.callSafely(reg.callback, goja.Undefined(), b.recordsArray(recs), reg.self)
	})})
}

// recordsArray builds a JS array from queued MutationRecord objects.
func (b *binder) recordsArray(recs []goja.Value) goja.Value {
	vals := make([]interface{}, len(recs))
	for i, r := range recs {
		vals[i] = r
	}
	return b.vm.NewArray(vals...)
}

// newMutationRecord builds one MutationRecord. previousSibling/nextSibling
// are left null: matching them correctly around AppendChild's own
// detach-then-reinsert semantics added real complexity no observed real-world
// caller (including unit.js's own sentinel pattern) actually reads.
func (b *binder) newMutationRecord(kind string, target *dom.Node, added, removed []*dom.Node, attrName string, oldVal *string) *goja.Object {
	o := b.vm.NewObject()
	o.Set("type", kind)
	o.Set("target", b.wrap(target))
	o.Set("addedNodes", b.wrapList(added))
	o.Set("removedNodes", b.wrapList(removed))
	o.Set("previousSibling", goja.Null())
	o.Set("nextSibling", goja.Null())
	if attrName != "" {
		o.Set("attributeName", attrName)
	} else {
		o.Set("attributeName", goja.Null())
	}
	o.Set("attributeNamespace", goja.Null())
	if oldVal != nil {
		o.Set("oldValue", *oldVal)
	} else {
		o.Set("oldValue", goja.Null())
	}
	return o
}

// compactNodes drops nil entries (a no-op operand from removeChild/appendChild
// on an already-detached or missing node) so they never appear in addedNodes/
// removedNodes.
func compactNodes(nodes []*dom.Node) []*dom.Node {
	out := make([]*dom.Node, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			out = append(out, n)
		}
	}
	return out
}
