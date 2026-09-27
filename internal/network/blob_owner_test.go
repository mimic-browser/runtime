package network

import "testing"

type testMediaSourceResource struct{ id string }

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
