package layoutblitz

import "testing"

func TestRepeatedControlValueDoesNotDirtyLayout(t *testing.T) {
	owner, err := New(1, 800, 600)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.Element(2, "http://www.w3.org/1999/xhtml", "textarea"); err != nil {
		t.Fatal(err)
	}
	if err := owner.Append(1, 2); err != nil {
		t.Fatal(err)
	}
	initial, err := owner.Resolve(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.ControlValue(2, "same text", true); err != nil {
		t.Fatal(err)
	}
	changed, err := owner.Resolve(0)
	if err != nil {
		t.Fatal(err)
	}
	if changed <= initial {
		t.Fatalf("changed control value did not resolve layout: %d -> %d", initial, changed)
	}
	if err := owner.ControlValue(2, "same text", true); err != nil {
		t.Fatal(err)
	}
	repeated, err := owner.Resolve(0)
	if err != nil {
		t.Fatal(err)
	}
	if repeated != changed {
		t.Fatalf("unchanged control value resolved layout again: %d -> %d", changed, repeated)
	}
}
