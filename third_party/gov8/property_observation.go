//go:build (windows || linux) && amd64

package gov8

import "unsafe"

// PropertyObservationReferences contains the stable native callbacks required
// by observed objects and their factory in both snapshot creators and consumers.
// No Go callback registry entries or Page state are serialized.
func PropertyObservationReferences() ([]ExternalReference, error) {
	if err := loadShim(); err != nil {
		return nil, err
	}
	references := make([]ExternalReference, 7)
	for index := range references {
		var address uintptr
		result, _, _ := proc("gov8_observation_callback_address").Call(uintptr(index), uintptr(unsafe.Pointer(&address)))
		if int64(result) < 0 {
			return nil, shimError("PropertyObservation.Reference", result)
		}
		references[index] = NewExternalReference(address)
	}
	return references, nil
}

// NewPropertyObservationFactory returns a nonconstructible native function.
// Its observer finishes before ordinary property lookup/write continues, so
// observation cannot add frames to an exception produced by that operation.
func (i *Isolate) NewPropertyObservationFactory(scope *Scope, context *Context) (Value, error) {
	if err := scope.check(); err != nil {
		return Value{}, err
	}
	if err := context.check(); err != nil {
		return Value{}, err
	}
	if scope.iso != i || context.iso != i {
		return Value{}, foreignIsolate("property observation factory")
	}
	handle, err := callHandle("PropertyObservation.Factory", proc("gov8_observation_factory_new"), i.handleAssumingCheck(), scope.handle, context.handle)
	if err != nil {
		return Value{}, err
	}
	return Value{iso: i, sc: scope, h: handle}, nil
}
