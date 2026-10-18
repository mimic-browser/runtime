package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestCrossOriginFramePermissionStateUsesEmbeddingPolicy(t *testing.T) {
	parallelBrowserTest(t)
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<script>const report=async(suffix='')=>{const values={};for(const name of ['geolocation','camera','notifications','background-sync','background-fetch','storage-access'])values[name]=(await navigator.permissions.query({name})).state;parent.postMessage({tag:location.hash.slice(1)+suffix,values},'*')};addEventListener('message',e=>{if(e.data==='recheck')report('-recheck')});report()</script>`)
	}))
	defer child.Close()
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/same" {
			fmt.Fprint(w, `<script>(async()=>{const values={};for(const name of ['geolocation','camera','notifications','background-sync','background-fetch','storage-access'])values[name]=(await navigator.permissions.query({name})).state;parent.postMessage({tag:'same',values},'*')})()</script>`)
			return
		}
		fmt.Fprintf(w, `<script>window.permissionReports={};addEventListener('message',e=>permissionReports[e.data.tag]=e.data.values)</script><iframe src="/same"></iframe><iframe src="%s#cross"></iframe><iframe allow="geolocation *" src="%s#delegated"></iframe>`, child.URL, child.URL)
	}))
	defer parent.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	for _, name := range []string{"geolocation", "camera"} {
		if err := c.SetPermission("", name, "granted"); err != nil {
			t.Fatal(err)
		}
	}
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), parent.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const done=()=>{if(Object.keys(permissionReports).length===3)resolve(permissionReports)};addEventListener('message',done);done()})`)
	if err != nil {
		t.Fatal(err)
	}
	reports := value.(map[string]any)
	check := func(tag, name, want string) {
		t.Helper()
		row, ok := reports[tag].(map[string]any)
		if !ok || row[name] != want {
			t.Fatalf("%s %s = %#v, want %s; reports=%#v", tag, name, row[name], want, reports)
		}
	}
	for _, name := range []string{"geolocation", "camera", "background-sync", "background-fetch", "storage-access"} {
		check("same", name, "granted")
	}
	check("same", "notifications", "prompt")
	for _, name := range []string{"geolocation", "camera", "notifications"} {
		check("cross", name, "denied")
	}
	check("cross", "background-sync", "granted")
	check("cross", "background-fetch", "prompt")
	check("cross", "storage-access", "prompt")
	check("delegated", "geolocation", "granted")
	check("delegated", "camera", "denied")
	check("delegated", "notifications", "denied")
	for _, name := range []string{"notifications", "storage-access", "background-fetch"} {
		if err := c.SetPermission(child.URL, name, "granted"); err != nil {
			t.Fatal(err)
		}
	}
	value, err = p.Evaluate(context.Background(), `new Promise(resolve=>{const done=()=>{if(permissionReports['cross-recheck'])resolve(permissionReports['cross-recheck'])};addEventListener('message',done);document.querySelectorAll('iframe')[1].contentWindow.postMessage('recheck','*');done()})`)
	if err != nil {
		t.Fatal(err)
	}
	updated := value.(map[string]any)
	for _, name := range []string{"notifications", "storage-access", "background-fetch"} {
		if updated[name] != "granted" {
			t.Fatalf("explicit child %s = %#v", name, updated[name])
		}
	}
}
