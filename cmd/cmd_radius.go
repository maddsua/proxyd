package main

import (
	"log/slog"
	"os"
	"strings"

	radius "github.com/maddsua/layeh-radius"
	radius_pkg "github.com/maddsua/proxyd/radius"
	"github.com/maddsua/proxyd/radius/handler"
	"github.com/maddsua/proxyd/utils"
)

func cmd_radius(args *utils.ArgList, exitCh <-chan os.Signal) {

	slog.Info("Service starting",
		slog.String("mode", "radius"))

	lock := utils.NewInstanceLock("radius")
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
			slog.Info("Usage: proxyd radius [--config <location>]")
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

	if cfg.Radius.Secret == "" {
		slog.Error("RADIUS secret is not set")
		os.Exit(1)
	}

	handler := &handler.Handler{}

	if cfg.Radius.DacAddr != "" {
		slog.Info("DAC", slog.String("addr", cfg.Radius.DacAddr))
		handler.DAClient = &radius_pkg.Client{DacAddr: cfg.Radius.DacAddr, Secret: cfg.Radius.Secret}
	}

	handler.SetUsers(cfg.Radius.Users)

	configWatcher := utils.NewFileWatcher(configLocation)
	defer configWatcher.Stop()

	go func() {
		for range configWatcher.C {

			cfg, err := utils.LoadGenericFile[GlobalConfiguration](configLocation)
			if err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}

			handler.SetUsers(cfg.Radius.Users)

			slog.Info("User list updated")
		}
	}()

	srv := radius.PacketServer{
		SecretSource: radius.StaticSecretSource([]byte(cfg.Radius.Secret)),
		Addr:         cfg.Radius.ListenAddr,
		Handler:      handler,
		ErrorLog: utils.LegacyLogger{
			Prefix: "RADIUS SRV",
			Level:  slog.LevelError,
		},
	}

	if srv.Addr == "" {
		srv.Addr = ":1812"
	}

	errCh := make(chan error)

	go func() {
		errCh <- srv.ListenAndServe()
	}()

	slog.Info("RADIUS server addrs",
		slog.String("auth", srv.Addr),
		slog.String("acct", srv.Addr))

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("RADIUS server",
				slog.String("err", err.Error()))
		}
	case <-exitCh:
	}

	slog.Warn("RADIUS server exiting")
}
