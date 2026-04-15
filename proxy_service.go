package proxyd

import (
	"context"
	"net"
)

type ProxyService interface {
	ProxyService() string
	BindAddr() net.Addr
	Shutdown(ctx context.Context) error
}

type ServiceManager interface {
	Exec() error
	Shutdown(ctx context.Context) error
}
