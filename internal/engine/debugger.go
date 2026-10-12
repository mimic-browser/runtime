package engine

// DebuggerValueRuntime supplies native brands without author reflection or
// incompatible-receiver probes. error-opaque marks an Error whose descriptor
// lookup can itself format its lazy stack; passive reflection must reject it.
type DebuggerValueRuntime interface {
	DebuggerValueKind(Value) (string, error)
	IsDebuggerProxy(Value) (bool, error)
}

// DebuggerDescriptorRuntime provides passive descriptors when an engine's
// public descriptor intrinsic can observe author prototype setters while
// constructing its result. The returned record is private transport metadata.
type DebuggerDescriptorRuntime interface {
	DebuggerOwnPropertyDescriptor(Value, Value) (Value, error)
}
