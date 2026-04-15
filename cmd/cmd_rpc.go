package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/rpc/handler"
	rpc_model "github.com/maddsua/proxyd/rpc/model"
	static "github.com/maddsua/proxyd/staticconfig"
	"github.com/maddsua/proxyd/utils"
)

func cmd_rpc(args *utils.ArgList, exitCh <-chan os.Signal) {

	fmt.Fprintln(os.Stderr, "Starting as an RPC server")

	fmt.Fprintln(os.Stderr, "WARN: RPC mode is meant for debugging other instances")
	fmt.Fprintln(os.Stderr, "WARN: If you just want to spin up some proxies without dynamic control - use static config instead")

	lock := utils.NewInstanceLock("rpc")
	defer lock.Unlock()

	configLocation, err := utils.FindFileLocation("./proxyd.yml", GlobalConfigLocation)
	if err != nil {
		fmt.Fprintln(os.Stderr, "No config files exist")
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
				fmt.Fprintln(os.Stderr, "Config location MAY NOT be empty", arg)
				os.Exit(1)
			}

		case "-d", "--debug":
			debugFlag = true

		default:
			fmt.Fprintln(os.Stderr, "Unexpected argument:", arg)
			fmt.Fprintln(os.Stderr, "Usage: proxyd [proxy] [--config <location>]")
			os.Exit(1)
		}
	}

	cfg, err := utils.LoadConfigLocation[GlobalConfiguration](configLocation)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Load config:", err)
		os.Exit(1)
	}

	if debugFlag || cfg.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		fmt.Fprintln(os.Stderr, "DEBUG ENABLED")
	}

	configWatcher, cancelWatcher := utils.WatchFile(configLocation)
	defer cancelWatcher()

	rpcHandler := rpcMethodHandler{}
	rpcHandler.cfg.Store(cfg.RPC)

	go func() {
		for range configWatcher {

			cfg, err := utils.LoadConfigLocation[GlobalConfiguration](configLocation)
			if err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}

			rpcHandler.cfg.Store(cfg.RPC)

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
	cfg atomic.Value
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

	cfg, ok := handler.cfg.Load().(RPCServerConfiguration)
	if !ok {
		panic(fmt.Errorf("invalid config atomic value of type %T", handler.cfg.Load()))
	}

	for _, client := range cfg.Instances {

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
