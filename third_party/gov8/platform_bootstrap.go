//go:build (windows || linux) && amd64

package gov8

// CompilePlatformBootstrap compiles trusted embedder bootstrap as non-user
// platform code before execution. Author scripts must use ordinary compilation.
// The code-cache producer and consumer retain the same Script classification.
func (c *Context) CompilePlatformBootstrap(s *Scope, source string, cache *FunctionCodeCache, tc *TryCatch) (*Function, bool, error) {
	return c.compileFunctionAdvanced(s, source, nil, cache, tc, true)
}

// CompilePlatformSeed preserves classic-script global lexical declarations
// across separate trusted snapshot stages. Function compilation would create
// a different closure scope for every stage.
func (c *Context) CompilePlatformSeed(s *Scope, source string, tc *TryCatch) (*Script, error) {
	return c.compileScript(s, source, tc, true)
}
