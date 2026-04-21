package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/maddsua/proxyd"
	radiusmanager "github.com/maddsua/proxyd/radius/manager"
	rpc_pkg "github.com/maddsua/proxyd/rpc"
	rpc_client "github.com/maddsua/proxyd/rpc/client"
	rpc_manager "github.com/maddsua/proxyd/rpc/manager"
	static_pkg "github.com/maddsua/proxyd/staticconfig"
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

	var manager proxyd.ServiceManager

	switch cfg.Manager.Type {

	case ManagerTypeStatic:

		slog.Info("Load static configuration")

		manager = &static_pkg.Manager{ConfigLocation: configLocation}

	case ManagerTypeRPC:

		opts := cfg.Manager.RPCClientConfig

		endpointURL, _ := url.Parse(opts.EndpointURL)
		if endpointURL == nil || endpointURL.Scheme == "" || endpointURL.Host == "" {
			slog.Error("Invalid RPC endpoint",
				slog.String("url", opts.EndpointURL))
			os.Exit(1)
		}

		client := rpc_client.Client{EndpointURL: endpointURL.String()}
		if client.Token, err = rpc_pkg.ParseInstanceToken(opts.SecretToken); err != nil {
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

		manager = &rpc_manager.Manager{Client: &client}

	case ManagerTypeRadius:

		opts := cfg.Manager.RadiusOptions

		if opts.AuthAddr == "" {
			slog.Error("RADIUS auth server address is not set")
			os.Exit(1)
		} else if opts.Secret == "" {
			slog.Error("RADIUS secret is not set")
			os.Exit(1)
		}

		svclist := cfg.Manager.Services
		if len(svclist) == 0 {
			slog.Error("No proxy services defined")
			os.Exit(1)
		}

		slots := make([]radiusmanager.ProxySlotOptions, len(svclist))
		for idx, entry := range svclist {
			slots[idx] = radiusmanager.ProxySlotOptions{
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

		manager = &radiusmanager.Manager{Opts: opts, Slots: slots}

	default:
		slog.Error("Service manager not configured")
		os.Exit(1)
	}

	errCh := make(chan error, 1)

	go func() {
		slog.Debug("Starting manager")
		errCh <- manager.Exec()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("Proxy manager",
				slog.String("err", err.Error()))
		}
	case <-exitCh:
	}

	slog.Warn("Proxy manager exiting")

	exitctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := manager.Shutdown(exitctx); err != nil {
		slog.Error("Proxy manager",
			slog.String("err", err.Error()))
	}
}
