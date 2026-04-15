package main

import (
	"context"
	"fmt"
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

	fmt.Fprintln(os.Stderr, "Starting as a proxy service")

	lock := utils.NewInstanceLock("proxy")
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

	var manager proxyd.ServiceManager

	switch cfg.Manager.Type {

	case ManagerTypeStatic:
		manager = &static_pkg.Manager{ConfigLocation: configLocation}
		fmt.Fprintln(os.Stderr, "Using STATIC manager")

	case ManagerTypeRPC:

		opts := cfg.Manager.RPCClientConfig

		endpointURL, _ := url.Parse(opts.EndpointURL)
		if endpointURL == nil || endpointURL.Scheme == "" || endpointURL.Host == "" {
			fmt.Fprintln(os.Stderr, "Invalid RPC endpoint url:", opts.EndpointURL)
			os.Exit(1)
		}

		client := rpc_client.Client{EndpointURL: endpointURL.String()}
		if client.Token, err = rpc_pkg.ParseInstanceToken(opts.SecretToken); err != nil {
			fmt.Fprintln(os.Stderr, "Invalid RPC token:", err)
			os.Exit(1)
		}

		if err := client.Ready(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "RPC check failed:", err)
			os.Exit(1)
		}

		fmt.Fprintln(os.Stderr, "RPC upstream OK:", client.EndpointURL)

		manager = &rpc_manager.Manager{Client: &client}
		fmt.Fprintln(os.Stderr, "Using RPC manager")

	case ManagerTypeRadius:

		opts := cfg.Manager.RadiusOptions

		if opts.AuthAddr == "" {
			fmt.Fprintln(os.Stderr, "RADIUS auth server address is not set")
			os.Exit(1)
		} else if opts.Secret == "" {
			fmt.Fprintln(os.Stderr, "RADIUS secret is not set")
			os.Exit(1)
		}

		svclist := cfg.Manager.Services
		if len(svclist) == 0 {
			fmt.Fprintln(os.Stderr, "No proxy services defined")
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

		manager = &radiusmanager.Manager{Opts: opts, Slots: slots}
		fmt.Fprintln(os.Stderr, "Using RADIUS manager")

		fmt.Fprintln(os.Stderr, "RADIUS AUTH:", opts.AuthAddr)

		if opts.DacAddr != "" {
			fmt.Fprintln(os.Stderr, "RADIUS DAC:", opts.DacAddr)
		}

	default:
		fmt.Fprintln(os.Stderr, "Service manager not configured")
		os.Exit(1)
	}

	errCh := make(chan error, 2)

	go func() {
		fmt.Fprintln(os.Stderr, "STARTING MANAGER\n--------")
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
