package network

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/pion/transport/v5"
)

var ErrRTCDirectTransport = errors.New("WebRTC direct transport is disabled by the Context proxy; use TURN over TCP or TLS")

// RTCIsolatedNet has no OS network implementation to fall back to. ICE server
// names go unchanged to the proxy dialer; peer names (including mDNS), direct
// checks, STUN and UDP TURN cannot reach the host resolver or socket APIs.
type RTCIsolatedNet struct{}

var _ transport.Net = RTCIsolatedNet{}

func (RTCIsolatedNet) ListenPacket(string, string) (net.PacketConn, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) ListenUDP(string, *net.UDPAddr) (transport.UDPConn, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) ListenTCP(string, *net.TCPAddr) (transport.TCPListener, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) Dial(string, string) (net.Conn, error) { return nil, ErrRTCDirectTransport }
func (RTCIsolatedNet) DialUDP(string, *net.UDPAddr, *net.UDPAddr) (transport.UDPConn, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) DialTCP(string, *net.TCPAddr, *net.TCPAddr) (transport.TCPConn, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) ResolveIPAddr(string, string) (*net.IPAddr, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) ResolveUDPAddr(string, string) (*net.UDPAddr, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) ResolveTCPAddr(string, string) (*net.TCPAddr, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) Interfaces() ([]*transport.Interface, error) { return nil, nil }
func (RTCIsolatedNet) InterfaceByIndex(int) (*transport.Interface, error) {
	return nil, ErrRTCDirectTransport
}
func (RTCIsolatedNet) InterfaceByName(string) (*transport.Interface, error) {
	return nil, ErrRTCDirectTransport
}
func (n RTCIsolatedNet) CreateDialer(*net.Dialer) transport.Dialer { return n }
func (n RTCIsolatedNet) CreateListenConfig(*net.ListenConfig) transport.ListenConfig {
	return rtcIsolatedListen{n}
}

type rtcIsolatedListen struct{ RTCIsolatedNet }

func (rtcIsolatedListen) Listen(context.Context, string, string) (net.Listener, error) {
	return nil, ErrRTCDirectTransport
}
func (rtcIsolatedListen) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, ErrRTCDirectTransport
}

// RTCProxyDialer permits only the configured TURN stream endpoints. Each
// allocation owns a tunnel: proxy rotation affects subsequent connections,
// never the address or packets of an existing allocation. Close cancels pending
// auth/CONNECT/TLS handshakes as well as established tunnels.
type RTCProxyDialer struct {
	ctx           context.Context
	cancel        context.CancelFunc
	proxy         *profileProxyDialer
	turnTLSConfig *tls.Config // nil uses system trust; injected roots in package tests
	mu            sync.Mutex
	closed        bool
	servers       map[string]bool // address -> TURN TLS (independent of proxy TLS)
	connections   map[*rtcTunnelConn]struct{}
}

func NewRTCProxyDialer(parent context.Context, raw string) (*RTCProxyDialer, error) {
	p, err := newProfileProxyDialer(raw, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	return &RTCProxyDialer{ctx: ctx, cancel: cancel, proxy: p, connections: map[*rtcTunnelConn]struct{}{}}, nil
}

func (d *RTCProxyDialer) SetServers(servers map[string]bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = make(map[string]bool, len(servers))
	for address, secure := range servers {
		d.servers[address] = secure
	}
}

func (d *RTCProxyDialer) Dial(network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, ErrRTCDirectTransport
	}
	d.mu.Lock()
	secure, allowed := d.servers[address]
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}
	if !allowed {
		return nil, ErrRTCDirectTransport
	}
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	conn, err := d.proxy.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, proxyRequestError(err)
	}
	// Pion's proxy hook returns a stream for both turn: and turns:. It does
	// not wrap the latter in TLS, so authenticate the TURN server here.
	if secure {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			conn.Close()
			return nil, err
		}
		raw := conn
		stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
		config := &tls.Config{MinVersion: tls.VersionTLS12}
		if d.turnTLSConfig != nil {
			config = d.turnTLSConfig.Clone()
		}
		config.ServerName, config.InsecureSkipVerify = host, false
		tlsConn := tls.Client(conn, config)
		err = tlsConn.HandshakeContext(ctx)
		stop()
		if err != nil {
			conn.Close()
			return nil, errors.New("WebRTC TURN TLS handshake failed")
		}
		conn = tlsConn
	}
	tunnel := &rtcTunnelConn{Conn: conn, owner: d}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		conn.Close()
		return nil, net.ErrClosed
	}
	d.connections[tunnel] = struct{}{}
	return tunnel, nil
}

func (d *RTCProxyDialer) Close() {
	d.cancel()
	d.mu.Lock()
	d.closed = true
	connections := make([]*rtcTunnelConn, 0, len(d.connections))
	for conn := range d.connections {
		connections = append(connections, conn)
	}
	d.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

type rtcTunnelConn struct {
	net.Conn
	owner *RTCProxyDialer
}

// The tunnel has no local interface endpoint in the TURN network. Do not
// publish the OS socket to the proxy as a related ICE address. The allocated
// relay endpoint still comes exclusively from the authenticated TURN response.
func (*rtcTunnelConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero} }
func (c *rtcTunnelConn) Close() error {
	err := c.Conn.Close()
	c.owner.mu.Lock()
	delete(c.owner.connections, c)
	c.owner.mu.Unlock()
	return err
}
