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
	return slot.svc.Shutdown(ctx)
}

func newService(opts *ProxyServiceOptions, auth *peerAuthenticator) (proxyd.ProxyService, error) {
	switch opts.Service {
	case http_pkg.ServiceType:
		return http_pkg.NewService(opts.BindAddr, auth, opts.HttpServiceOptions)
	case socks.ServiceType:
		return socks.NewService(opts.BindAddr, auth)
	default:
		return nil, fmt.Errorf("unsupported service type '%v'", opts.Service)
	}
}
