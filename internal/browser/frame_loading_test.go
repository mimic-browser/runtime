package browser

import (
	"reflect"
	"sync"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

func TestFrameLoadingPublicationPreservesConcurrentTransitions(t *testing.T) {
	p := &Page{trace: trace.New()}
	frame := &Frame{ID: "frame"}
	entered, release, ended, began := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var observed []string
	unsubscribe := p.trace.Subscribe(func(event trace.Event) {
		if event.Name == "frameStoppedLoading" && event.Data["loaderId"] == "old" {
			close(entered)
			<-release
		}
		mu.Lock()
		observed = append(observed, event.Name+":"+event.Data["loaderId"].(string))
		mu.Unlock()
	})
	defer unsubscribe()
	p.beginFrameLoading(frame, "old")
	go func() { p.endFrameLoading(frame, "old"); close(ended) }()
	<-entered
	go func() { p.beginFrameLoading(frame, "new"); close(began) }()
	<-began
	mu.Lock()
	beforeRelease := append([]string(nil), observed...)
	mu.Unlock()
	close(release)
	<-ended
	if !reflect.DeepEqual(beforeRelease, []string{"frameStartedLoading:old"}) {
		t.Fatalf("successor event overtook pending predecessor stop: %v", beforeRelease)
	}
	want := []string{"frameStartedLoading:old", "frameStoppedLoading:old", "frameStartedLoading:new"}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("published transitions = %v, want %v", observed, want)
	}
	p.endFrameLoading(frame, "old")
	if frame.loadingLoaderID != "new" {
		t.Fatal("stale completion ended successor loading interval")
	}
}
