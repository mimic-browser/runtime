package cdp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/network"
)

func interceptionRequest() network.Request {
	parsed, _ := url.Parse("https://example.test/page/probe")
	return network.Request{ID: "network-request", ContextID: "frame", URL: parsed, Method: "GET", Headers: http.Header{}, Initiator: network.Navigation}
}

func TestInterceptionRequestStageDefaultDoesNotPauseResponse(t *testing.T) {
	var interceptor *ControlInterceptor
	var events []string
	interceptor = NewControlInterceptor(func(method string, payload any) {
		events = append(events, method)
		params := payload.(map[string]any)
		if params["networkId"] != "network-request" {
			t.Errorf("Fetch networkId = %v, must correlate with Network request", params["networkId"])
		}
		if err := interceptor.Resolve(params["requestId"].(string), interceptAnswer{action: "continue"}); err != nil {
			t.Error(err)
		}
	})
	interceptor.Enable(true)
	request := interceptionRequest()
	if _, err := interceptor.Before(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := interceptor.After(context.Background(), request, network.Response{Status: 200}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"Fetch.requestPaused"}) {
		t.Fatalf("default Fetch stages: %v", events)
	}
}

func TestInterceptionPatternsAndExplicitEmptyList(t *testing.T) {
	cases := []struct {
		name     string
		patterns []any
		want     int
	}{
		{"omitted", nil, 1},
		{"empty", []any{}, 0},
		{"resource-filter", []any{map[string]any{"resourceType": "Script"}}, 0},
		{"wildcard", []any{map[string]any{"urlPattern": "*/page/prob?"}}, 1},
		{"escaped-wildcard", []any{map[string]any{"urlPattern": `*/page/prob\*`}}, 0},
		{"literal-regexp", []any{map[string]any{"urlPattern": `https://example.test/page/[probe]`}}, 0},
		{"response-only", []any{map[string]any{"requestStage": "Response"}}, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var interceptor *ControlInterceptor
			count := 0
			interceptor = NewControlInterceptor(func(_ string, payload any) {
				count++
				id := payload.(map[string]any)["requestId"].(string)
				if err := interceptor.Resolve(id, interceptAnswer{action: "continue"}); err != nil {
					t.Error(err)
				}
			})
			if err := interceptor.ConfigureFetch(test.patterns); err != nil {
				t.Fatal(err)
			}
			if _, err := interceptor.Before(context.Background(), interceptionRequest()); err != nil {
				t.Fatal(err)
			}
			if count != test.want {
				t.Fatalf("paused %d requests, want %d", count, test.want)
			}
		})
	}
}

func TestInterceptionResponsePatternsAndPerRequestOverride(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "suppress-response", true: "force-response"}[enabled], func(t *testing.T) {
			var interceptor *ControlInterceptor
			var stages []string
			var firstID string
			interceptor = NewControlInterceptor(func(_ string, payload any) {
				params := payload.(map[string]any)
				id := params["requestId"].(string)
				if firstID == "" {
					firstID = id
				} else if id != firstID {
					t.Errorf("interception identity changed across stages: %q -> %q", firstID, id)
				}
				answer := interceptAnswer{action: "continue"}
				if _, response := params["responseStatusCode"]; response {
					stages = append(stages, "response")
					headers, ok := params["responseHeaders"].([]map[string]string)
					if !ok || len(headers) != 2 || headers[0]["name"] != "Set-Cookie" {
						t.Errorf("Fetch headers must preserve duplicate entries: %#v", params["responseHeaders"])
					}
				} else {
					stages = append(stages, "request")
					answer.interceptResponse = &enabled
				}
				if err := interceptor.Resolve(params["requestId"].(string), answer); err != nil {
					t.Error(err)
				}
			})
			patterns := []any{map[string]any{"requestStage": "Request"}}
			if !enabled {
				patterns = append(patterns, map[string]any{"requestStage": "Response"})
			}
			if err := interceptor.ConfigureFetch(patterns); err != nil {
				t.Fatal(err)
			}
			request := interceptionRequest()
			if _, err := interceptor.Before(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if _, err := interceptor.After(context.Background(), request, network.Response{Status: 200, Headers: http.Header{"Set-Cookie": {"a=1", "b=2"}}}); err != nil {
				t.Fatal(err)
			}
			want := []string{"request"}
			if enabled {
				want = append(want, "response")
			}
			if !reflect.DeepEqual(stages, want) || len(interceptor.responseOverrides) != 0 {
				t.Fatalf("stages %v, want %v; retained overrides %d", stages, want, len(interceptor.responseOverrides))
			}
		})
	}
}

func TestInterceptionResponseBodySharesPausedLoaderBytes(t *testing.T) {
	var interceptor *ControlInterceptor
	var interceptionID string
	interceptor = NewControlInterceptor(func(_ string, payload any) {
		interceptionID = payload.(map[string]any)["requestId"].(string)
		body, err := interceptor.ResponseBody(interceptionID)
		if err != nil || string(body) != "original" {
			t.Errorf("body = %q, error = %v", body, err)
		}
		body[0] = 'X'
		if err := interceptor.Resolve(interceptionID, interceptAnswer{action: "continue"}); err != nil {
			t.Error(err)
		}
	})
	if err := interceptor.ConfigureFetch([]any{map[string]any{"requestStage": "Response"}}); err != nil {
		t.Fatal(err)
	}
	response, err := interceptor.After(context.Background(), interceptionRequest(), network.Response{Status: 200, Body: []byte("original")})
	if err != nil || string(response.Body) != "original" {
		t.Fatalf("readback mutated loader bytes: %q, %v", response.Body, err)
	}
	if _, err := interceptor.ResponseBody(interceptionID); err == nil {
		t.Fatal("response body remained available after continuation")
	}
}

func TestInterceptionDoesNotPauseLocallyDecodedDataResponse(t *testing.T) {
	interceptor := NewControlInterceptor(func(method string, _ any) { t.Errorf("local data URL emitted %s", method) })
	if err := interceptor.ConfigureFetch([]any{map[string]any{"requestStage": "Response"}}); err != nil {
		t.Fatal(err)
	}
	request := interceptionRequest()
	request.URL, _ = url.Parse("data:text/plain,local")
	response, err := interceptor.After(context.Background(), request, network.Response{Status: 200, Body: []byte("local")})
	if err != nil || string(response.Body) != "local" {
		t.Fatalf("response %q, %v", response.Body, err)
	}
}

func TestInterceptionDisableResumesPendingRequestAndResponse(t *testing.T) {
	for _, stage := range []string{"request", "response"} {
		for _, legacy := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "-fetch", true: "-legacy"}[legacy], func(t *testing.T) {
				events := make(chan map[string]any, 1)
				interceptor := NewControlInterceptor(func(_ string, payload any) { events <- payload.(map[string]any) })
				if legacy {
					value := "Request"
					if stage == "response" {
						value = "HeadersReceived"
					}
					if err := interceptor.ConfigureNetwork([]any{map[string]any{"interceptionStage": value}}); err != nil {
						t.Fatal(err)
					}
				} else {
					value := "Request"
					if stage == "response" {
						value = "Response"
					}
					if err := interceptor.ConfigureFetch([]any{map[string]any{"requestStage": value}}); err != nil {
						t.Fatal(err)
					}
				}
				done := make(chan error, 1)
				go func() {
					if stage == "request" {
						decision, err := interceptor.Before(context.Background(), interceptionRequest())
						if decision.Block != nil {
							err = decision.Block
						}
						done <- err
					} else {
						response, err := interceptor.After(context.Background(), interceptionRequest(), network.Response{Status: 201, Body: []byte("unchanged")})
						if response.Status != 201 || string(response.Body) != "unchanged" {
							err = errors.New("disabled interceptor changed response")
						}
						done <- err
					}
				}()
				var event map[string]any
				select {
				case event = <-events:
				case <-time.After(time.Second):
					t.Fatal("request did not pause")
				}
				if legacy {
					interceptor.EnableNetwork(false)
				} else {
					interceptor.Enable(false)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("disable did not resume request")
				}
				key := "requestId"
				if legacy {
					key = "interceptionId"
				}
				if interceptor.Resolve(event[key].(string), interceptAnswer{action: "continue"}) == nil {
					t.Fatal("resolved stale interception after disable")
				}
			})
		}
	}
}

func TestInterceptionCancellationDoesNotRetainPendingRequests(t *testing.T) {
	events := make(chan map[string]any, 1)
	interceptor := NewControlInterceptor(func(_ string, payload any) { events <- payload.(map[string]any) })
	interceptor.Enable(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := interceptor.Before(ctx, interceptionRequest()); done <- err }()
	var event map[string]any
	select {
	case event = <-events:
	case <-time.After(time.Second):
		t.Fatal("request did not pause")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request retained")
	}
	if len(interceptor.pending) != 0 {
		t.Fatal("pending request retained after cancellation")
	}
	if interceptor.Resolve(event["requestId"].(string), interceptAnswer{action: "continue"}) == nil {
		t.Fatal("canceled request can still be resolved")
	}
}

func TestInterceptionDetachResumesRequest(t *testing.T) {
	paused := make(chan struct{}, 1)
	interceptor := NewControlInterceptor(func(_ string, _ any) { paused <- struct{}{} })
	interceptor.Enable(true)
	done := make(chan network.Decision, 1)
	go func() {
		decision, _ := interceptor.Before(context.Background(), interceptionRequest())
		done <- decision
	}()
	select {
	case <-paused:
	case <-time.After(time.Second):
		t.Fatal("request did not pause")
	}
	interceptor.Close()
	select {
	case decision := <-done:
		if decision.Block != nil || decision.Request == nil {
			t.Fatalf("detach blocked request: %+v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("detach did not resume request")
	}
}

func TestInterceptionCanceledResponseOverrideIsReleased(t *testing.T) {
	var interceptor *ControlInterceptor
	interceptor = NewControlInterceptor(func(_ string, payload any) {
		enabled := true
		if err := interceptor.Resolve(payload.(map[string]any)["requestId"].(string), interceptAnswer{action: "continue", interceptResponse: &enabled}); err != nil {
			t.Error(err)
		}
	})
	interceptor.Enable(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := interceptor.Before(ctx, interceptionRequest()); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		interceptor.mu.Lock()
		remaining := len(interceptor.responseOverrides)
		interceptor.mu.Unlock()
		if remaining == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("response override retained canceled request")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestInterceptionBinaryPostDataSurvivesJSON(t *testing.T) {
	for _, fetch := range []bool{false, true} {
		t.Run(fmt.Sprint(fetch), func(t *testing.T) {
			body := []byte{0xce, 0xa9, 0xff, 0, 0x80}
			var interceptor *ControlInterceptor
			interceptor = NewControlInterceptor(func(method string, payload any) {
				raw, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				var params map[string]any
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
				request := params["request"].(map[string]any)
				if request["postData"] != "\u00ce\u00a9\u00ff\x00\u0080" {
					t.Errorf("%s text: %q", method, request["postData"])
				}
				data, err := base64.StdEncoding.DecodeString(request["postDataEntries"].([]any)[0].(map[string]any)["bytes"].(string))
				if err != nil || !bytes.Equal(data, body) {
					t.Errorf("%s bytes: %x %v", method, data, err)
				}
				key := "interceptionId"
				if fetch {
					key = "requestId"
				}
				if err := interceptor.Resolve(params[key].(string), interceptAnswer{action: "continue"}); err != nil {
					t.Error(err)
				}
			})
			if fetch {
				interceptor.Enable(true)
			} else {
				interceptor.EnableNetwork(true)
			}
			request := interceptionRequest()
			request.Method = "POST"
			request.Body = body
			if _, err := interceptor.Before(context.Background(), request); err != nil {
				t.Fatal(err)
			}
		})
	}
}
