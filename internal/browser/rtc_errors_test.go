package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

//go:embed testdata/rtc_error_classes.js
var rtcErrorClasses string

func TestRTCErrorClassesMatchChrome152(t *testing.T) {
	serialBrowserTest(t)
	// Independently measured in normal headful Chrome 152, with the recorder
	// detached before the author operations. These invalid operations use no
	// network route and must preserve their class through the Promise boundary.
	want := map[string]string{
		"answerWithoutOffer":      "InvalidStateError",
		"iceWithoutRemote":        "InvalidStateError",
		"localAnswerWithoutOffer": "InvalidModificationError",
		"malformedRemote":         "OperationError",
		"offerClosed":             "InvalidStateError",
		"localClosed":             "InvalidStateError",
	}
	cameraTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, rtcErrorClasses)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		raw, ok := value.(string)
		if !ok || json.Unmarshal([]byte(raw), &got) != nil {
			t.Fatalf("invalid error result %v", value)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("error classes: got %v; want %v", got, want)
		}
	})
}
