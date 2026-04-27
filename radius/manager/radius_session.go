package radiusmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maddsua/layeh-radius/rfc2866"
	"github.com/maddsua/proxyd"
	radius_pkg "github.com/maddsua/proxyd/radius"
	"github.com/maddsua/proxyd/utils"
)

const DefaultSessionTTL = time.Minute
const DefaultReauthPeriod = 90 * time.Second
const DefaultAccountingInterval = 30 * time.Second

type peerSessionState struct {
	params radius_pkg.PeerCredentials
	sess   proxyd.ProxySession

	init    atomic.Bool
	done    atomic.Bool
	mtx     sync.Mutex
	expires time.Time

	slotID           string
	lastUserActivity time.Time
	idleTimeout      time.Duration

	acctUid       string
	acctSid       string
	acctMtx       sync.Mutex
	lastAccounted time.Time

	dnsTester *proxyd.DNSTester
	upstream  *radius_pkg.Client
}

func (state *peerSessionState) Refresh(ctx context.Context, peer *radius_pkg.PeerAuthorization) error {
	state.mtx.Lock()
	defer state.mtx.Unlock()
	return state.refresh(ctx, peer)
}

func (state *peerSessionState) refresh(ctx context.Context, peer *radius_pkg.PeerAuthorization) error {

	if state.done.Load() {
		return fmt.Errorf("invalid state")
	}

	if state.acctSid != "" && state.acctSid != peer.AcctSessionID {
		return fmt.Errorf("accounting session id doesn't match: %s/%s", state.acctSid, peer.AcctSessionID)
	}

	// Set session activation options.
	// These can be racy under certain conditions, but they do not change program's behavior
	if wantPeerID := utils.NonZeroString(
		peer.ChargeableUserID,
		state.params.Username,
		"sess:"+peer.AcctSessionID,
	); state.sess.PeerID != wantPeerID {
		state.sess.PeerID = wantPeerID
		state.sess.PeerEnabled = true
	}

	isInit := state.init.CompareAndSwap(false, true)

	state.acctSid = peer.AcctSessionID
	state.acctUid = peer.ChargeableUserID

	state.expires = time.Now().Add(utils.NonZeroDuration(peer.Timeout, DefaultSessionTTL))
	state.idleTimeout = utils.NonZeroDuration(peer.IdleTimeout, DefaultReauthPeriod)

	if state.sess.Pool.ConnectionLimit() != peer.ConnectionLimit {

		if !isInit {
			slog.Info("RADIUS: Update connection limit",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.Int("maxconn", peer.ConnectionLimit))
		}

		state.sess.Pool.SetConnectionLimit(peer.ConnectionLimit)
	}

	if rxRate, txRate := state.sess.Pool.Bandwidth(); rxRate != peer.MaxRxRate || txRate != peer.MaxTxRate {

		if !isInit {
			slog.Info("RADIUS: Update bandwidth",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.Int64("rx", peer.MaxRxRate),
				slog.Int64("tx", peer.MaxTxRate))
		}

		state.sess.Pool.SetBandwidth(peer.MaxRxRate, peer.MaxTxRate)
	}

	var sessionReset bool

	wrantFramedIP, err := unwrapFramedIP(peer.FramedIP)
	if err != nil {
		slog.Warn("RADIUS: New framed IP cannot be assigned",
			slog.String("slot", state.slotID),
			slog.String("peer", state.sess.PeerID),
			slog.String("framed_ip", peer.FramedIP.String()),
			slog.String("err", err.Error()))
	}

	if wrantFramedIP.String() != state.sess.Dialer.OutboundAddr.String() {

		if !isInit {
			slog.Info("RADIUS: Update framed IP",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.String("framed_ip", wrantFramedIP.String()))
		}

		state.sess.Dialer.OutboundAddr = wrantFramedIP
		sessionReset = true
	}

	if wantDns := unwrapDnsServerAddr(peer.DNSServer); state.sess.DNS.ServerAddr != wantDns {

		if !isInit {
			slog.Info("RADIUS: Update DNS server",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.String("dns_server", wantDns))
		}

		if state.dnsTester != nil && wantDns != "" {

			if err := state.dnsTester.Test(ctx, wantDns); err != nil {

				slog.Warn("RADIUS: Peer's DNS server unreachable. Default DNS server will be used",
					slog.String("slot", state.slotID),
					slog.String("peer", state.sess.PeerID),
					slog.String("dns_server", wantDns),
					slog.String("err", err.Error()))

				wantDns = ""
			}
		}

		state.sess.DNS.ServerAddr = wantDns
	}

	if sessionReset {
		state.sess.Reset()
	}

	state.account(ctx, false)

	return nil
}

func (state *peerSessionState) account(ctx context.Context, syncFlag bool) {

	state.acctMtx.Lock()

	params, valid := state.prepareAcct()
	if !valid {
		state.acctMtx.Unlock()
		return
	}

	if syncFlag {
		defer state.acctMtx.Unlock()
		state.sendAcct(ctx, params)
		return
	}

	go func() {
		defer state.acctMtx.Unlock()
		state.sendAcct(ctx, params)
	}()
}

func (state *peerSessionState) prepareAcct() (radius_pkg.AccountingDelta, bool) {

	params := radius_pkg.AccountingDelta{
		ChargeableUserID: state.acctUid,
		SessionID:        state.acctSid,
		RxBytes:          state.sess.Pool.TrafficRx.Load(),
		TxBytes:          state.sess.Pool.TrafficTx.Load(),
	}

	// skip any accounting if there's no session identification
	if params.ChargeableUserID == "" && params.SessionID == "" {
		return radius_pkg.AccountingDelta{}, false
	}

	// always trigger accounting for freshly started and stopped sessions
	if state.done.Load() {
		params.Type = rfc2866.AcctStatusType_Value_Stop
		return params, true
	} else if state.lastAccounted.IsZero() {
		params.Type = rfc2866.AcctStatusType_Value_Start
		return params, true
	}

	// only report the rest if there is a traffic delta and it wasn't reported too recently
	if !params.IsZero() && time.Since(state.lastAccounted) >= DefaultAccountingInterval {
		params.Type = rfc2866.AcctStatusType_Value_InterimUpdate
		return params, true
	}

	return radius_pkg.AccountingDelta{}, false
}

func (state *peerSessionState) sendAcct(ctx context.Context, params radius_pkg.AccountingDelta) {

	if err := state.upstream.AccountTraffic(ctx, params); err != nil {

		slog.Error("RADIUS: Account traffic",
			slog.String("peer_id", state.sess.PeerID),
			slog.String("acct_id", state.acctSid),
			slog.String("acct_type", params.Type.String()),
			slog.String("err", err.Error()))

		return
	}

	state.sess.Pool.TrafficRx.Add(-params.RxBytes)
	state.sess.Pool.TrafficTx.Add(-params.TxBytes)

	state.lastAccounted = time.Now()
}

func (state *peerSessionState) reauthenticate(ctx context.Context) error {
	peer, err := state.upstream.Authorize(ctx, state.params)
	if err != nil {
		return err
	}
	return state.refresh(ctx, peer)
}

func (state *peerSessionState) Terminate(ctx context.Context) {

	if !state.done.CompareAndSwap(false, true) {
		return
	}

	state.sess.Reset()
	state.account(ctx, true)
}

func unwrapFramedIP(ip net.IP) (*proxyd.PeerAddr, error) {

	if ip == nil {
		return nil, nil
	} else if !ip.IsGlobalUnicast() {
		return nil, errors.New("ip not public")
	}

	if !utils.IPBindable(ip) {
		return nil, errors.New("ip not bindable")
	}

	return &proxyd.PeerAddr{IP: ip}, nil
}

func unwrapDnsServerAddr(addr net.IP) string {
	if addr != nil {
		return addr.String()
	}
	return ""
}
