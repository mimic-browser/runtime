// Package rtctest provides real local TURN and proxy transports for network
// integration tests. It never contacts external servers.
package rtctest

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/stun/v4"
	"github.com/pion/turn/v5"
)

func Certificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RTC fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"turn.fixture.invalid", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}, pool
}

type Proxy struct {
	URL                   string
	Requests, Auth, Bytes atomic.Int64
	Exit                  atomic.Value // string: source IP for new upstream connections
	mu                    sync.Mutex
	connections           map[net.Conn]struct{}
	listener              net.Listener
	server                *http.Server
	Targets               map[string]string // proxy-side DNS for deliberately unresolvable names
	Failure               string            // immutable: auth, deny or stall
}

func NewProxy(t *testing.T, scheme, failure string, cert tls.Certificate) *Proxy {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{listener: listener, Targets: map[string]string{}, Failure: failure, connections: map[net.Conn]struct{}{}}
	p.Exit.Store("127.0.0.2")
	p.URL = scheme + "://user:pass@" + listener.Addr().String()
	if scheme == "socks5" {
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go p.socks(conn)
			}
		}()
	} else {
		if scheme == "https" {
			p.listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		}
		p.server = &http.Server{Handler: http.HandlerFunc(p.connect)}
		go p.server.Serve(p.listener)
	}
	t.Cleanup(func() {
		p.listener.Close()
		if p.server != nil {
			p.server.Close()
		}
		p.Drop()
	})
	return p
}

func (p *Proxy) track(conn net.Conn) func() {
	p.mu.Lock()
	p.connections[conn] = struct{}{}
	p.mu.Unlock()
	return func() { conn.Close(); p.mu.Lock(); delete(p.connections, conn); p.mu.Unlock() }
}
func (p *Proxy) Drop() {
	p.mu.Lock()
	for conn := range p.connections {
		conn.Close()
	}
	p.mu.Unlock()
}
func (p *Proxy) upstream(address string) (net.Conn, error) {
	if target, ok := p.Targets[address]; ok {
		address = target
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	source := p.Exit.Load().(string)
	if net.ParseIP(host) != nil && net.ParseIP(host).To4() == nil {
		source = "::1"
	}
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: 3 * time.Second}
	return dialer.DialContext(context.Background(), "tcp", address)
}
func (p *Proxy) pipe(client net.Conn, reader io.Reader, upstream net.Conn) {
	defer p.track(upstream)()
	done := make(chan struct{})
	go func() { n, _ := io.Copy(upstream, reader); p.Bytes.Add(n); upstream.Close(); close(done) }()
	n, _ := io.Copy(client, upstream)
	p.Bytes.Add(n)
	client.Close()
	<-done
}
func (p *Proxy) connect(w http.ResponseWriter, r *http.Request) {
	p.Requests.Add(1)
	if r.Method != "CONNECT" {
		w.WriteHeader(405)
		return
	}
	if p.Failure == "auth" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")) {
		w.WriteHeader(407)
		return
	}
	p.Auth.Add(1)
	if p.Failure == "deny" {
		w.WriteHeader(403)
		return
	}
	if p.Failure == "stall" {
		<-r.Context().Done()
		return
	}
	upstream, err := p.upstream(r.Host)
	if err != nil {
		w.WriteHeader(502)
		return
	}
	conn, buffer, err := w.(http.Hijacker).Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer p.track(conn)()
	fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n")
	buffer.Flush()
	p.pipe(conn, buffer, upstream)
}
func (p *Proxy) socks(conn net.Conn) {
	defer p.track(conn)()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(r, b); return b, err }
	h, err := read(2)
	if err != nil || h[0] != 5 {
		return
	}
	if _, err := read(int(h[1])); err != nil {
		return
	}
	conn.Write([]byte{5, 2})
	h, err = read(2)
	if err != nil {
		return
	}
	user, err := read(int(h[1]))
	if err != nil {
		return
	}
	h, err = read(1)
	if err != nil {
		return
	}
	pass, err := read(int(h[0]))
	if err != nil {
		return
	}
	if p.Failure == "auth" || string(user) != "user" || string(pass) != "pass" {
		conn.Write([]byte{1, 1})
		return
	}
	p.Auth.Add(1)
	conn.Write([]byte{1, 0})
	h, err = read(4)
	if err != nil {
		return
	}
	p.Requests.Add(1)
	if h[1] != 1 {
		conn.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	var host string
	switch h[3] {
	case 1:
		b, e := read(4)
		if e != nil {
			return
		}
		host = net.IP(b).String()
	case 4:
		b, e := read(16)
		if e != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		b, e := read(1)
		if e != nil {
			return
		}
		b, e = read(int(b[0]))
		if e != nil {
			return
		}
		host = string(b)
	default:
		return
	}
	port, err := read(2)
	if err != nil {
		return
	}
	if p.Failure == "stall" {
		read(1)
		return
	}
	if p.Failure == "deny" {
		conn.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	upstream, err := p.upstream(net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))))
	if err != nil {
		conn.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 2, 0, 0})
	conn.SetDeadline(time.Time{})
	p.pipe(conn, r, upstream)
}

type TURN struct {
	Address                                                      string
	Direct, Accepted, Allocations, Active, ReadBytes, WriteBytes atomic.Int64
	RelayIP                                                      atomic.Value
	server                                                       *turn.Server
}

func NewTURN(t *testing.T, ipv6, secure bool, cert tls.Certificate) *TURN {
	t.Helper()
	address, network, relay := "127.0.0.1:0", "tcp4", "127.0.0.1"
	if ipv6 {
		address, network, relay = "[::1]:0", "tcp6", "::1"
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	f := &TURN{Address: listener.Addr().String()}
	f.RelayIP.Store(relay)
	observed := net.Listener(&turnListener{Listener: listener, fixture: f})
	if secure {
		observed = tls.NewListener(observed, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	f.server, err = turn.NewServer(turn.ServerConfig{Realm: "fixture", AuthHandler: func(request *turn.RequestAttributes) (string, []byte, bool) {
		return request.Username, turn.GenerateAuthKey("rtc", request.Realm, "secret"), request.Username == "rtc"
	}, ListenerConfigs: []turn.ListenerConfig{{Listener: observed, RelayAddressGenerator: &relayGenerator{fixture: f}}}})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { f.server.Close() })
	return f
}

func NewUDPTURN(t *testing.T) *TURN {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &TURN{Address: conn.LocalAddr().String()}
	f.RelayIP.Store("127.0.0.1")
	f.server, err = turn.NewServer(turn.ServerConfig{Realm: "fixture", AuthHandler: func(request *turn.RequestAttributes) (string, []byte, bool) {
		return request.Username, turn.GenerateAuthKey("rtc", request.Realm, "secret"), request.Username == "rtc"
	}, PacketConnConfigs: []turn.PacketConnConfig{{PacketConn: conn, RelayAddressGenerator: &relayGenerator{fixture: f}}}})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { f.server.Close() })
	return f
}

type turnListener struct {
	net.Listener
	fixture *TURN
}

func (l *turnListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.fixture.Accepted.Add(1)
		ip := conn.RemoteAddr().(*net.TCPAddr).IP.String()
		if ip != "127.0.0.2" && ip != "127.0.0.3" && ip != "::1" {
			l.fixture.Direct.Add(1)
		}
	}
	return conn, err
}

type relayGenerator struct{ fixture *TURN }

func (*relayGenerator) Validate() error { return nil }
func (g *relayGenerator) AllocatePacketConn(c turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	conn, err := net.ListenPacket(c.Network, net.JoinHostPort(g.fixture.RelayIP.Load().(string), "0"))
	if err != nil {
		return nil, nil, err
	}
	g.fixture.Allocations.Add(1)
	g.fixture.Active.Add(1)
	return &relayPacketConn{PacketConn: conn, fixture: g.fixture}, conn.LocalAddr(), nil
}
func (*relayGenerator) AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, fmt.Errorf("fixture has UDP relay endpoints only")
}
func (*relayGenerator) AllocateConn(turn.AllocateConnConfig) (net.Conn, error) {
	return nil, fmt.Errorf("fixture has UDP relay endpoints only")
}

type relayPacketConn struct {
	net.PacketConn
	fixture *TURN
	closed  atomic.Bool
}

func (c *relayPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, a, err := c.PacketConn.ReadFrom(b)
	c.fixture.ReadBytes.Add(int64(n))
	return n, a, err
}
func (c *relayPacketConn) WriteTo(b []byte, a net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(b, a)
	c.fixture.WriteBytes.Add(int64(n))
	return n, err
}
func (c *relayPacketConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.fixture.Active.Add(-1)
	}
	return c.PacketConn.Close()
}

// Sink observes forbidden UDP egress independently of browser-visible results.
type Sink struct {
	Address string
	Packets atomic.Int64
	conn    net.PacketConn
}

func NewSink(t *testing.T, ipv6 bool) *Sink {
	return newSink(t, ipv6, false)
}

func NewSTUN(t *testing.T, ipv6 bool) *Sink { return newSink(t, ipv6, true) }

func newSink(t *testing.T, ipv6, respond bool) *Sink {
	t.Helper()
	address, network := "127.0.0.1:0", "udp4"
	if ipv6 {
		address, network = "[::1]:0", "udp6"
	}
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		t.Fatal(err)
	}
	s := &Sink{Address: conn.LocalAddr().String(), conn: conn}
	go func() {
		b := make([]byte, 65536)
		for {
			n, source, err := conn.ReadFrom(b)
			if err != nil {
				return
			}
			s.Packets.Add(1)
			if respond {
				request := &stun.Message{Raw: append([]byte(nil), b[:n]...)}
				if request.Decode() != nil || request.Type != stun.BindingRequest {
					continue
				}
				address := source.(*net.UDPAddr)
				response := stun.MustBuild(stun.NewTransactionIDSetter(request.TransactionID), stun.BindingSuccess, &stun.XORMappedAddress{IP: address.IP, Port: address.Port})
				conn.WriteTo(response.Raw, source)
			}
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return s
}

func Wait(t *testing.T, description string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", description)
}
