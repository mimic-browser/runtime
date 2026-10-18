package browser

import (
	"context"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestXMLNamespaceDeclarationsPrecedeOrdinaryAttributes(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const source='<svg version="1.1" id="Layer_1" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" x="0px" y="0px"></svg>';
 const xml=new DOMParser().parseFromString(source,'image/svg+xml').documentElement;
 const html=document.createElement('div');html.innerHTML=source;
 return JSON.stringify([xml.getAttributeNames(),Array.from(xml.attributes,a=>a.name),html.firstElementChild.getAttributeNames()]);
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[["xmlns","xmlns:xlink","version","id","x","y"],["xmlns","xmlns:xlink","version","id","x","y"],["version","id","xmlns","xmlns:xlink","x","y"]]`
	if value != want {
		t.Fatalf("SVG attribute order differs from Chrome 152: %v", value)
	}
}
