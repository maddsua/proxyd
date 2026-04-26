package main

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	radius "github.com/maddsua/layeh-radius"
	radius_pkg "github.com/maddsua/proxyd/radius"
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

	if cfg.Radius.Secret == "" {
		slog.Error("RADIUS secret is not set")
		os.Exit(1)
	}

	handler := &radiusHandler{}
	handler.RefreshConfig(cfg.Radius)

	configWatcher, cancelWatcher := utils.WatchFile(configLocation)
	defer cancelWatcher()

	go func() {
		for range configWatcher {

			cfg, err := utils.LoadConfigLocation[GlobalConfiguration](configLocation)
			if err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}

			handler.RefreshConfig(cfg.Radius)

			slog.Info("Config updated")
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

type radiusHandler struct {
	peerSet map[string]struct{}
	mtx     sync.Mutex
	cfg     RadiusServerConfiguration
}

func (handler *radiusHandler) ServeRADIUS(wrt radius.ResponseWriter, req *radius.Request) {

	switch req.Code {

	case radius.CodeAccessRequest:
		wrt.Write(handler.HandleAccessRequest(req))

	case radius.CodeAccountingRequest:
		wrt.Write(handler.HandleAccountingRequest(req))

	default:
		slog.Warn("RADIUS server: Unexpected code",
			slog.String("client", req.RemoteAddr.String()),
			slog.String("code", req.Code.String()))
	}
}

func (handler *radiusHandler) HandleAccessRequest(req *radius.Request) *radius.Packet {

	params := radius_pkg.ParsePeerCredentials(req.Packet)

	for _, user := range handler.userList() {

		if user.Username != params.Username || user.Suspended {
			continue
		}

		if subtle.ConstantTimeCompare([]byte(user.Password), []byte(params.Password)) != 1 {

			slog.Info("RADIUS server: Unautorized",
				slog.String("client", req.RemoteAddr.String()),
				slog.String("username", params.Username),
				slog.String("cause", "password invalid"),
				slog.String("user_ip", params.UserAddr.String()),
				slog.String("proxy_host", params.ProxyHost.String()))

			break
		}

		if allowedHost, allowedPort, err := net.SplitHostPort(user.ProxyHost); err == nil {

			hostIP, hostPort := utils.SplitIPPort(params.ProxyHost)

			if allow := net.ParseIP(allowedHost); allow != nil && hostIP != nil && !hostIP.Equal(allow) {

				slog.Info("RADIUS server: Unautorized",
					slog.String("client", req.RemoteAddr.String()),
					slog.String("username", params.Username),
					slog.String("cause", "host not allowed"),
					slog.String("allowed_host", allowedHost),
					slog.String("user_ip", params.UserAddr.String()),
					slog.String("proxy_host", params.ProxyHost.String()))

				break
			}

			if allow, _ := strconv.Atoi(allowedPort); (allow > 0 && hostPort > 0) && hostPort != allow {

				slog.Info("RADIUS server: Unautorized",
					slog.String("client", req.RemoteAddr.String()),
					slog.String("username", params.Username),
					slog.String("cause", "port not allowed"),
					slog.Int("allowed_port", allow),
					slog.String("user_ip", params.UserAddr.String()),
					slog.String("proxy_host", params.ProxyHost.String()))

				break
			}
		}

		slog.Info("RADIUS server: Peer accepted",
			slog.String("client", req.RemoteAddr.String()),
			slog.String("username", params.Username),
			slog.String("user_ip", params.UserAddr.String()),
			slog.String("proxy_host", params.ProxyHost.String()))

		reply := req.Response(radius.CodeAccessAccept)

		if err := user.ToPeer().MarshalPacket(reply); err != nil {
			slog.Warn("RADIUS server: Copy peer attributes",
				slog.String("client", req.RemoteAddr.String()),
				slog.String("username", params.Username),
				slog.String("err", err.Error()))
		}

		return reply
	}

	return req.Response(radius.CodeAccessReject)
}

func (handler *radiusHandler) HandleAccountingRequest(req *radius.Request) *radius.Packet {

	acct := radius_pkg.ParseAccountingDelta(req.Packet)

	slog.Info("RADIUS server: Accounting",
		slog.String("client", req.RemoteAddr.String()),
		slog.String("sess", acct.SessionID),
		slog.String("type", acct.Type.String()),
		slog.Int("rx", int(acct.RxBytes)),
		slog.Int("tx", int(acct.TxBytes)))

	return req.Response(radius.CodeAccountingResponse)
}

func (handler *radiusHandler) RefreshConfig(cfg RadiusServerConfiguration) {

	handler.mtx.Lock()
	defer handler.mtx.Unlock()

	handler.cfg = cfg

	if dacAddr := cfg.DacAddr; dacAddr != "" {
		slog.Debug("Executing DAC requests")
		handler.execDAC(&radius_pkg.Client{DacAddr: dacAddr, Secret: cfg.Secret})
		slog.Debug("DAC done")
	}
}

func (handler *radiusHandler) execDAC(client *radius_pkg.Client) {

	if handler.peerSet != nil {

		for _, user := range handler.cfg.Users {

			acctID := user.AccountingID()

			if _, has := handler.peerSet[acctID]; has {
				if err := client.SendCoA(user.ToPeer()); err != nil {
					slog.Error("RADIUS DAC: Send CoA",
						slog.String("addr", client.DacAddr),
						slog.String("acct_id", acctID),
						slog.String("err", err.Error()))
				} else {
					slog.Info("RADIUS DAC: Send CoA",
						slog.String("addr", client.DacAddr),
						slog.String("acct_id", acctID))
				}

				delete(handler.peerSet, acctID)
			}
		}

		for acctID := range handler.peerSet {
			if err := client.SendDM(acctID); err != nil {
				slog.Error("RADIUS DAC: Send DM",
					slog.String("addr", client.DacAddr),
					slog.String("acct_id", acctID),
					slog.String("err", err.Error()))
			} else {
				slog.Info("RADIUS DAC: Send DM",
					slog.String("addr", client.DacAddr),
					slog.String("acct_id", acctID))
			}
		}
	}

	handler.peerSet = map[string]struct{}{}

	for _, user := range handler.cfg.Users {
		handler.peerSet[user.AccountingID()] = struct{}{}
	}
}

func (handler *radiusHandler) userList() []RadiusUserConfig {
	handler.mtx.Lock()
	defer handler.mtx.Unlock()
	return handler.cfg.Users
}
