//go:build (windows || linux) && amd64

package gov8

import "unsafe"

// ReceiverDispatchReferences supplies the stable native callbacks used by the
// realm-owned factory and generated methods in snapshot producers and consumers.
func ReceiverDispatchReferences() ([]ExternalReference, error) {
	if err := loadShim(); err != nil {
		return nil, err
	}
	refs := make([]ExternalReference, 2)
	for index := range refs {
		var address uintptr
		status, _, _ := proc("gov8_receiver_dispatch_callback_address").Call(uintptr(index), uintptr(unsafe.Pointer(&address)))
		if int64(status) < 0 {
			return nil, shimError("ReceiverDispatch.Reference", status)
		}
		refs[index] = NewExternalReference(address)
	}
	return refs, nil
}

// NewReceiverDispatchFactory creates nonconstructible native receiver gates.
// Their uncached template data remains context-owned during serialization.
func (i *Isolate) NewReceiverDispatchFactory(scope *Scope, context *Context) (Value, error) {
	if err := scope.check(); err != nil {
		return Value{}, err
	}
	if err := context.check(); err != nil {
		return Value{}, err
	}
	if scope.iso != i || context.iso != i {
		return Value{}, foreignIsolate("receiver dispatch factory")
	}
	handle, err := callHandle("ReceiverDispatch.Factory", proc("gov8_receiver_dispatch_factory_new"), i.handleAssumingCheck(), scope.handle, context.handle)
	if err != nil {
		return Value{}, err
	}
	return Value{iso: i, sc: scope, h: handle}, nil
}
