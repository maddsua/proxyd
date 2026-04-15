package socks5

import (
	"context"
	"fmt"
	"net"

	"github.com/maddsua/proxyd"
)

func AuthenticateConnection(ctx context.Context, conn net.Conn, auth proxyd.ProxyAuthenticator) (*proxyd.ProxySession, error) {

	methodReq, err := ReadMethodRequest(conn)
	if err != nil {
		return nil, fmt.Errorf("read method request: %v", err)
	}

	for _, method := range methodReq.Methods {
		switch method {
		case AuthMethodPassword:
			if _, err := NewMethodReply(AuthMethodPassword).Write(conn); err != nil {
				return nil, fmt.Errorf("write method reply: %v", err)
			}
			return AuthenticateConnectionWithPassword(ctx, conn, auth)
		}
	}

	if _, err := NewMethodReply(AuthMethodUnacceptable).Write(conn); err != nil {
		return nil, fmt.Errorf("write method reply: %v", err)
	}

	return nil, fmt.Errorf("no supported auth methods")
}
