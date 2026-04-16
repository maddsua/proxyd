package proxytable

import (
	"context"
	"fmt"
	"time"

	"github.com/maddsua/proxyd"
	http_pkg "github.com/maddsua/proxyd/http"
	"github.com/maddsua/proxyd/socks"
)

type serviceSlot struct {
	svc  proxyd.ProxyService
	auth peerAuthenticator
	err  error
}

func (slot *serviceSlot) Shutdown(ctx context.Context) error {

	// forcing a slot to shut down anyway after a 3 second wait period;
	// tbh this shouldn't be necessary
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// terminate all sessions of this slot
	slot.auth.ResetPeers()

	// shutdown slot service
	if svc := slot.svc; svc != nil {
		return slot.svc.Shutdown(ctx)
	}

	return nil
}

type ServiceStartError struct {
	ProxyServiceOptions
	Message string
}

func (err *ServiceStartError) Error() string {
	return err.Message
}

func serviceStartErrorMessage(err error) string {
	if err == nil {
		return "service failed to start"
	}
	return err.Error()
}

func NewSlotService(opts ProxyServiceOptions, auth *peerAuthenticator) (proxyd.ProxyService, error) {
	svc, err := newService(opts, auth)
	if svc == nil {
		return nil, &ServiceStartError{
			ProxyServiceOptions: opts,
			Message:             serviceStartErrorMessage(err),
		}
	}
	return svc, err
}

func newService(opts ProxyServiceOptions, auth *peerAuthenticator) (proxyd.ProxyService, error) {
	switch opts.Service {
	case http_pkg.ServiceType:
		return http_pkg.NewService(opts.BindAddr, auth, opts.HttpServiceOptions)
	case socks.ServiceType:
		return socks.NewService(opts.BindAddr, auth)
	default:
		return nil, fmt.Errorf("unsupported service type '%v'", opts.Service)
	}
}
