package proxyd

import (
	"context"
	"net"
)

type ProxyService interface {
	ProxyService() string
	BindAddr() net.Addr
	Options() ProxyServiceOptions
	Shutdown(ctx context.Context) error
}

type ProxyServiceOptions interface {
	ProxyService() string
	String() string
}

type ServiceManager interface {
	Exec() error
	Shutdown(ctx context.Context) error
}
