package engine

import "context"

// BootstrapSnapshotFactory optionally prepares an immutable, engine-specific
// snapshot from complete, quiescent JavaScript initialization. The source must
// not depend on embedder callbacks or contain live external resources.
type BootstrapSnapshotFactory interface {
	// BootstrapSnapshotsEnabled permits backend diagnostics to request ordinary
	// execution rather than omit the source intervals they intend to measure.
	BootstrapSnapshotsEnabled() bool
	// Sources execute in order in the same seed realm. Separate scripts allow
	// temporary initialization data to be collected independently of closures.
	BuildBootstrapSnapshot(context.Context, ...string) (BootstrapSnapshot, error)
}

// BootstrapSnapshot creates independent runtimes with separately owned realm
// objects. Close prevents subsequent creation without invalidating existing
// runtimes. Implementations support concurrent creation and Close.
type BootstrapSnapshot interface {
	NewRuntime() (Runtime, error)
	Close() error
	// SizeBytes reports the immutable serialized size, including after Close.
	SizeBytes() int
}

// BareBootstrapSnapshot creates the first ordinary realm directly in a
// snapshot-backed isolate without deserializing an unused platform context.
// Its caller still installs the normal cold bootstrap in that bare realm.
type BareBootstrapSnapshot interface {
	BootstrapSnapshot
	NewBareRuntime() (Runtime, error)
}

// RuntimePool creates independent realm runtimes while allowing an engine to
// share immutable/native isolate infrastructure between them. Each returned
// Runtime remains independently closeable.
type RuntimePool interface {
	NewRuntime() (Runtime, error)
	Close() error
}

type BareRuntimePool interface {
	RuntimePool
	NewBareRuntime() (Runtime, error)
}

// RuntimePoolSnapshot is implemented by snapshots whose engine can host more
// than one independent realm in a single isolate.
type RuntimePoolSnapshot interface {
	NewRuntimePool(maxRealmsPerIsolate int) RuntimePool
}

// PersistentBootstrapSnapshotFactory imports and identifies portable snapshot
// bytes. The identity must change whenever the engine can no longer consume a
// previously serialized blob.
type PersistentBootstrapSnapshotFactory interface {
	BootstrapSnapshotFactory
	BootstrapSnapshotIdentity() (string, error)
	LoadBootstrapSnapshot([]byte) (BootstrapSnapshot, error)
}

// PersistentBootstrapSnapshot exposes the immutable serialized artifact.
type PersistentBootstrapSnapshot interface {
	BootstrapSnapshot
	BootstrapSnapshotBytes() []byte
}
