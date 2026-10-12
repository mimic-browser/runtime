package gojaengine

import (
	"context"
	"crypto/sha256"
	"sync"

	"github.com/dop251/goja"
	"github.com/moreveal/mimic/internal/engine"
)

// Goja Programs are immutable, runtime-independent code and support concurrent
// execution. The cache owns no runtime, JavaScript object, host or Page state.
// Page scripts use Eval; an internal-looking script name cannot enter this cache.
const bootstrapProgramLimit = 8

type bootstrapKey struct {
	name   string
	digest [32]byte
}

var bootstrapPrograms = struct {
	sync.Mutex
	entries map[bootstrapKey]func() (*goja.Program, error)
}{entries: make(map[bootstrapKey]func() (*goja.Program, error))}

func compileBootstrap(source, name string) (*goja.Program, error) {
	key := bootstrapKey{name: name, digest: sha256.Sum256([]byte(source))}
	bootstrapPrograms.Lock()
	compile := bootstrapPrograms.entries[key]
	if compile == nil {
		compile = sync.OnceValues(func() (*goja.Program, error) { return goja.Compile(name, "(function(){"+source+"\n})()", false) })
		if len(bootstrapPrograms.entries) < bootstrapProgramLimit {
			bootstrapPrograms.entries[key] = compile
		}
	}
	bootstrapPrograms.Unlock()
	// Compilation and execution never hold the cache lock.
	return compile()
}

func (r *runtime) EvalBootstrap(ctx context.Context, source, name string) (engine.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	program, err := compileBootstrap(source, name)
	if err != nil {
		return nil, r.jsError(err)
	}
	return r.run(ctx, func() (goja.Value, error) { return r.vm.RunProgram(program) })
}
