package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPermissionQueryCompletionOrderingMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		// Both retained headful Chrome controls complete conversion failures
		// before ordinary microtasks, and service successes after them.
		value, err := p.Evaluate(context.Background(), `(async () => {
			const events = [];
			const queries = ['geolocation', 'push', 'speaker', 'camera', 'midi'].map(name =>
				navigator.permissions.query({ name }).then(
					status => events.push(name + ':resolved:' + status.state),
					error => events.push(name + ':rejected:' + error.name),
				),
			);
			Promise.resolve().then(() => events.push('ordinary-promise'));
			queueMicrotask(() => events.push('queued-microtask'));
			await Promise.all(queries);
			return JSON.stringify(events);
		})()`)
		want := `["push:rejected:NotSupportedError","speaker:rejected:TypeError","ordinary-promise","queued-microtask","geolocation:resolved:prompt","camera:resolved:prompt","midi:resolved:prompt"]`
		if err != nil || value != want {
			t.Fatalf("permission completion ordering: %v, %v", value, err)
		}
	})
}

func TestPermissionDescriptorsMatchFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	probe, err := os.ReadFile("testdata/permission_descriptor_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/permission_descriptor_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Observation any `json:"observation"`
	}
	if err = json.Unmarshal(fixture, &expected); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(context.Background(), string(probe))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var actual any
		if err = json.Unmarshal(encoded, &actual); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(expected.Observation, actual) {
			t.Fatalf("permission descriptor oracle mismatch: %s", encoded)
		}
	})
}

func TestPermissionStatusAliasesShareEnvironmentChanges(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	ctx := context.Background()
	_, err := p.Evaluate(ctx, `(async()=>{globalThis.permissionEvents=[];globalThis.statuses=await Promise.all([navigator.permissions.query({name:'notifications'}),navigator.permissions.query({name:'push',userVisibleOnly:true}),navigator.permissions.query({name:'clipboard-write',allowWithoutGesture:true})]);statuses.forEach((s,i)=>s.addEventListener('change',()=>permissionEvents.push(i+':'+s.state)))})()`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notifications", "clipboard-read"} {
		if err = p.ctx.SetPermission(originOf(p.URL()), name, "granted"); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := p.Evaluate(ctx, `new Promise(resolve=>setTimeout(()=>resolve(JSON.stringify({names:statuses.map(s=>s.name),states:statuses.map(s=>s.state),events:permissionEvents.sort()})),0))`)
	if err != nil || actual != `{"names":["notifications","notifications","clipboard_read"],"states":["granted","granted","granted"],"events":["0:granted","1:granted","2:granted"]}` {
		t.Fatalf("permission aliases: %v %v", actual, err)
	}
}
