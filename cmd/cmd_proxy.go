package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/local"
	radius "github.com/maddsua/proxyd/radius/manager"
	"github.com/maddsua/proxyd/rpc"
	rpc_client "github.com/maddsua/proxyd/rpc/client"
	rpc_manager "github.com/maddsua/proxyd/rpc/manager"
	"github.com/maddsua/proxyd/utils"
)

func cmd_proxy(args *utils.ArgList, exitCh <-chan os.Signal) {

	slog.Info("Service starting",
		slog.String("mode", "proxy"))

	lock := utils.NewInstanceLock("proxy")
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
			slog.Info("Usage: proxyd [proxy] [--config <location>]")
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

	var manager proxyd.ServiceManager

	switch cfg.Manager.Type {

	case ManagerTypeLocal:
		slog.Info("Load local configuration")
		manager = &local.Manager{ConfigLocation: configLocation}

	case ManagerTypeRPC:
		manager = initRPCManager(cfg.Manager.RPCClientOptions)

	case ManagerTypeRadius:
		manager = initRADIUSManager(cfg.Manager.RadiusOptions, cfg.Manager.Services)

	default:

		if cfg.LegacyManagerOptions.RemoteURL != "" {
			manager = initRPCManagerWithLegacyConfig(cfg.LegacyManagerOptions)
			slog.Warn("A legacy API/RPC config has been loaded. Please update it to use the new syntax")
			break
		}

		slog.Error("Service manager not configured")
		os.Exit(1)
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- manager.Exec()
	}()

	select {
	case <-exitCh:
		slog.Warn("Service exiting ...")
		break
	case err := <-errCh:
		if err != nil {
			slog.Error("Service terminated",
				slog.String("err", err.Error()))
		}
	}

	exitctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := manager.Shutdown(exitctx); err != nil {
		slog.Error("Proxy manager",
			slog.String("err", err.Error()))
	}
}

func initRADIUSManager(opts radius.RadiusOptions, svcs []local.ProxyServiceConfig) proxyd.ServiceManager {

	if opts.AuthAddr == "" {
		slog.Error("RADIUS auth server address is not set")
		os.Exit(1)
	} else if opts.Secret == "" {
		slog.Error("RADIUS secret is not set")
		os.Exit(1)
	}

	if len(svcs) == 0 {
		slog.Error("No proxy services defined")
		os.Exit(1)
	}

	slots := make([]radius.ProxySlotOptions, len(svcs))
	for idx, entry := range svcs {
		slots[idx] = radius.ProxySlotOptions{
			BindAddr:           entry.BindAddr,
			Service:            entry.Type,
			HttpServiceOptions: entry.HttpServiceOptions,
		}
	}

	slog.Info("Set RADIUS auth",
		slog.String("addr", opts.AuthAddr))

	if opts.AcctAddr != "" {
		slog.Info("Set RADIUS accounting",
			slog.String("addr", opts.AuthAddr))
	} else {
		slog.Info("Set RADIUS accounting",
			slog.String("addr", opts.AuthAddr))
	}

	if opts.DacAddr != "" {
		slog.Info("Set RADIUS DAC",
			slog.String("addr", opts.DacAddr))
	}

	return &radius.Manager{Opts: opts, Slots: slots}
}

func initRPCManager(opts rpc_client.RPCClientOptions) proxyd.ServiceManager {

	endpointURL, err := url.Parse(opts.EndpointURL)
	if err != nil || endpointURL.Scheme == "" || endpointURL.Host == "" {
		slog.Error("Invalid RPC endpoint",
			slog.String("url", opts.EndpointURL))
		os.Exit(1)
	}

	client := rpc_client.Client{EndpointURL: endpointURL.String()}

	if client.Token, err = rpc.ParseInstanceToken(opts.SecretToken); err != nil {
		slog.Error("Invalid RPC token",
			slog.String("err", err.Error()))
		os.Exit(1)
	}

	if err := client.Ready(context.Background()); err != nil {
		slog.Error("RPC check failed",
			slog.String("err", err.Error()))
		os.Exit(1)
	}

	slog.Info("RPC upstream OK",
		slog.String("url", client.EndpointURL))

	return &rpc_manager.Manager{Client: &client}
}

func initRPCManagerWithLegacyConfig(opts LegacyManagerOptions) proxyd.ServiceManager {
	return initRPCManager(rpc_client.RPCClientOptions{
		EndpointURL: opts.RemoteURL,
		SecretToken: opts.SecretToken,
	})
}
