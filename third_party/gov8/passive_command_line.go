//go:build (windows || linux) && amd64

package gov8

import (
	"errors"
	"runtime"
	"unsafe"
)

// CommandLineResult identifies a raw native evaluation result. No Inspector
// RemoteObject, preview, description or author-value serialization is created.
type CommandLineResult int32

const (
	CommandLineNotRun CommandLineResult = iota
	CommandLineSuccess
	CommandLineException
)

// EvaluateCommandLine creates an ephemeral native Inspector command-line scope.
// selected is ordered oldest to newest. last is the owning client's $_ value.
// The returned local belongs to scope; all Inspector state is closed on return.
func (c *Context) EvaluateCommandLine(scope *Scope, source InspectorStringView, selected []Value, last Value) (Value, CommandLineResult, error) {
	if err := c.checkAssumingIsolate(); err != nil {
		return Value{}, CommandLineNotRun, err
	}
	if scope == nil || scope.iso != c.iso {
		return Value{}, CommandLineNotRun, foreignIsolate("scope")
	}
	if err := scope.check(); err != nil {
		return Value{}, CommandLineNotRun, err
	}
	if err := scope.requireCurrent(); err != nil {
		return Value{}, CommandLineNotRun, err
	}
	if len(selected) > 5 {
		return Value{}, CommandLineNotRun, errors.New("gov8: at most five command-line selections are supported")
	}
	handles := make([]uintptr, len(selected))
	for index, value := range selected {
		if value.h == 0 {
			continue
		}
		if value.iso != c.iso {
			return Value{}, CommandLineNotRun, foreignIsolate("selected value")
		}
		if err := value.check(); err != nil {
			return Value{}, CommandLineNotRun, err
		}
		handles[index] = value.h
	}
	if last.h != 0 {
		if last.iso != c.iso {
			return Value{}, CommandLineNotRun, foreignIsolate("last result")
		}
		if err := last.check(); err != nil {
			return Value{}, CommandLineNotRun, err
		}
	}
	is8, data, length := source.native()
	var status int32
	var raw uintptr
	err := callErr("Context.EvaluateCommandLine", proc("gov8_passive_command_line_eval"), c.iso.handleAssumingCheck(), scope.handle, c.handle, is8, data, length, slicePointer(handles), uintptr(len(handles)), last.h, uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&raw)))
	runtime.KeepAlive(source)
	runtime.KeepAlive(handles)
	if err != nil {
		return Value{}, CommandLineNotRun, err
	}
	if status < 0 || status > int32(CommandLineException) || (status != 0 && raw == 0) {
		return Value{}, CommandLineNotRun, errors.New("gov8: invalid passive command-line result")
	}
	if raw == 0 {
		return Value{}, CommandLineResult(status), nil
	}
	return Value{iso: c.iso, sc: scope, h: raw}, CommandLineResult(status), nil
}
