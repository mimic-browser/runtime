package network

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

type testMediaSourceResource struct{ id string }

type capturedBlobInterceptor struct{ decision Decision }

func (i capturedBlobInterceptor) Before(context.Context, Request) (Decision, error) {
	return i.decision, nil
}

func (i capturedBlobInterceptor) After(_ context.Context, _ Request, response Response) (Response, error) {
	return response, nil
}

func TestCapturedBlobLoadStillHonorsInterception(t *testing.T) {
	raw := "blob:https://example.test/resource"
	target, _ := url.Parse(raw)
	other, _ := url.Parse("blob:https://example.test/unknown")
	blocked := errors.New("blocked by interceptor")
	for name, decision := range map[string]Decision{
		"blocked":   {Block: blocked},
		"rewritten": {Request: &Request{URL: other}},
		"fulfilled": {Response: &Response{Status: 200, Body: []byte("replacement")}},
	} {
		t.Run(name, func(t *testing.T) {
			session := NewSessionState()
			session.PutBlob(raw, []byte("original"), "text/plain", "owner")
			resource := session.CaptureBlob(raw)
			session.RevokeBlob(raw)
			loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
			defer loader.CloseOwnedTransport()
			loader.Use(capturedBlobInterceptor{decision: decision})
			response, err := loader.Load(context.Background(), Request{URL: target, BlobResource: resource, Initiator: Worker})
			if name == "fulfilled" {
				if err != nil || string(response.Body) != "replacement" {
					t.Fatalf("fulfillment ignored: %+v, %v", response, err)
				}
			} else if err == nil {
				t.Fatal("captured blob bypassed interception")
			} else if name == "blocked" && !errors.Is(err, blocked) {
				t.Fatalf("block error lost: %v", err)
			}
		})
	}
}

func TestCapturedBlobLoadSurvivesRevocationWithoutRestoringURL(t *testing.T) {
	for _, ownerRevocation := range []bool{false, true} {
		session := NewSessionState()
		raw := "blob:https://example.test/resource"
		session.PutBlob(raw, []byte("body"), "text/plain", "owner")
		resource := session.CaptureBlob(raw)
		if ownerRevocation {
			session.RevokeBlobsForOwner("owner")
		} else {
			session.RevokeBlob(raw)
		}
		loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
		loader.Policy().SetOffline(true)
		target, _ := url.Parse(raw)
		response, err := loader.Load(context.Background(), Request{URL: target, BlobResource: resource, Initiator: Worker})
		if err != nil || string(response.Body) != "body" || response.Headers.Get("Content-Type") != "text/plain" {
			t.Fatalf("captured resource: %+v, %v", response, err)
		}
		if session.CaptureBlob(raw) != nil {
			t.Fatal("revoked URL available to a new consumer")
		}
		if _, err := loader.Load(context.Background(), Request{URL: target, Initiator: Fetch}); err == nil {
			t.Fatal("new load succeeded after revocation")
		}
		other, _ := url.Parse("blob:https://example.test/other")
		if _, err := loader.Load(context.Background(), Request{URL: other, BlobResource: resource, Initiator: Fetch}); err == nil {
			t.Fatal("captured capability transferred to another URL")
		}
		loader.CloseOwnedTransport()
	}
}

func (s *testMediaSourceResource) MediaSourceID() string { return s.id }

func TestMediaSourceURLIdentityAndOwnerRevocation(t *testing.T) {
	session := NewSessionState()
	source := &testMediaSourceResource{id: "source"}
	childURL := "blob:https://example.test/child-source"
	parentURL := "blob:https://example.test/parent-source"
	session.PutMediaSourceURL(childURL, "child", source)
	session.PutMediaSourceURL(parentURL, "parent", source)
	for _, raw := range []string{childURL, parentURL} {
		if session.MediaSourceURL(raw) != source {
			t.Fatal("object URL duplicated its MediaSource identity")
		}
		if _, _, exists := session.Blob(raw); exists {
			t.Fatal("MediaSource URL exposed a fabricated Blob body")
		}
	}
	session.RevokeBlobsForOwner("child")
	if session.MediaSourceURL(childURL) != nil || session.MediaSourceURL(parentURL) != source {
		t.Fatal("owner revocation changed another owner's source URL")
	}
	session.RevokeBlob(parentURL)
	if session.MediaSourceURL(parentURL) != nil {
		t.Fatal("explicit revocation retained the source URL")
	}
}

func TestBlobOwnerRevocationIsIsolated(t *testing.T) {
	session := NewSessionState()
	session.PutBlob("blob:https://example.test/child", []byte("child"), "text/plain", "child")
	session.PutBlob("blob:https://example.test/parent", []byte("parent"), "text/plain", "parent")
	session.RevokeBlobsForOwner("child")
	if _, _, exists := session.Blob("blob:https://example.test/child"); exists {
		t.Fatal("deactivated owner's URL remains registered")
	}
	body, mime, exists := session.Blob("blob:https://example.test/parent")
	if !exists || string(body) != "parent" || mime != "text/plain" {
		t.Fatal("revocation changed another owner's resource")
	}
	session.RevokeBlobsForOwner("child")
	session.RevokeBlob("blob:https://example.test/parent")
	if _, _, exists := session.Blob("blob:https://example.test/parent"); exists {
		t.Fatal("explicit URL revocation no longer works")
	}
}
