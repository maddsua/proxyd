package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/maddsua/proxyd/rpc"
	rpc_handler "github.com/maddsua/proxyd/rpc/handler"
	"github.com/maddsua/proxyd/utils"
)

func cmd_rpc(args *utils.ArgList, exitCh <-chan os.Signal) {

	slog.Info("Service starting",
		slog.String("mode", "rpc"))

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

	cfg, err := utils.LoadGenericFile[GlobalConfiguration](configLocation)
	if err != nil {
		slog.Error("Load config",
			slog.String("err", err.Error()))
		os.Exit(1)
	}

	if debugFlag || cfg.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		slog.Debug("ENABLED")
	}

	configWatcher := utils.NewFileWatcher(configLocation)
	defer configWatcher.Stop()

	rpcHandler := rpc_handler.MethodHandler{}
	rpcHandler.SetOpts(cfg.RPC.HandlerOptions)

	go func() {
		for range configWatcher.C {

			cfg, err := utils.LoadGenericFile[GlobalConfiguration](configLocation)
			if err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}

			rpcHandler.SetOpts(cfg.RPC.HandlerOptions)

			slog.Info("Config updated")
		}
	}()

	mux := http.NewServeMux()
	rpc.HandleWithMux(mux, &rpcHandler)

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
