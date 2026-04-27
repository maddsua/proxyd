package proxyd

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type ProxyAuthenticator interface {
	// Tries to authorize a user using their name and password.
	// MUST always return either a non-nil session or an error.
	// Use CredentialsError to indicate non-logic errors
	AuthenticateWithPassword(ctx context.Context, proxyHost net.Addr, clientIP net.IP, username, password string) (*ProxySession, error)
}

type ProxyCredentialsError struct {
	Username   string
	RetryAfter time.Time
}

func (err *ProxyCredentialsError) Error() string {

	if err.Username == "" {
		return "invalid credentials"
	}

	if !err.RetryAfter.IsZero() {
		return fmt.Sprintf("too many attempts for '%s'; retry in %ds", err.Username, int(time.Until(err.RetryAfter).Seconds()))
	}

	return fmt.Sprintf("invalid password for '%s'", err.Username)
}

type ProxySession struct {
	PeerID      string
	PeerEnabled bool

	Dialer ProxyDialer
	DNS    ProxyDNSResolver

	Attributes ProxySessionAttributes
	Pool       ProxyConnectionPool
}

// Dials an address while checking that a peer is allowed to access the remote and wrapping resulting connection in a (proxyConnection)
func (sess *ProxySession) DialDestinationContext(ctx context.Context, network, address string) (net.Conn, error) {

	dstAddr, err := sess.DNS.ResolveDestination(ctx, sess.Dialer.OutboundAddr.Network(), address)
	if err != nil {
		return nil, err
	}

	ctl, err := sess.Pool.Add()
	if err != nil {
		return nil, err
	}

	conn, err := sess.Dialer.DialContext(ctx, network, dstAddr)
	if err != nil {
		ctl.Close()
		return nil, err
	}

	proxyConn, err := ctl.WithConnection(conn)
	if err != nil {
		ctl.Close()
		conn.Close()
		return nil, err
	}

	return proxyConn, nil
}

func (sess *ProxySession) Reset() {
	sess.Pool.CloseConnections()
	sess.Attributes.Reset()
}

type ProxyAttribute interface {
	EqualAttribute(attr ProxyAttribute) bool
	Destroy()
}

// Wraps the underlying sync map to prevent it from getting cleared without being properly closed
type ProxySessionAttributes struct {
	values map[any]ProxyAttribute
	mtx    sync.Mutex
}

func (attrs *ProxySessionAttributes) WithValue(key any, newAttr ProxyAttribute) (attr ProxyAttribute, loaded bool) {

	attrs.mtx.Lock()
	defer attrs.mtx.Unlock()

	if attrs.values == nil {
		attrs.values = map[any]ProxyAttribute{}
	}

	existing := attrs.values[key]
	if existing != nil {
		if newAttr.EqualAttribute(existing) {
			return existing, true
		}
		existing.Destroy()
	}

	attrs.values[key] = newAttr
	return newAttr, false
}

func (attrs *ProxySessionAttributes) Reset() {

	attrs.mtx.Lock()
	defer attrs.mtx.Unlock()

	if len(attrs.values) == 0 {
		return
	}

	for key, val := range attrs.values {
		val.Destroy()
		delete(attrs.values, key)
	}
}
