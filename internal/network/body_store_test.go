package network

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

func TestBodyStoreCopiesAndOwnerRelease(t *testing.T) {
	store := newBodyStore()
	defer store.close()
	a, err := store.put([]byte("memory"))
	if err != nil {
		t.Fatal(err)
	}
	source := []byte{0, 128, 255, 1, 2, 3}
	b, err := store.put(source)
	if err != nil {
		t.Fatal(err)
	}
	source[0] = 17
	read, err := b.copyBytes()
	if err != nil || read[0] != 0 {
		t.Fatal("caller changed retained bytes")
	}
	read[0] = 79
	body, encoded, err := b.protocolBody()
	if err != nil || !encoded || body != base64.StdEncoding.EncodeToString([]byte{0, 128, 255, 1, 2, 3}) {
		t.Fatal("protocol projection changed storage", body, encoded, err)
	}
	if !b.retain() {
		t.Fatal("second owner rejected")
	}
	b.release()
	if data, err := b.copyBytes(); err != nil || data[0] != 0 {
		t.Fatal("first release invalidated another owner", err)
	}
	b.release()
	if _, err := b.copyBytes(); err == nil || b.data != nil {
		t.Fatal("last release did not release retained bytes", err)
	}
	a.release()
	if len(store.bodies) != 0 || store.resident != 0 {
		t.Fatal("released storage remains accounted")
	}
}

type bodyStoreTransport struct{ body []byte }

func (t bodyStoreTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": {"max-age=60"}}, Body: io.NopCloser(bytes.NewReader(t.body))}, nil
}

type bodyStoreObserver struct{ mutate bool }

func (bodyStoreObserver) Before(context.Context, Request) (Decision, error) { return Decision{}, nil }
func (i bodyStoreObserver) After(_ context.Context, _ Request, res Response) (Response, error) {
	if i.mutate {
		res.Body[0] = '!'
	}
	return res, nil
}

func TestBodyStoreSharesCacheAndHistoryButKeepsAfterMutationIndependent(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(fmt.Sprint(mutate), func(t *testing.T) {
			session := NewSessionState()
			session.responseBodies = newBodyStore()
			defer session.Close()
			loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
			defer loader.CloseResponseBodies()
			loader.SetTransport(bodyStoreTransport{[]byte("original")})
			loader.Use(bodyStoreObserver{mutate})
			u, _ := url.Parse("https://example.test/body")
			request := Request{ID: "request-1", URL: u, Method: http.MethodGet}
			response, err := loader.Load(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			want := "original"
			if mutate {
				want = "!riginal"
			}
			if string(response.Body) != want || response.sharedBody != nil {
				t.Fatal("wrong public response or sharing hint escaped")
			}
			cache := session.cache[u.String()][0].response.sharedBody
			history := loader.completed[request.ID].sharedBody
			if (cache == history) == mutate {
				t.Fatal("cache/history shared the wrong representation")
			}
			response.Body[0] = '?'
			retained, ok := loader.Completed(request.ID)
			if !ok || string(retained.Body) != want {
				t.Fatal("caller mutation changed history")
			}
			retained.Body[0] = '#'
			body, encoded, found, err := loader.CompletedBody(request.ID)
			if err != nil || !found || encoded || body != want {
				t.Fatal("CDP body differs from retained representation", body, err)
			}
			cached, ok := session.GetCached(request, time.Now())
			if !ok || string(cached.Body) != "original" {
				t.Fatal("After/caller modified HTTP cache")
			}
			loader.CloseResponseBodies()
			if _, err := cache.copyBytes(); err != nil {
				t.Fatal("Page history release deleted cache body")
			}
			if mutate && history.data != nil {
				t.Fatal("unshared history body survived release")
			}
			session.ClearCache()
			if stats := session.BodyStorageStats(); stats.Bodies != 0 || stats.ResidentBytes != 0 {
				t.Fatal("owner release leaked storage", stats)
			}
			if cache.data != nil {
				t.Fatal("last cache owner retained bytes")
			}
		})
	}
}

func TestBodyStoreConcurrentReadReleaseAndClose(t *testing.T) {
	store := newBodyStore()
	body, err := store.put(bytes.Repeat([]byte("ab😀"), 1<<13))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		if !body.retain() {
			t.Fatal("retain failed")
		}
		workers.Go(func() {
			defer body.release()
			_, _, _ = body.protocolBody()
		})
	}
	body.release()
	store.close()
	workers.Wait()
	if body.data != nil {
		t.Fatal("concurrent close retained bytes")
	}
	if _, err := store.put([]byte("closed")); err == nil {
		t.Fatal("closed store accepted new data")
	}
}

func TestBodyStoreFailurePreservesDeliveryAndReportsUnavailableHistory(t *testing.T) {
	session := NewSessionState()
	session.responseBodies.close()
	defer session.Close()
	recorder := trace.New()
	recorder.Start()
	loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, recorder)
	defer loader.CloseResponseBodies()
	loader.SetTransport(bodyStoreTransport{[]byte("delivered")})
	u, _ := url.Parse("https://example.test/body")
	response, err := loader.Load(context.Background(), Request{ID: "storage-error", URL: u, Method: http.MethodGet})
	if err != nil || string(response.Body) != "delivered" {
		t.Fatal("optional storage failure broke resource delivery", err)
	}
	if _, _, found, err := loader.CompletedBody("storage-error"); !found || err == nil {
		t.Fatal("CDP did not distinguish unavailable body from absent request", found, err)
	}
	diagnostics := 0
	for _, event := range recorder.Events() {
		if event.Kind == trace.Error && event.Name == "responseBodyStorage" {
			diagnostics++
		}
	}
	if diagnostics != 2 || session.BodyStorageStats().Bodies != 0 {
		t.Fatal("lost storage failure diagnostics or leaked failed allocation", diagnostics)
	}
}

func TestBodyStoreHistoryReleasesReplacedAndEvictedOwners(t *testing.T) {
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	defer loader.session.Close()
	defer loader.CloseResponseBodies()
	for index := range 130 {
		if err := loader.remember(fmt.Sprint(index), Response{Body: []byte{byte(index)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, found := loader.Completed("1"); found {
		t.Fatal("evicted history body survived")
	}
	if err := loader.remember("2", Response{Body: []byte("replacement")}); err != nil {
		t.Fatal(err)
	}
	if stats := loader.session.BodyStorageStats(); stats.Bodies != 128 || len(loader.completedOrder) != 128 {
		t.Fatal("replacement/eviction retained abandoned owner", stats)
	}
	loader.CloseResponseBodies()
	if stats := loader.session.BodyStorageStats(); stats.Bodies != 0 {
		t.Fatal("history close leaked owners", stats)
	}
}

func TestBodyStoreConcurrentPutAndClose(t *testing.T) {
	store := newBodyStore()
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			<-start
			for range 10 {
				body, err := store.put(bytes.Repeat([]byte("file"), 4096))
				if err != nil {
					return
				}
				body.release()
			}
		})
	}
	close(start)
	store.close()
	workers.Wait()
	if len(store.bodies) != 0 || store.resident != 0 {
		t.Fatal("write raced teardown and retained storage")
	}
}
