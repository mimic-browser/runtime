//go:build (windows || linux) && amd64

package gov8

import "unsafe"

// ExceptionStateReferences supplies the native callback retained by platform
// exception constructors in snapshot producers and consumers.
func ExceptionStateReferences() ([]ExternalReference, error) {
	if err := loadShim(); err != nil {
		return nil, err
	}
	refs := make([]ExternalReference, 1)
	for index := range refs {
		var address uintptr
		status, _, _ := proc("gov8_exception_state_callback_address").Call(uintptr(index), uintptr(unsafe.Pointer(&address)))
		if int64(status) < 0 {
			return nil, shimError("ExceptionState.Reference", status)
		}
		refs[index] = NewExternalReference(address)
	}
	return refs, nil
}

// NewExceptionStateFactory creates the realm-owned platform exception initializer.
func (i *Isolate) NewExceptionStateFactory(scope *Scope, context *Context) (Value, error) {
	if err := scope.check(); err != nil {
		return Value{}, err
	}
	if err := context.check(); err != nil {
		return Value{}, err
	}
	if scope.iso != i || context.iso != i {
		return Value{}, foreignIsolate("exception state factory")
	}
	handle, err := callHandle("ExceptionState.Factory", proc("gov8_exception_state_factory_new"), i.handleAssumingCheck(), scope.handle, context.handle)
	if err != nil {
		return Value{}, err
	}
	return Value{iso: i, sc: scope, h: handle}, nil
}
