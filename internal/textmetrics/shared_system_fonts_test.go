package textmetrics

import (
	"os"
	"testing"
)

func TestInstalledFontSharesOnlyImmutableTables(t *testing.T) {
	first, second := New(), New()
	defer first.Close()
	defer second.Close()
	first.scan()
	var selected resource
	for _, candidate := range first.catalog {
		info, err := os.Stat(candidate.path)
		if err == nil && info.Size() < 2<<20 && candidate.index == 0 {
			selected = candidate
			break
		}
	}
	if selected.path == "" {
		t.Skip("no small installed font available")
	}
	a, err := first.load(selected)
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.load(selected)
	if err != nil {
		t.Fatal(err)
	}
	if a.face == b.face || a.shaper == b.shaper || a.face.Font != b.face.Font {
		t.Fatal("Page engines must share parsed font tables but own faces and shapers")
	}
	author := selected
	author.shared = false
	third := New()
	defer third.Close()
	c, err := third.load(author)
	if err != nil {
		t.Fatal(err)
	}
	if c.face.Font == a.face.Font {
		t.Fatal("author font unexpectedly reused installed-font cache")
	}
}
