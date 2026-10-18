package cdp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moreveal/mimic/internal/profile"
)

// Context generation intentionally selects preferences. Reusing its token must
// preserve those selections in every Page, including after ordinary CDP attach.
func TestPortableProfilePreferencesStayCanonical(t *testing.T) {
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	base := s.Browser.Environment()
	ordinary := wireCall(t, c, 1, "Target.createBrowserContext", map[string]any{})["browserContextId"].(string)
	target := wireCall(t, c, 2, "Target.createTarget", map[string]any{"browserContextId": ordinary, "url": "about:blank"})["targetId"].(string)
	page, _, ok := s.target(target)
	if !ok || !reflect.DeepEqual(page.Environment().Preferences, base.Preferences) {
		t.Fatal("ordinary CDP Context changed baseline preferences")
	}
	wireCall(t, c, 3, "Target.disposeBrowserContext", map[string]any{"browserContextId": ordinary})
	for index, seed := range []string{"preference-a", "preference-b", "preference-c", "preference-d"} {
		raw, _ := json.Marshal(map[string]any{"seed": seed})
		document, descriptor, err := profile.Generate(raw, base)
		if err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 2; repeat++ {
			id := 100 + index*20 + repeat*8
			created := wireCall(t, c, id, "Mimic.createContext", map[string]any{"profile": descriptor.Token()})
			contextID := created["browserContextId"].(string)
			for sibling := 0; sibling < 2; sibling++ {
				target := wireCall(t, c, id+1+sibling*2, "Target.createTarget", map[string]any{"browserContextId": contextID, "url": "about:blank"})["targetId"].(string)
				wireCall(t, c, id+2+sibling*2, "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})
				page, _, ok := s.target(target)
				if !ok || !reflect.DeepEqual(page.Environment().Preferences, document.Preferences) {
					t.Fatal("Page preferences differ from selected profile")
				}
				value, err := page.Evaluate(context.Background(), `JSON.stringify([matchMedia('(prefers-color-scheme: dark)').matches,matchMedia('(prefers-reduced-motion: reduce)').matches])`)
				want, _ := json.Marshal([]bool{document.Preferences.ColorScheme == "dark", document.Preferences.ReducedMotion})
				if err != nil || value != string(want) {
					t.Fatalf("seed=%s repeat=%d sibling=%d: %v %v, want %s", seed, repeat, sibling, value, err, want)
				}
			}
			wireCall(t, c, id+6, "Target.disposeBrowserContext", map[string]any{"browserContextId": contextID})
		}
	}
}
