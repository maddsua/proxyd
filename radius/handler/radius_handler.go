package handler

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	radius "github.com/maddsua/layeh-radius"
	"github.com/maddsua/proxyd/local"
	radius_pkg "github.com/maddsua/proxyd/radius"
	"github.com/maddsua/proxyd/utils"
)

type RadiusUserConfig struct {
	ProxyHost                string `json:"proxy_host" yaml:"proxy_host"`
	RadiusSessionTTl         int    `json:"radius_session_ttl" yaml:"radius_session_ttl"`
	RadiusSessionIdleTimeout int    `json:"radius_session_idle_timeout" yaml:"radius_session_idle_timeout"`
	local.UserConfig         `yaml:",inline"`
}

func (cfg *RadiusUserConfig) AccountingID() string {
	return cfg.ProxyHost + ":" + cfg.Username
}

func (cfg *RadiusUserConfig) ToPeer() *radius_pkg.PeerAuthorization {
	return &radius_pkg.PeerAuthorization{
		AcctSessionID:    cfg.AccountingID(),
		ChargeableUserID: cfg.Username,
		FramedIP:         net.ParseIP(cfg.OutboundAddr),
		DNSServer:        net.ParseIP(cfg.DNS),
		Timeout:          time.Duration(max(0, cfg.RadiusSessionTTl)),
		IdleTimeout:      time.Duration(max(0, cfg.RadiusSessionIdleTimeout)),
		ConnectionLimit:  cfg.MaxConn,
		MaxRxRate:        max(0, int64(cfg.BandwidthKbit)*1000),
		MaxTxRate:        max(0, int64(cfg.BandwidthKbit)*1000),
	}
}

type Handler struct {
	DAClient *radius_pkg.Client

	userList []RadiusUserConfig
	mtx      sync.Mutex
	peerSet  map[string]struct{}
}

func (rh *Handler) ServeRADIUS(wrt radius.ResponseWriter, req *radius.Request) {

	switch req.Code {

	case radius.CodeAccessRequest:
		wrt.Write(rh.HandleAccessRequest(req))

	case radius.CodeAccountingRequest:
		wrt.Write(rh.HandleAccountingRequest(req))

	default:
		slog.Warn("RADIUS server: Unexpected code",
			slog.String("client", req.RemoteAddr.String()),
			slog.String("code", req.Code.String()))
	}
}

func (rh *Handler) HandleAccessRequest(req *radius.Request) *radius.Packet {

	params := radius_pkg.ParsePeerCredentials(req.Packet)

	user := rh.lookupUser(params.Username)
	if user == nil {

		slog.Info("RADIUS server: Unautorized",
			slog.String("client", req.RemoteAddr.String()),
			slog.String("username", params.Username),
			slog.String("cause", "user not found"),
			slog.String("user_ip", params.UserAddr.String()),
			slog.String("proxy_host", params.ProxyHost.String()))

		return req.Response(radius.CodeAccessReject)
	}

	if subtle.ConstantTimeCompare([]byte(user.Password), []byte(params.Password)) != 1 {

		slog.Info("RADIUS server: Unautorized",
			slog.String("client", req.RemoteAddr.String()),
			slog.String("username", params.Username),
			slog.String("cause", "password invalid"),
			slog.String("user_ip", params.UserAddr.String()),
			slog.String("proxy_host", params.ProxyHost.String()))

		return req.Response(radius.CodeAccessReject)
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

			return req.Response(radius.CodeAccessReject)
		}

		if allow, _ := strconv.Atoi(allowedPort); (allow > 0 && hostPort > 0) && hostPort != allow {

			slog.Info("RADIUS server: Unautorized",
				slog.String("client", req.RemoteAddr.String()),
				slog.String("username", params.Username),
				slog.String("cause", "port not allowed"),
				slog.Int("allowed_port", allow),
				slog.String("user_ip", params.UserAddr.String()),
				slog.String("proxy_host", params.ProxyHost.String()))

			return req.Response(radius.CodeAccessReject)
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

func (rh *Handler) HandleAccountingRequest(req *radius.Request) *radius.Packet {

	acct := radius_pkg.ParseAccountingDelta(req.Packet)

	slog.Info("RADIUS server: Accounting",
		slog.String("client", req.RemoteAddr.String()),
		slog.String("sess", acct.SessionID),
		slog.String("type", acct.Type.String()),
		slog.Int("rx", int(acct.RxBytes)),
		slog.Int("tx", int(acct.TxBytes)))

	return req.Response(radius.CodeAccountingResponse)
}

func (rh *Handler) SetUsers(users []RadiusUserConfig) {
	rh.mtx.Lock()
	defer rh.mtx.Unlock()
	rh.userList = users
	rh.execDAC()
}

func (rh *Handler) execDAC() {

	client := rh.DAClient
	if client == nil {
		return
	}

	if rh.peerSet != nil {

		slog.Debug("Executing DAC requests")
		defer slog.Debug("DAC done")

		for _, user := range rh.userList {

			acctID := user.AccountingID()

			if _, has := rh.peerSet[acctID]; has {
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

				delete(rh.peerSet, acctID)
			}
		}

		for acctID := range rh.peerSet {
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

	rh.peerSet = map[string]struct{}{}

	for _, user := range rh.userList {
		rh.peerSet[user.AccountingID()] = struct{}{}
	}
}

func (rh *Handler) lookupUser(username string) *RadiusUserConfig {

	rh.mtx.Lock()
	defer rh.mtx.Unlock()

	for _, entry := range rh.userList {
		if entry.Username == username {
			return &entry
		}
	}

	return nil
}
