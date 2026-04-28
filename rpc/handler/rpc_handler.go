package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/maddsua/proxyd/local"
	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/rpc/model"
)

type HandlerOptions struct {
	Instances []ClientInstanceOptions `json:"instances" yaml:"instances"`
}

type ClientInstanceOptions struct {
	ID     uuid.UUID        `json:"id" yaml:"id"`
	Secret rpc.RawSecretKey `json:"secret" yaml:"secret"`

	local.LocalManagerConfig `yaml:",inline"`
}

type MethodHandler struct {
	opts HandlerOptions
	mtx  sync.Mutex
}

func (mh *MethodHandler) SetOpts(opts HandlerOptions) {
	mh.mtx.Lock()
	defer mh.mtx.Unlock()
	mh.opts = opts
}

func (mh *MethodHandler) OnStatus(ctx context.Context, token *rpc.InstanceToken, params model.InstanceStatus) error {

	instance, err := mh.authorizeInstance(token)
	if err != nil {
		return err
	}

	slog.Info("RPC: Status update",
		slog.String("instance_id", instance.ID.String()),
		slog.String("run_id", params.RunID.String()),
		slog.Int64("uptime", params.Uptime))

	return nil
}

func (mh *MethodHandler) OnTraffic(ctx context.Context, token *rpc.InstanceToken, params model.InstanceTrafficUpdate) error {

	instance, err := mh.authorizeInstance(token)
	if err != nil {
		return err
	}

	slog.Debug("RPC: Traffic update",
		slog.String("instance_id", instance.ID.String()),
		slog.Int("deltas", len(params.Deltas)))

	for _, delta := range params.Deltas {
		slog.Info("RPC: Traffic update",
			slog.String("instance_id", instance.ID.String()),
			slog.String("peer_id", delta.PeerID),
			slog.Int64("delta_rx", int64(delta.RxBytes)),
			slog.Int64("delta_tx", int64(delta.TxBytes)))
	}

	return nil
}

func (mh *MethodHandler) OnProxyTable(ctx context.Context, token *rpc.InstanceToken) (*model.ProxyTable, error) {

	instance, err := mh.authorizeInstance(token)
	if err != nil {
		return nil, err
	}

	slog.Debug("RPC: Proxy table request",
		slog.String("instance_id", instance.ID.String()))

	return &model.ProxyTable{Services: local.ProxyServiceTable(instance.Services)}, nil
}

func (mh *MethodHandler) authorizeInstance(token *rpc.InstanceToken) (*ClientInstanceOptions, error) {

	mh.mtx.Lock()
	defer mh.mtx.Unlock()

	for _, client := range mh.opts.Instances {

		if client.ID == token.ID {

			if !client.Secret.Equal(&token.SecretKey) {
				return nil, &rpc.ProcedureError{
					RPCError: model.RPCError{
						Message: "Invalid secret key",
					},
					Code: http.StatusForbidden,
				}
			}

			return &client, nil
		}
	}

	return nil, &rpc.ProcedureError{
		RPCError: model.RPCError{
			Message: "Instance token doesn't match any of the defined instances",
		},
		Code: http.StatusUnauthorized,
	}
}
