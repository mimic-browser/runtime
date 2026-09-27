package csp

import (
	"net/url"
	"testing"
)

func TestFormActionHasNoDefaultSourceFallback(t *testing.T) {
	doc, _ := url.Parse("https://example.test/index")
	other, _ := url.Parse("https://other.test/submit")
	if !Parse("default-src 'none'").AllowsFormAction(doc, other) {
		t.Fatal("default-src incorrectly blocked form navigation")
	}
	if Parse("form-action 'self'").AllowsFormAction(doc, other) {
		t.Fatal("cross-origin form action accepted")
	}
	if !Parse("form-action 'self'").AllowsFormAction(doc, doc) {
		t.Fatal("same-origin form action rejected")
	}
	if append(Parse("form-action *"), Parse("form-action 'none'")...).AllowsFormAction(doc, doc) {
		t.Fatal("multiple policies did not intersect")
	}
}

func TestScriptPolicy(t *testing.T) {
	doc, _ := url.Parse("https://example.test/index")
	self, _ := url.Parse("https://example.test/app.js")
	other, _ := url.Parse("https://cdn.test/app.js")
	p := Parse("default-src 'none'; script-src 'self' 'nonce-good'")
	if ok, _ := p.AllowsScript(doc, nil, true, false, "good"); !ok {
		t.Fatal("matching inline nonce rejected")
	}
	if ok, _ := p.AllowsScript(doc, nil, true, false, ""); ok {
		t.Fatal("inline script without nonce allowed")
	}
	if ok, _ := p.AllowsScript(doc, self, false, false, ""); !ok {
		t.Fatal("self resource rejected")
	}
	if ok, _ := p.AllowsScript(doc, other, false, false, ""); ok {
		t.Fatal("cross-origin resource allowed")
	}
}

func TestConnectionPolicy(t *testing.T) {
	doc, _ := url.Parse("https://example.test/index")
	other, _ := url.Parse("https://other.test/data")
	cases := []struct {
		policy PolicySet
		target *url.URL
		want   bool
	}{
		{Parse("default-src 'none'"), doc, false},
		{Parse("default-src 'none'; connect-src 'self'"), doc, true},
		{Parse("connect-src 'self'"), other, false},
		{Parse("script-src 'none'"), other, true},
		{Parse("connect-src"), doc, false},
		{ParseReportOnly("connect-src 'none'"), doc, true},
		{append(Parse("connect-src *"), Parse("connect-src 'none'")...), doc, false},
	}
	for i, c := range cases {
		if got := c.policy.AllowsConnection(doc, c.target); got != c.want {
			t.Fatalf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestConnectionPolicyWebSocketSources(t *testing.T) {
	doc, _ := url.Parse("http://example.test:8080/page")
	socket, _ := url.Parse("ws://example.test:8080/socket")
	for _, source := range []string{"*", "'self'", "ws:"} {
		if !Parse("connect-src "+source).AllowsConnection(doc, socket) {
			t.Fatalf("rejected websocket source %s", source)
		}
	}
	if Parse("default-src 'none'").AllowsConnection(doc, socket) {
		t.Fatal("blocked websocket allowed")
	}
}
