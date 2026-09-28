package dom

import "testing"

func TestLegacySelectorListPreservesLeafMatching(t *testing.T) {
	d, err := Parse("<div id='probe' class='active'></div>")
	if err != nil {
		t.Fatal(err)
	}
	ids := d.FindAllIDs(0, "#probe")
	if len(ids) != 1 {
		t.Fatalf("probe IDs: %v", ids)
	}
	for _, tc := range []struct {
		selector string
		want     bool
	}{
		{"#probe", true},
		{"#missing, #probe", true},
		{" , .active, ", true},
		{"#missing, .other,", false},
		{"", false},
	} {
		if got := d.Matches(ids[0], tc.selector); got != tc.want {
			t.Errorf("Matches(%q) = %v, want %v", tc.selector, got, tc.want)
		}
	}
}
