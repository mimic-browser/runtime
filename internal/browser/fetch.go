package browser

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
)

// Windows and workers project the same Fetch request/response contract onto
// the shared loader; the owning agent supplies URL resolution and task lifetime.
func fetchRequest(contextID string, target, source *url.URL, args []engine.Value) network.Request {
	request := network.Request{ContextID: contextID, URL: target, Referrer: source, SourceURL: source, Method: strarg(args, 1), Headers: headerMap(arg(args, 2)), Body: byteSlice(arg(args, 3)), Initiator: network.Fetch, Credentials: "same-origin", Mechanism: "api"}
	if options, ok := arg(args, 5).(map[string]any); ok {
		request.Mode, _ = options["mode"].(string)
		request.Credentials, _ = options["credentials"].(string)
		request.Redirect, _ = options["redirect"].(string)
		request.Kind, _ = options["resourceKind"].(string)
	}
	return request
}

func fetchResponse(response network.Response) map[string]any {
	typeName := response.Type
	if typeName == "" {
		typeName = "basic"
	}
	// Do not expose http.Header methods as JavaScript properties in Goja.
	headers := make(map[string]string, len(response.Headers))
	for name, values := range response.Headers {
		headers[name] = strings.Join(values, ", ")
	}
	return map[string]any{"status": response.Status, "statusText": http.StatusText(response.Status), "url": urlString(response.URL), "headers": headers, "bodyBytes": engine.BinaryBuffer(response.Body), "bodyUnavailable": response.Partial, "type": typeName, "redirected": response.Redirected}
}
