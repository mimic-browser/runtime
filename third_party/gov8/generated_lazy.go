//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	"unsafe"
)

// GeneratedLazyReferences returns the two process-lifetime callbacks used by
// generated lazy properties. Snapshot creators and consumers must configure
// both addresses in the same order. The callback's per-property data is an
// integer; no Go closure is retained or serialized with a property.
func GeneratedLazyReferences() ([]ExternalReference, error) {
	if err := loadShim(); err != nil {
		return nil, err
	}
	references := make([]ExternalReference, 2)
	for index := range references {
		var address uintptr
		status, _, _ := proc("gov8_generated_lazy_callback_address").Call(
			uintptr(index), uintptr(unsafe.Pointer(&address)))
		if int64(status) < 0 {
			return nil, shimError("GeneratedLazy.Reference", status)
		}
		references[index] = NewExternalReference(address)
	}
	return references, nil
}

// NewGeneratedLazyInstaller creates a native function for one context. Its
// one-argument form registers the realm-local factory: installer(factory).
// Its four-argument form installs a lazy data property:
// installer(target, name, nonnegativeMemberID, attributes). On first access,
// V8 invokes factory(memberID, holder, propertyName) and replaces the slot
// with the returned value. This function and its lazy getter can be serialized
// into a snapshot when GeneratedLazyReferences is configured.
func (i *Isolate) NewGeneratedLazyInstaller(scope *Scope, context *Context) (Value, error) {
	if err := scope.check(); err != nil {
		return Value{}, err
	}
	if err := context.check(); err != nil {
		return Value{}, err
	}
	if scope.iso != i || context.iso != i {
		return Value{}, foreignIsolate("generated lazy installer")
	}
	handle, err := callHandle("GeneratedLazy.Installer",
		proc("gov8_generated_lazy_installer_new"),
		i.handleAssumingCheck(), scope.handle, context.handle)
	if err != nil {
		return Value{}, err
	}
	return Value{iso: i, sc: scope, h: handle}, nil
}

// SetGeneratedLazyDataProperty places a snapshot-portable lazy data property
// on every object instantiated from this template. The current context must
// have a factory registered by a generated lazy installer before first read.
func (t *ObjectTemplate) SetGeneratedLazyDataProperty(key string, memberID int32, attr PropertyAttribute) error {
	if t == nil {
		return fmt.Errorf("gov8: nil object template")
	}
	if err := t.check(); err != nil {
		return err
	}
	if memberID < 0 {
		return fmt.Errorf("gov8: generated lazy member ID is negative")
	}
	if err := validPropertyAttribute(attr); err != nil {
		return err
	}
	name, err := t.sc.NewString(key)
	if err != nil {
		return err
	}
	return callErr("ObjectTemplate.SetGeneratedLazyDataProperty",
		proc("gov8_generated_lazy_template_set"),
		t.iso.handleAssumingCheck(), t.sc.handle, t.h, name.h,
		uintptr(memberID), uintptr(attr))
}

// GeneratedConstructorReferences returns the native factory and constructor
// callback addresses. Snapshot creators and consumers must use the same order.
func GeneratedConstructorReferences() ([]ExternalReference, error) {
	if err := loadShim(); err != nil {
		return nil, err
	}
	references := make([]ExternalReference, 2)
	for index := range references {
		var address uintptr
		status, _, _ := proc("gov8_generated_constructor_callback_address").Call(
			uintptr(index), uintptr(unsafe.Pointer(&address)))
		if int64(status) < 0 {
			return nil, shimError("GeneratedConstructor.Reference", status)
		}
		references[index] = NewExternalReference(address)
	}
	return references, nil
}

// NewGeneratedConstructorFactory creates the native generated-interface
// constructor factory for a context. JavaScript first calls factory(dispatcher)
// once, then factory(name, nonnegativeMemberID) for each constructor.
// Constructor calls forward to dispatcher(id, receiver, arguments, newTarget),
// with undefined newTarget for a normal call. The returned function is
// constructible, named, and has length zero.
func (i *Isolate) NewGeneratedConstructorFactory(scope *Scope, context *Context) (Value, error) {
	if err := scope.check(); err != nil {
		return Value{}, err
	}
	if err := context.check(); err != nil {
		return Value{}, err
	}
	if scope.iso != i || context.iso != i {
		return Value{}, foreignIsolate("generated constructor factory")
	}
	handle, err := callHandle("GeneratedConstructor.Factory",
		proc("gov8_generated_constructor_factory_new"),
		i.handleAssumingCheck(), scope.handle, context.handle)
	if err != nil {
		return Value{}, err
	}
	return Value{iso: i, sc: scope, h: handle}, nil
}
