// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"testing"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// center returns a point safely inside r's interior (not on its boundary,
// where rectContains' half-open comparison could disagree with a naive
// caller's expectation).
func center(r layout.Rect) image.Point {
	return image.Pt(int(r.X+r.W/2), int(r.Y+r.H/2))
}

func TestElementAtResolvesMostSpecificNestedElement(t *testing.T) {
	live := openFixture(t, New(), `<html><body>
		<div id="outer" style="width:200px;height:200px;background:#eee">
			<button id="btn" style="width:40px;height:20px">Go</button>
		</div>
	</body></html>`, image.Rect(0, 0, 300, 300))

	elems := live.Elements()
	if len(elems) == 0 {
		t.Fatal("Elements() returned an empty index")
	}
	outer, btn := findByID(live.Document().Root, "outer"), findByID(live.Document().Root, "btn")
	outerRect, ok := elems[outer]
	if !ok {
		t.Fatal("outer div missing from the element index")
	}
	btnRect, ok := elems[btn]
	if !ok {
		t.Fatal("button missing from the element index")
	}

	// Inside the button: must resolve to the button itself, not outer/body/
	// html even though all of their rects also contain this point.
	n, ok := live.ElementAt(center(btnRect))
	if !ok {
		t.Fatal("ElementAt inside the button: want a hit")
	}
	if n.Type != dom.Element || n.Tag != "button" || n.Attr["id"] != "btn" {
		t.Fatalf("ElementAt inside the button resolved to <%s id=%q>, want the button", n.Tag, n.Attr["id"])
	}

	// Inside outer but well below the button: must resolve to outer, not the
	// button (whose rect does not extend there) and not a larger ancestor.
	belowBtn := image.Pt(int(outerRect.X+outerRect.W/2), int(btnRect.Y+btnRect.H+20))
	n, ok = live.ElementAt(belowBtn)
	if !ok {
		t.Fatal("ElementAt inside outer, outside the button: want a hit")
	}
	if n.Attr["id"] != "outer" {
		t.Fatalf("resolved to <%s id=%q>, want outer", n.Tag, n.Attr["id"])
	}
}

func TestElementAtMissReportsFalse(t *testing.T) {
	live := openFixture(t, New(), `<html><body>
		<div id="box" style="width:10px;height:10px"></div>
	</body></html>`, image.Rect(0, 0, 300, 300))

	if _, ok := live.ElementAt(image.Pt(290, 290)); ok {
		t.Fatal("ElementAt far outside any element: want a miss")
	}
}

func TestElementAtEmptyIndex(t *testing.T) {
	if n, ok := ElementAt(nil, image.Pt(0, 0)); ok || n != nil {
		t.Fatalf("ElementAt(nil, ...) = (%v, %v), want (nil, false)", n, ok)
	}
}

// TestElementsReflectsInteraction proves Elements()/ElementAt() see the
// CURRENT layout, not a snapshot from Open — a resized element must move the
// hit-test result on the very next call, matching how a real click after a
// synthetic mutation must land on where things NOW are.
func TestElementsReflectsInteraction(t *testing.T) {
	live := openFixture(t, New(), `<html><body>
		<div id="box" style="width:10px;height:10px"></div>
	</body></html>`, image.Rect(0, 0, 300, 300))

	if _, ok := live.ElementAt(image.Pt(50, 50)); ok {
		t.Fatal("before resize: (50,50) should miss the 10x10 box")
	}

	box := dom.Find(live.Document().Root, "div")
	_, _, err := live.Interact(context.Background(), func() {
		box.Attr["style"] = "width:100px;height:100px"
	})
	if err != nil {
		t.Fatalf("Interact: %v", err)
	}

	n, ok := live.ElementAt(image.Pt(50, 50))
	if !ok || n.Attr["id"] != "box" {
		t.Fatalf("after resize: ElementAt(50,50) = (%v,%v), want the resized box", n, ok)
	}
}
