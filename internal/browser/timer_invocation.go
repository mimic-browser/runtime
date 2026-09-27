package browser

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/moreveal/mimic/internal/engine"
)

// Timer callbacks enter through the engine, without JavaScript dispatch frames.
// The registration owns its arguments; public errors use the realm's reporter.
type timerInvocation struct {
	function  engine.Value
	reporter  engine.Value
	arguments []engine.Value
	source    string
}

func newTimerInvocation(runtime engine.Runtime, values []engine.Value) (timerInvocation, error) {
	invocation := timerInvocation{}
	if runtime.TypeOf(values[0]) == "function" {
		invocation.function = retainRuntimeValue(runtime, values[0])
	} else {
		invocation.source = strarg(values, 0)
	}
	if len(values) > 3 && runtime.TypeOf(values[3]) == "function" {
		invocation.reporter = retainRuntimeValue(runtime, values[3])
	}
	if len(values) > 4 && invocation.function != nil {
		length := runtime.GetProperty(values[4], "length")
		if length == nil {
			invocation.release(runtime)
			return timerInvocation{}, fmt.Errorf("invalid timer argument list")
		}
		count, err := strconv.Atoi(length.String())
		releaseRuntimeValues(runtime, length)
		if err != nil || count < 0 {
			invocation.release(runtime)
			return timerInvocation{}, fmt.Errorf("invalid timer argument list")
		}
		invocation.arguments = make([]engine.Value, count)
		for i := range invocation.arguments {
			value := runtime.GetProperty(values[4], strconv.Itoa(i))
			invocation.arguments[i] = retainRuntimeValue(runtime, value)
			releaseRuntimeValues(runtime, value)
		}
	}

	return invocation, nil
}

func (invocation *timerInvocation) release(runtime engine.Runtime) {
	releaseRuntimeValues(runtime, invocation.function, invocation.reporter)
	releaseRuntimeValues(runtime, invocation.arguments...)
	invocation.function, invocation.reporter = nil, nil
	invocation.arguments = nil
	invocation.source = ""
}

func (invocation *timerInvocation) invoke(ctx context.Context, runtime engine.Runtime, receiver engine.Value, sourceBlocked bool) error {
	// Self-cancellation releases the registration during the author call. Keep
	// error reporting alive until that call has either returned or thrown.
	reporter := retainRuntimeValue(runtime, invocation.reporter)
	defer releaseRuntimeValues(runtime, reporter)
	var result engine.Value
	var err error
	if invocation.function != nil {
		result, err = runtime.Call(ctx, invocation.function, receiver, invocation.arguments...)
	} else if !sourceBlocked {
		result, err = runtime.Eval(ctx, invocation.source, "")
	}
	releaseRuntimeValues(runtime, result)
	var thrown engine.ThrownValue
	if err == nil || reporter == nil || !errors.As(err, &thrown) {
		return err
	}
	reported, reportErr := runtime.Call(ctx, reporter, nil, thrown.ThrownValue())
	releaseRuntimeValues(runtime, reported)
	if reportErr != nil {
		message := err.Error()
		releaseRuntimeValues(runtime, thrown.ThrownValue())
		return fmt.Errorf("timer exception %s; reporting failed: %w", message, reportErr)
	}
	releaseRuntimeValues(runtime, thrown.ThrownValue())
	return nil
}
