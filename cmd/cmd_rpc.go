package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/rpc/handler"
	rpc_model "github.com/maddsua/proxyd/rpc/model"
	static "github.com/maddsua/proxyd/staticconfig"
	"github.com/maddsua/proxyd/utils"
)

func cmd_rpc(args *utils.ArgList, exitCh <-chan os.Signal) {

	slog.Info("Service starting",
		slog.String("mode", "rpc server"))

	lock := utils.NewInstanceLock("rpc")
	defer lock.Unlock()

	configLocation, err := utils.FindFileLocation("./proxyd.yml", GlobalConfigLocation)
	if err != nil {
		slog.Error("No config files exist")
		os.Exit(1)
	}

	debugFlag := strings.EqualFold(os.Getenv("DEBUG"), "true")

	for {

		arg, ok := args.Next()
		if !ok {
			break
		}

		switch arg {

		case "-c", "--config":
			if configLocation, ok = args.Next(); !ok {
				slog.Error("Config location argument MAY NOT be empty",
					slog.String("cmd_arg", arg))
				os.Exit(1)
			}

		case "-d", "--debug":
			debugFlag = true

		default:
			slog.Error("Unexpected argument",
				slog.String("cmd_arg", arg))
			slog.Info("Usage: proxyd rpc [--config <location>]")
			os.Exit(1)
		}
	}

	cfg, err := utils.LoadConfigLocation[GlobalConfiguration](configLocation)
	if err != nil {
		slog.Error("Load config",
			slog.String("err", err.Error()))
		os.Exit(1)
	}

	if debugFlag || cfg.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		slog.Debug("ENABLED")
	}

	configWatcher, cancelWatcher := utils.WatchFile(configLocation)
	defer cancelWatcher()

	rpcHandler := rpcMethodHandler{}
	rpcHandler.Refresh(cfg.RPC)

	go func() {
		for range configWatcher {

			cfg, err := utils.LoadConfigLocation[GlobalConfiguration](configLocation)
			if err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}

			rpcHandler.Refresh(cfg.RPC)

			slog.Info("Config updated")
		}
	}()

	mux := http.NewServeMux()
	handler.HandleWithMux(mux, &rpcHandler)

	srv := http.Server{
		Addr:    cfg.RPC.ListenAddr,
		Handler: mux,
	}

	if srv.Addr == "" {
		srv.Addr = ":46135"
	}

	errCh := make(chan error)

	go func() {
		slog.Info("RPC server listening",
			slog.String("at", srv.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("RPC server",
				slog.String("err", err.Error()))
		}
	case <-exitCh:
	}

	slog.Warn("RPC server exiting")
}

type rpcMethodHandler struct {
	cfg RPCServerConfiguration
	mtx sync.Mutex
}

func (handler *rpcMethodHandler) Refresh(cfg RPCServerConfiguration) {
	handler.mtx.Lock()
	defer handler.mtx.Unlock()
	handler.cfg = cfg
}

func (handler *rpcMethodHandler) OnStatus(ctx context.Context, token *rpc.InstanceToken, params rpc_model.InstanceStatus) error {

	instance, err := handler.authorizeInstance(token)
	if err != nil {
		return err
	}

	slog.Info("RPC: Status update",
		slog.String("instance_id", instance.ID.String()),
		slog.String("run_id", params.RunID.String()),
		slog.Int64("uptime", params.Uptime))

	return nil
}

func (handler *rpcMethodHandler) OnTraffic(ctx context.Context, token *rpc.InstanceToken, params rpc_model.InstanceTrafficUpdate) error {

	instance, err := handler.authorizeInstance(token)
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

func (handler *rpcMethodHandler) OnProxyTable(ctx context.Context, token *rpc.InstanceToken) (*rpc_model.ProxyTable, error) {

	instance, err := handler.authorizeInstance(token)
	if err != nil {
		return nil, err
	}

	slog.Debug("RPC: Proxy table request",
		slog.String("instance_id", instance.ID.String()))

	return &rpc_model.ProxyTable{Services: static.ProxyServiceTable(instance.Services)}, nil
}

func (handler *rpcMethodHandler) authorizeInstance(token *rpc.InstanceToken) (*RPCClientConfiguration, error) {

	handler.mtx.Lock()
	defer handler.mtx.Unlock()

	for _, client := range handler.cfg.Instances {

		if client.ID == token.ID {

			if !client.Secret.Equal(&token.SecretKey) {
				return nil, &rpc.Error{
					RPCError: rpc_model.RPCError{
						Message: "Invalid secret key",
					},
					Code: http.StatusForbidden,
				}
			}

			return &client, nil
		}
	}

	return nil, &rpc.Error{
		RPCError: rpc_model.RPCError{
			Message: "Instance token doesn't match any of the defined instances",
		},
		Code: http.StatusUnauthorized,
	}
}
