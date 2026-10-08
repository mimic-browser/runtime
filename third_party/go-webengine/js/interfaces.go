// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"strings"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// The DOM/BOM interface constructor hierarchy.
//
// core-js and React-DOM don't just call DOM methods — they reach the interface
// *constructors* as first-class values: core-js's DOMException polyfill reads
// `getBuiltIn('DOMException').prototype`, and React-DOM (and custom-element code)
// does `class X extends HTMLElement {}` and tests `node instanceof HTMLElement`.
// A binding that only exposes plain wrapper objects has none of that, so the
// polyfill layer throws `Cannot read property 'prototype' of undefined` before
// any app code runs.
//
// installInterfaces builds the standard interfaces as real goja constructables
// with correct prototype chains (HTMLElement → Element → Node → EventTarget →
// Object, Text → CharacterData → Node, MouseEvent → UIEvent → Event, …), exposes
// them as globals, and records each interface's `.prototype` so wrap() can stamp
// every DOM wrapper with the right one. The result: `el instanceof HTMLElement`
// holds, `class C extends HTMLElement {}` works, and `new DOMException(m,name)`
// carries the right name/code — all wired to the existing dom.Node model.

// ifaceSpec is one interface: its global name and its parent interface ("" means
// it descends directly from Object).
type ifaceSpec struct {
	name   string
	parent string
}

// interfaceHierarchy lists every interface constructor to install, each BEFORE
// any child that extends it (so the parent prototype exists when the child links
// to it). It is intentionally broad but bounded: the node/character/document
// interfaces the wrapper model uses, the event interfaces constructed by
// hydration code, and the exception/collection/observer types core-js probes.
var interfaceHierarchy = []ifaceSpec{
	// Exceptions.
	{"DOMException", ""},

	// Core node chain.
	{"EventTarget", ""},
	{"Node", "EventTarget"},
	{"Element", "Node"},
	{"HTMLElement", "Element"},
	{"SVGElement", "Element"},
	{"CharacterData", "Node"},
	{"Text", "CharacterData"},
	{"Comment", "CharacterData"},
	{"CDATASection", "Text"},
	{"ProcessingInstruction", "CharacterData"},
	{"DocumentType", "Node"},
	{"Attr", "Node"},
	{"DocumentFragment", "Node"},
	{"ShadowRoot", "DocumentFragment"},
	{"Document", "Node"},
	{"HTMLDocument", "Document"},
	{"XMLDocument", "Document"},

	// Common concrete HTML element interfaces (all extend HTMLElement) — enough
	// that `createElement('div') instanceof HTMLDivElement` and the handful
	// React-DOM name-checks resolve. Unmapped tags fall back to HTMLElement.
	{"HTMLUnknownElement", "HTMLElement"},
	{"HTMLDivElement", "HTMLElement"},
	{"HTMLSpanElement", "HTMLElement"},
	{"HTMLAnchorElement", "HTMLElement"},
	{"HTMLParagraphElement", "HTMLElement"},
	{"HTMLImageElement", "HTMLElement"},
	{"HTMLInputElement", "HTMLElement"},
	{"HTMLButtonElement", "HTMLElement"},
	{"HTMLTextAreaElement", "HTMLElement"},
	{"HTMLSelectElement", "HTMLElement"},
	{"HTMLOptionElement", "HTMLElement"},
	{"HTMLFormElement", "HTMLElement"},
	{"HTMLLabelElement", "HTMLElement"},
	{"HTMLUListElement", "HTMLElement"},
	{"HTMLOListElement", "HTMLElement"},
	{"HTMLLIElement", "HTMLElement"},
	{"HTMLHeadingElement", "HTMLElement"},
	{"HTMLBodyElement", "HTMLElement"},
	{"HTMLHtmlElement", "HTMLElement"},
	{"HTMLHeadElement", "HTMLElement"},
	{"HTMLScriptElement", "HTMLElement"},
	{"HTMLStyleElement", "HTMLElement"},
	{"HTMLLinkElement", "HTMLElement"},
	{"HTMLMetaElement", "HTMLElement"},
	{"HTMLTemplateElement", "HTMLElement"},
	{"HTMLCanvasElement", "HTMLElement"},
	{"HTMLTableElement", "HTMLElement"},
	{"HTMLTableRowElement", "HTMLElement"},
	{"HTMLTableCellElement", "HTMLElement"},
	{"HTMLIFrameElement", "HTMLElement"},
	{"HTMLTimeElement", "HTMLElement"},
	{"HTMLPreElement", "HTMLElement"},
	{"HTMLBRElement", "HTMLElement"},
	{"HTMLHRElement", "HTMLElement"},
	{"HTMLPictureElement", "HTMLElement"},
	{"HTMLSourceElement", "HTMLElement"},
	{"HTMLMediaElement", "HTMLElement"},
	{"HTMLVideoElement", "HTMLMediaElement"},
	{"HTMLAudioElement", "HTMLMediaElement"},

	// Event chain.
	{"Event", ""},
	{"UIEvent", "Event"},
	{"MouseEvent", "UIEvent"},
	{"PointerEvent", "MouseEvent"},
	{"WheelEvent", "MouseEvent"},
	{"DragEvent", "MouseEvent"},
	{"KeyboardEvent", "UIEvent"},
	{"FocusEvent", "UIEvent"},
	{"InputEvent", "UIEvent"},
	{"CompositionEvent", "UIEvent"},
	{"TouchEvent", "UIEvent"},
	{"CustomEvent", "Event"},
	{"ErrorEvent", "Event"},
	{"MessageEvent", "Event"},
	{"CloseEvent", "Event"},
	{"PromiseRejectionEvent", "Event"},
	{"PopStateEvent", "Event"},
	{"HashChangeEvent", "Event"},
	{"PageTransitionEvent", "Event"},
	{"StorageEvent", "Event"},
	{"ProgressEvent", "Event"},
	{"AnimationEvent", "Event"},
	{"TransitionEvent", "Event"},
	{"ClipboardEvent", "Event"},
	{"SubmitEvent", "Event"},
	{"BeforeUnloadEvent", "Event"},

	// Collections / maps / misc structural types core-js and DOM code probe. They
	// are rarely `new`'d directly but are read as `X.prototype` and used in
	// instanceof checks, so they need to exist with a chain.
	{"NodeList", ""},
	{"HTMLCollection", ""},
	{"NamedNodeMap", ""},
	{"DOMTokenList", ""},
	{"DOMStringMap", ""},
	{"CSSStyleDeclaration", ""},
	{"CSSStyleSheet", ""},
	{"StyleSheet", ""},
	{"MediaQueryList", "EventTarget"},
	{"MutationRecord", ""},
	{"DOMImplementation", ""},
	{"DOMParser", ""},
	{"XMLSerializer", ""},
	{"Range", ""},
	{"Selection", ""},
	{"AbortSignal", "EventTarget"},
	{"AbortController", ""},
	{"DOMRect", ""},
	{"DOMRectReadOnly", ""},
	{"DOMPoint", ""},
	{"DOMPointReadOnly", ""},
	{"DOMMatrix", ""},
	{"DOMMatrixReadOnly", ""},
}

// nodeTypeConstants are the Node.ELEMENT_NODE et al. constants, exposed on both
// the Node constructor and Node.prototype (a browser puts them on both).
var nodeTypeConstants = map[string]int{
	"ELEMENT_NODE": 1, "ATTRIBUTE_NODE": 2, "TEXT_NODE": 3, "CDATA_SECTION_NODE": 4,
	"ENTITY_REFERENCE_NODE": 5, "ENTITY_NODE": 6, "PROCESSING_INSTRUCTION_NODE": 7,
	"COMMENT_NODE": 8, "DOCUMENT_NODE": 9, "DOCUMENT_TYPE_NODE": 10,
	"DOCUMENT_FRAGMENT_NODE": 11, "NOTATION_NODE": 12,
	"DOCUMENT_POSITION_DISCONNECTED": 1, "DOCUMENT_POSITION_PRECEDING": 2,
	"DOCUMENT_POSITION_FOLLOWING": 4, "DOCUMENT_POSITION_CONTAINS": 8,
	"DOCUMENT_POSITION_CONTAINED_BY": 16, "DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC": 32,
}

// domExceptionCodes is the legacy name→code table (DOMException.code).
var domExceptionCodes = map[string]int{
	"IndexSizeError": 1, "HierarchyRequestError": 3, "WrongDocumentError": 4,
	"InvalidCharacterError": 5, "NoModificationAllowedError": 7, "NotFoundError": 8,
	"NotSupportedError": 9, "InUseAttributeError": 10, "InvalidStateError": 11,
	"SyntaxError": 12, "InvalidModificationError": 13, "NamespaceError": 14,
	"InvalidAccessError": 15, "SecurityError": 18, "NetworkError": 19,
	"AbortError": 20, "URLMismatchError": 21, "QuotaExceededError": 22,
	"TimeoutError": 23, "InvalidNodeTypeError": 24, "DataCloneError": 25,
}

// tagInterface maps an HTML tag to its concrete element interface. Unmapped HTML
// tags use HTMLElement; unmapped/unknown ones still resolve there via protoForTag.
var tagInterface = map[string]string{
	"div": "HTMLDivElement", "span": "HTMLSpanElement", "a": "HTMLAnchorElement",
	"p": "HTMLParagraphElement", "img": "HTMLImageElement", "input": "HTMLInputElement",
	"button": "HTMLButtonElement", "textarea": "HTMLTextAreaElement", "select": "HTMLSelectElement",
	"option": "HTMLOptionElement", "form": "HTMLFormElement", "label": "HTMLLabelElement",
	"ul": "HTMLUListElement", "ol": "HTMLOListElement", "li": "HTMLLIElement",
	"h1": "HTMLHeadingElement", "h2": "HTMLHeadingElement", "h3": "HTMLHeadingElement",
	"h4": "HTMLHeadingElement", "h5": "HTMLHeadingElement", "h6": "HTMLHeadingElement",
	"body": "HTMLBodyElement", "html": "HTMLHtmlElement", "head": "HTMLHeadElement",
	"script": "HTMLScriptElement", "style": "HTMLStyleElement", "link": "HTMLLinkElement",
	"meta": "HTMLMetaElement", "template": "HTMLTemplateElement", "canvas": "HTMLCanvasElement",
	"table": "HTMLTableElement", "tr": "HTMLTableRowElement", "td": "HTMLTableCellElement",
	"th": "HTMLTableCellElement", "iframe": "HTMLIFrameElement", "time": "HTMLTimeElement",
	"pre": "HTMLPreElement", "br": "HTMLBRElement", "hr": "HTMLHRElement",
	"picture": "HTMLPictureElement", "source": "HTMLSourceElement", "video": "HTMLVideoElement",
	"audio": "HTMLAudioElement",
}

// svgTags are the SVG tags whose elements descend from SVGElement, not HTMLElement.
var svgTags = map[string]bool{
	"svg": true, "path": true, "g": true, "circle": true, "rect": true, "line": true,
	"polyline": true, "polygon": true, "ellipse": true, "text": true, "tspan": true,
	"defs": true, "use": true, "symbol": true, "clippath": true, "lineargradient": true,
	"radialgradient": true, "stop": true, "mask": true, "pattern": true, "filter": true,
}

// installInterfaces builds the interface constructor hierarchy, wires the globals,
// and records the prototypes for wrap() to stamp onto DOM wrappers.
func (b *binder) installInterfaces(g *goja.Object) {
	b.protos = map[string]*goja.Object{}
	objProto := b.objectPrototype()

	for _, spec := range interfaceHierarchy {
		proto := b.vm.NewObject()
		parentProto := objProto
		if spec.parent != "" {
			if p, ok := b.protos[spec.parent]; ok {
				parentProto = p
			}
		}
		if parentProto != nil {
			_ = proto.SetPrototype(parentProto)
		}
		ctor := b.newInterfaceCtor(spec.name, proto)
		_ = ctor.Set("prototype", proto)
		_ = proto.DefineDataProperty("constructor", ctor, goja.FLAG_TRUE, goja.FLAG_FALSE, goja.FLAG_TRUE)
		b.protos[spec.name] = proto
		g.Set(spec.name, ctor)
	}

	// Node carries the nodeType constants on the constructor and its prototype.
	if nodeCtor := b.ctorOf(g, "Node"); nodeCtor != nil {
		for name, val := range nodeTypeConstants {
			nodeCtor.Set(name, val)
		}
	}
	if nodeProto := b.protos["Node"]; nodeProto != nil {
		for name, val := range nodeTypeConstants {
			nodeProto.Set(name, val)
		}
	}

	// EventTarget.prototype gets the three listener methods so a synthetic
	// `new EventTarget()` (core-js's AbortSignal base) is callable.
	if etp := b.protos["EventTarget"]; etp != nil {
		noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
		etp.Set("addEventListener", noop)
		etp.Set("removeEventListener", noop)
		etp.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(true) })
	}
}

// objectPrototype returns the runtime's Object.prototype (root of every chain).
func (b *binder) objectPrototype() *goja.Object {
	if oc, ok := b.vm.GlobalObject().Get("Object").(*goja.Object); ok {
		if p, ok := oc.Get("prototype").(*goja.Object); ok {
			return p
		}
	}
	return nil
}

// ctorOf returns the constructor object previously set as global name.
func (b *binder) ctorOf(g *goja.Object, name string) *goja.Object {
	if c, ok := g.Get(name).(*goja.Object); ok {
		return c
	}
	return nil
}

// newInterfaceCtor builds the goja constructable for one interface. Structural
// interfaces return call.This unchanged (so `class C extends X {}` and direct
// `new X()` both yield an instance with the correct prototype); DOMException and
// the Event family populate the instance with their real fields.
func (b *binder) newInterfaceCtor(name string, proto *goja.Object) *goja.Object {
	var fn func(goja.ConstructorCall) *goja.Object
	switch {
	case name == "DOMException":
		fn = func(call goja.ConstructorCall) *goja.Object {
			return b.constructDOMException(call, proto)
		}
	case b.isEventInterface(name):
		fn = func(call goja.ConstructorCall) *goja.Object {
			o := call.This
			if o == nil {
				o = b.vm.CreateObject(proto)
			}
			b.populateEvent(o, call.Argument(0).String(), call.Argument(1), name)
			return o
		}
	default:
		// Structural interface: honour the (possibly subclassed) call.This.
		fn = func(call goja.ConstructorCall) *goja.Object { return nil }
	}
	return b.vm.ToValue(fn).(*goja.Object)
}

// isEventInterface reports whether name is in the Event constructor family.
func (b *binder) isEventInterface(name string) bool {
	switch name {
	case "Event", "UIEvent", "MouseEvent", "PointerEvent", "WheelEvent", "DragEvent",
		"KeyboardEvent", "FocusEvent", "InputEvent", "CompositionEvent", "TouchEvent",
		"CustomEvent", "ErrorEvent", "MessageEvent", "CloseEvent", "PromiseRejectionEvent",
		"PopStateEvent", "HashChangeEvent", "PageTransitionEvent", "StorageEvent",
		"ProgressEvent", "AnimationEvent", "TransitionEvent", "ClipboardEvent",
		"SubmitEvent", "BeforeUnloadEvent":
		return true
	}
	return false
}

// constructDOMException builds a DOMException instance (message/name/code) on the
// (possibly subclassed) instance, matching the legacy code table.
func (b *binder) constructDOMException(call goja.ConstructorCall, proto *goja.Object) *goja.Object {
	o := call.This
	if o == nil {
		o = b.vm.CreateObject(proto)
	}
	message := ""
	if a := call.Argument(0); !goja.IsUndefined(a) {
		message = a.String()
	}
	exName := "Error"
	if a := call.Argument(1); !goja.IsUndefined(a) && !goja.IsNull(a) {
		exName = a.String()
	}
	o.Set("message", message)
	o.Set("name", exName)
	o.Set("code", domExceptionCodes[exName]) // 0 when the name has no legacy code
	o.Set("stack", "")
	o.Set("toString", func(goja.FunctionCall) goja.Value {
		if message == "" {
			return b.vm.ToValue(exName)
		}
		return b.vm.ToValue(exName + ": " + message)
	})
	return o
}

// populateEvent fills o with the fields for an Event of the given interface,
// applying the init dict. It is the shared body for the whole Event family.
func (b *binder) populateEvent(o *goja.Object, typ string, init goja.Value, iface string) {
	o.Set("type", typ)
	o.Set("bubbles", false)
	o.Set("cancelable", false)
	o.Set("composed", false)
	o.Set("defaultPrevented", false)
	o.Set("target", goja.Null())
	o.Set("currentTarget", goja.Null())
	o.Set("srcElement", goja.Null())
	o.Set("eventPhase", 0)
	o.Set("timeStamp", 0)
	o.Set("isTrusted", false)
	o.Set("returnValue", true)
	o.Set("cancelBubble", false)
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	o.Set("preventDefault", func(goja.FunctionCall) goja.Value {
		o.Set("defaultPrevented", true)
		o.Set("returnValue", false)
		return goja.Undefined()
	})
	o.Set("stopPropagation", noop)
	o.Set("stopImmediatePropagation", noop)
	o.Set("composedPath", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
	o.Set("initEvent", func(call goja.FunctionCall) goja.Value {
		o.Set("type", call.Argument(0).String())
		o.Set("bubbles", call.Argument(1).ToBoolean())
		o.Set("cancelable", call.Argument(2).ToBoolean())
		return goja.Undefined()
	})
	applyEventInit(o, init)

	// Interface-specific fields the hydration layer reads.
	obj, hasInit := init.(*goja.Object)
	getInit := func(k string) goja.Value {
		if hasInit {
			if v := obj.Get(k); v != nil {
				return v
			}
		}
		return goja.Undefined()
	}
	switch iface {
	case "CustomEvent":
		o.Set("detail", orNull(getInit("detail")))
	case "MouseEvent", "PointerEvent", "WheelEvent", "DragEvent":
		for _, k := range []string{"clientX", "clientY", "screenX", "screenY", "pageX", "pageY", "button", "buttons"} {
			o.Set(k, numOr(getInit(k), 0))
		}
		for _, k := range []string{"ctrlKey", "shiftKey", "altKey", "metaKey"} {
			o.Set(k, boolOr(getInit(k)))
		}
		o.Set("relatedTarget", orNull(getInit("relatedTarget")))
	case "KeyboardEvent":
		o.Set("key", strOr(getInit("key")))
		o.Set("code", strOr(getInit("code")))
		o.Set("keyCode", numOr(getInit("keyCode"), 0))
		o.Set("which", numOr(getInit("which"), 0))
		o.Set("location", numOr(getInit("location"), 0))
		o.Set("repeat", boolOr(getInit("repeat")))
		for _, k := range []string{"ctrlKey", "shiftKey", "altKey", "metaKey"} {
			o.Set(k, boolOr(getInit(k)))
		}
	case "InputEvent", "CompositionEvent":
		o.Set("data", strOr(getInit("data")))
	case "MessageEvent":
		o.Set("data", orNull(getInit("data")))
		o.Set("origin", strOr(getInit("origin")))
	case "ProgressEvent":
		o.Set("lengthComputable", boolOr(getInit("lengthComputable")))
		o.Set("loaded", numOr(getInit("loaded"), 0))
		o.Set("total", numOr(getInit("total"), 0))
	case "ErrorEvent":
		o.Set("message", strOr(getInit("message")))
		o.Set("filename", strOr(getInit("filename")))
		o.Set("error", orNull(getInit("error")))
	case "PopStateEvent":
		o.Set("state", orNull(getInit("state")))
	case "HashChangeEvent":
		o.Set("oldURL", strOr(getInit("oldURL")))
		o.Set("newURL", strOr(getInit("newURL")))
	}
}

// protoForNode returns the interface prototype a wrapper for n should carry.
func (b *binder) protoForNode(n *dom.Node) *goja.Object {
	if b.protos == nil {
		return nil
	}
	if n.Type == dom.Text {
		return b.protos["Text"]
	}
	switch n.Tag {
	case "#fragment":
		return b.protos["DocumentFragment"]
	case "#document":
		return b.protos["Document"]
	}
	return b.protoForTag(n.Tag)
}

// protoForTag resolves an element tag to its interface prototype, falling back to
// HTMLElement (SVG tags use SVGElement).
func (b *binder) protoForTag(tag string) *goja.Object {
	t := strings.ToLower(tag)
	if svgTags[t] {
		if p := b.protos["SVGElement"]; p != nil {
			return p
		}
	}
	if iface, ok := tagInterface[t]; ok {
		if p := b.protos[iface]; p != nil {
			return p
		}
	}
	return b.protos["HTMLElement"]
}

// --- init-dict coercion helpers ---------------------------------------------

func orNull(v goja.Value) goja.Value {
	if v == nil || goja.IsUndefined(v) {
		return goja.Null()
	}
	return v
}

func strOr(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}

func numOr(v goja.Value, def int64) int64 {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return def
	}
	return v.ToInteger()
}

func boolOr(v goja.Value) bool {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return false
	}
	return v.ToBoolean()
}
