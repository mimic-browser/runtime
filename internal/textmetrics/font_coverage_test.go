package textmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	fontcontainer "github.com/tdewolff/font"
)

func TestCatalogCoverageSharedAcrossConcurrentEngines(t *testing.T) {
	source, _ := runtime.FuncForPC(reflect.ValueOf(fontcontainer.ToSFNT).Pointer()).FileLine(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "resources", "DejaVuSerif.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "reference.ttf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	first, second := NewDirectories([]string{dir}), NewDirectories([]string{dir})
	first.scan()
	second.scan()
	key := fmt.Sprintf("%s#0", path)
	if first.coverage[key] == nil || second.coverage[key] == nil {
		t.Fatal("catalog omitted font coverage")
	}
	// Concurrent first reads share the same immutable decoded resource. Each
	// Engine's selected faces and shaping state remain independent.
	var reads sync.WaitGroup
	for i := 0; i < 16; i++ {
		reads.Go(func() {
			for _, engine := range []*Engine{first, second} {
				cmap := engine.coverage[key]()
				if cmap == nil || !nominalCoverage(cmap, []rune("ABC")) || nominalCoverage(cmap, []rune("\u0378")) {
					t.Error("concurrent catalog coverage changed")
				}
			}
		})
	}
	reads.Wait()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if cmap := second.coverage[key](); cmap == nil || !nominalCoverage(cmap, []rune("ABC")) {
		t.Fatal("completed catalog coverage was decoded again")
	}
	if len(first.faces) != 0 || len(second.faces) != 0 {
		t.Fatal("nominal coverage decoded a Page-owned shaping face")
	}
}
