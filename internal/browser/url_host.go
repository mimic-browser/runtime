package browser

import (
	"net/url"

	"github.com/moreveal/mimic/internal/engine"
	whatwg "github.com/nlnwa/whatwg-url/url"
)

// Constructors, reflected element URLs and worker fetches share the browser
// parser. net/url remains the transport representation, after WHATWG parsing.
func resolveURL(base *url.URL, raw string) (*url.URL, error) {
	u, err := whatwg.ParseRef(base.String(), raw)
	if err != nil {
		return nil, err
	}
	return url.Parse(u.Href(false))
}
func browserURLOrigin(u *whatwg.Url) string {
	switch u.Scheme() {
	case "http", "https", "ws", "wss", "ftp":
		return u.Scheme() + "://" + u.Host()
	case "blob":
		if inner, err := whatwg.Parse(u.Pathname()); err == nil {
			return browserURLOrigin(inner)
		}
	}
	return "null"
}
func installURLHost(host map[string]any, runtime engine.Runtime, baseURL func() *url.URL) {
	function := runtime.Function
	if borrowed, ok := runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		function = borrowed.TransientFunction
	}
	host["urlParts"] = function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		var u *whatwg.Url
		var err error
		if len(a) > 1 && strarg(a, 1) == "" {
			u, err = whatwg.Parse(strarg(a, 0))
		} else {
			base := baseURL().String()
			if len(a) > 1 {
				base = strarg(a, 1)
			}
			u, err = whatwg.ParseRef(base, strarg(a, 0))
		}
		if err != nil {
			return nil, err
		}
		return runtime.Value(map[string]any{"href": u.Href(false), "origin": browserURLOrigin(u), "protocol": u.Protocol(), "username": u.Username(), "password": u.Password(), "host": u.Host(), "hostname": u.Hostname(), "port": u.Port(), "pathname": u.Pathname(), "search": u.Search(), "hash": u.Hash()}), nil
	})
	host["setURLPart"] = function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		u, err := whatwg.Parse(strarg(a, 0))
		if err != nil {
			return nil, err
		}
		value := strarg(a, 2)
		switch strarg(a, 1) {
		case "protocol":
			u.SetProtocol(value)
		case "username":
			u.SetUsername(value)
		case "password":
			u.SetPassword(value)
		case "host":
			u.SetHost(value)
		case "hostname":
			u.SetHostname(value)
		case "port":
			u.SetPort(value)
		case "pathname":
			u.SetPathname(value)
		case "search":
			u.SetSearch(value)
		case "hash":
			u.SetHash(value)
		}
		return runtime.Value(u.Href(false)), nil
	})
}
