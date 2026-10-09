package chrome152

import (
	"reflect"
	"testing"

	"github.com/moreveal/mimic/chrome/152/generated"
	"github.com/moreveal/mimic/compatibility"
	"github.com/moreveal/mimic/internal/webapi"
)

func TestBootstrapMetadataMatchesFrozenInputs(t *testing.T) {
	surface := (&Bundle{}).Surface()
	for _, name := range []string{"window.insecure.non-isolated", "window.secure.non-isolated", "window.secure.isolated"} {
		exposure, ok := surface.Exposure(name)
		if !ok {
			t.Fatal(name)
		}
		unprepared := &compatibility.WebAPISurface{GeneratedJavaScript: generated.Surface, GeneratedCatalogJSON: generated.SurfaceCatalog, Exposures: map[string]compatibility.RealmExposure{name: exposure}}
		gotSource, gotExposure, gotCatalog := webapi.BootstrapFor(surface, name)
		wantSource, wantExposure, wantCatalog := webapi.BootstrapFor(unprepared, name)
		if gotSource != wantSource || gotExposure != wantExposure || gotCatalog != wantCatalog {
			t.Fatalf("stale bootstrap metadata for %s", name)
		}
	}
	if !reflect.DeepEqual(generated.BootstrapEventAttributes, trustedTypeEventAttributes(generated.SurfaceCatalog)) {
		t.Fatal("stale event attributes")
	}
}
