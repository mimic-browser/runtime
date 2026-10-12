package engine

// InspectorRuntime exposes the native engine debugger transport without
// translating author values through the browser runtime. Implementations keep
// the transport confined to the engine owner thread.
type InspectorRuntime interface {
	InspectorOwnerID() uint64
	ConnectInspector(func([]byte)) (InspectorSession, error)
}

// InspectorSession carries complete CDP messages to and from the native
// engine inspector. Notifications are delivered through the callback supplied
// to ConnectInspector.
type InspectorSession interface {
	DispatchInspector([]byte) ([]byte, error)
	CloseInspector() error
}
