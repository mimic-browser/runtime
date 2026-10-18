package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func TestTLSHandshakeDoesNotBlockOtherOrigins(t *testing.T) {
	for _, racing := range []bool{false, true} {
		for _, h2 := range []bool{false, true} {
			t.Run(fmt.Sprintf("racing=%t/h2=%t", racing, h2), func(t *testing.T) {
				fast := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
				fast.EnableHTTP2 = h2
				fast.StartTLS()
				defer fast.Close()
				cold := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
				cold.EnableHTTP2 = h2
				cold.StartTLS()
				defer cold.Close()
				entered, release := make(chan struct{}), make(chan struct{})
				slow := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "slow") }))
				slow.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { close(entered); <-release; return nil, nil }}
				slow.StartTLS()
				defer slow.Close()
				defer close(release)
				options := []tlsclient.HttpClientOption{tlsclient.WithInsecureSkipVerify()}
				if racing {
					options = append(options, tlsclient.WithProtocolRacing())
				} else {
					options = append(options, tlsclient.WithDisableHttp3())
				}
				transport, err := NewTLSClientTransport(profiles.Chrome_152, options...)
				if err != nil {
					t.Fatal(err)
				}
				defer transport.CloseIdleConnections()
				get := func(ctx context.Context, url string) error {
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
					if err != nil {
						return err
					}
					response, err := transport.RoundTrip(req)
					if err != nil {
						return err
					}
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					if err != nil {
						return err
					}
					if string(body) != "ok" {
						return fmt.Errorf("unexpected body %q", body)
					}
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if err := get(ctx, fast.URL); err != nil {
					t.Fatal(err)
				}
				slowDone := make(chan error, 1)
				go func() { slowDone <- get(ctx, slow.URL) }()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("slow TLS handshake did not start")
				}
				// A stalled origin must neither hold a warm connection nor prevent an
				// independent origin from establishing its own connection.
				for _, url := range []string{fast.URL, cold.URL} {
					independent, stop := context.WithTimeout(ctx, time.Second)
					start := time.Now()
					err := get(independent, url)
					stop()
					t.Logf("independent request %s: %s", url, time.Since(start))
					if err != nil {
						t.Errorf("independent request blocked by unrelated TLS handshake: %v", err)
					}
				}
			})
		}
	}
}
