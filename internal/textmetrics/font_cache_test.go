package textmetrics

import (
	"fmt"
	fontcontainer "github.com/tdewolff/font"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestLocalFontCacheEvictsWithoutLosingReloadedMetrics(t *testing.T) {
	source, _ := runtime.FuncForPC(reflect.ValueOf(fontcontainer.ToSFNT).Pointer()).FileLine(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "resources", "DejaVuSerif.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e := NewDirectories(nil)
	var first resource
	var ascent float64
	for i := 0; i < 70; i++ {
		path := filepath.Join(dir, fmt.Sprintf("face-%d.ttf", i))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		r := resource{path: path}
		face, err := e.load(r)
		if err != nil {
			t.Fatal(i, err)
		}
		if i == 0 {
			first, ascent = r, face.ascent
		}
		if len(e.faces) > 64 || e.bytes > 64<<20 {
			t.Fatal("unbounded cache")
		}
	}
	if e.faces[first.path+"#0"] != nil {
		t.Fatal("oldest face not evicted")
	}
	face, err := e.load(first)
	if err != nil || face.ascent != ascent {
		t.Fatalf("reload changed metrics: %v %v", face, err)
	}
}

func TestRegisteredFontIsReloadableAndBackingStorageCloses(t *testing.T) {
	source, _ := runtime.FuncForPC(reflect.ValueOf(fontcontainer.ToSFNT).Pointer()).FileLine(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "resources", "DejaVuSerif.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	e := NewDirectories(nil)
	id, err := e.RegisterFont(data)
	if err != nil {
		t.Fatal(err)
	}
	registered := e.resources[id]
	original, err := e.load(registered)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i := 0; i < 70; i++ {
		path := filepath.Join(dir, fmt.Sprintf("evict-%d.ttf", i))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.load(resource{path: path}); err != nil {
			t.Fatal(err)
		}
	}
	if e.faces[registered.path+"#0"] != nil {
		t.Fatal("registered face remained pinned in decoded cache")
	}
	reloaded, err := e.load(registered)
	if err != nil || reloaded.ascent != original.ascent || reloaded.descent != original.descent {
		t.Fatalf("registered reload changed metrics: %#v, %v", reloaded, err)
	}
	spool := e.spoolDir
	if _, err := os.Stat(spool); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatalf("backing storage retained after close: %v", err)
	}
}

func TestTrueTypeHintingLoadsOnlyOnFirstInkObservation(t *testing.T) {
	source, _ := runtime.FuncForPC(reflect.ValueOf(fontcontainer.ToSFNT).Pointer()).FileLine(0)
	path := filepath.Join(filepath.Dir(source), "resources", "DejaVuSerif.ttf")
	e := NewDirectories(nil)
	face, err := e.load(resource{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if face.hintLoaded || face.hintFont != nil {
		t.Fatal("DOM font loading eagerly materialized Canvas hint data")
	}
	glyph, ok := face.face.NominalGlyph('A')
	if !ok || !hintGlyphBounds(face, 19.37, uint32(glyph)) {
		t.Fatal("first Canvas ink observation could not load TrueType hint data")
	}
	if !face.hintLoaded || face.hintFont == nil {
		t.Fatal("Canvas hint data was not retained for later observations")
	}
	if !hintGlyphBounds(face, 19.37, uint32(glyph)) {
		t.Fatal("repeated Canvas ink observation lost its hint data")
	}
}
