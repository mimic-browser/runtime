package textmetrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFallbackVisitsLaterAuthorFamilyBeforeDefaultSerif(t *testing.T) {
	if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", "seguiemj.ttf")); err != nil {
		t.Skip("requires the Windows reference fonts")
	}
	e := New()
	e.SetFallbackFamilies([]string{"Segoe UI Emoji", "Segoe UI Symbol", "Segoe UI", "Arial"})
	list := "'Segoe Fluent Icons','A Missing Font','Segoe UI Emoji',sans-serif"
	primary, err := e.selectResource(list, 400, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	face, err := e.load(primary)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []rune{'☺', '⁉', 'ℹ', '♀', '♂', '▪'} {
		selected, err := e.fallbackFace(face, []rune{ch}, list, 400, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if selected.resource.family != "segoe ui emoji" {
			t.Errorf("U+%04X chose %q before a later author family", ch, selected.resource.family)
		}
	}
	if _, err := e.selectNamedResource("'A Missing Font'", 400, false, nil); err == nil {
		t.Fatal("missing candidate silently selected the default serif")
	}
	if _, err := e.selectResource("'A Missing Font'", 400, false, nil); err != nil {
		t.Fatalf("complete family list lost its final default: %v", err)
	}
}

func TestFallbackPresentationMatchesChrome152(t *testing.T) {
	if _, err := os.Stat(filepath.Join(os.Getenv("WINDIR"), "Fonts", "seguiemj.ttf")); err != nil {
		t.Skip("requires the Windows reference fonts")
	}
	e := New()
	e.SetFallbackFamilies([]string{"Segoe UI Emoji", "Segoe UI Symbol", "Segoe UI", "Arial"})
	for _, tc := range []struct {
		families string
		cluster  string
		want     string
	}{
		{"'Segoe UI'", "⁉", "times new roman"},
		{"'Segoe UI'", "ℹ", "times new roman"},
		{"'Arial'", "⁉", "times new roman"},
		{"'Segoe Fluent Icons'", "☺", "times new roman"},
		{"'Segoe UI'", "☺\ufe0f", "segoe ui emoji"},
		{"'Segoe UI'", "😀", "segoe ui emoji"},
		{"'Segoe Fluent Icons','Segoe UI Emoji'", "☺", "segoe ui emoji"},
	} {
		primaryResource, err := e.selectResource(tc.families, 400, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		primary, err := e.load(primaryResource)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := e.fallbackFace(primary, []rune(tc.cluster), tc.families, 400, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if selected.resource.family != tc.want {
			t.Errorf("%s %q: selected %q, want %q", tc.families, tc.cluster, selected.resource.family, tc.want)
		}
	}
}
