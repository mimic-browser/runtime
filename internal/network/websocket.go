package network

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	ws "github.com/bogdanfinn/websocket"
)

type WebSocketConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	WriteControl(int, []byte, time.Time) error
	Subprotocol() string
	Close() error
}

func websocketHeaders(headers http.Header) fhttp.Header {
	out := make(fhttp.Header, len(headers))
	for name, values := range headers {
		out[name] = append([]string(nil), values...)
	}
	return out
}

func (l *Loader) DialWebSocket(ctx context.Context, rawURL string, headers http.Header) (WebSocketConn, error) {
	if l.resourcePolicy != nil {
		if snapshot := l.resourcePolicy.Capture(); snapshot != nil {
			target, err := url.Parse(rawURL)
			if err != nil {
				return nil, err
			}
			decision := snapshot.decide(Request{URL: target, Kind: "websocket"})
			l.resourcePolicy.recordDecision(decision, false)
			if decision.Work.Network != nil && !*decision.Work.Network {
				l.resourcePolicy.recordBlocked(decision.ReportOnly)
				if !decision.ReportOnly {
					return nil, fmt.Errorf("net::ERR_BLOCKED_BY_CLIENT: resource policy rule %q", decision.RuleID)
				}
			}
			release, err := l.resourcePolicy.beginAcquisition(snapshot)
			if err != nil {
				return nil, err
			}
			defer release()
		}
	}
	if target, err := url.Parse(rawURL); err == nil {
		cookies := l.cookies.ForURL(target)
		if len(cookies) > 0 {
			request := &http.Request{Header: headers}
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			headers.Set("Cookie", strings.Join(request.Header.Values("Cookie"), "; "))
		}
	}
	if guard, ok := l.transport.(interface{ BeforeWebSocket() error }); ok {
		if err := guard.BeforeWebSocket(); err != nil {
			return nil, err
		}
	}
	transportBase := l.transport
	if wrapper, ok := transportBase.(interface{ WebSocketBase() Transport }); ok {
		transportBase = wrapper.WebSocketBase()
	}
	if transport, ok := transportBase.(*TLSClientTransport); ok {
		return transport.dialWebSocket(ctx, rawURL, websocketHeaders(headers))
	}
	dialer := *ws.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, rawURL, websocketHeaders(headers))
	return conn, err
}

func (t *TLSClientTransport) dialWebSocket(ctx context.Context, rawURL string, headers fhttp.Header) (WebSocketConn, error) {
	options := append([]tlsclient.HttpClientOption(nil), t.clientOptions...)
	options = append(options, tlsclient.WithDisableProtocolRacing(), tlsclient.WithForceHttp1())
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	socket, err := tlsclient.NewWebsocket(tlsclient.NewNoopLogger(), tlsclient.WithTlsClient(client), tlsclient.WithUrl(rawURL), tlsclient.WithHeaders(headers), tlsclient.WithHandshakeTimeoutMilliseconds(30000))
	if err != nil {
		return nil, err
	}
	return socket.Connect(ctx)
}
