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
	params radius_pkg.AuthorizationParams
	sess   proxyd.ProxySession

	init    atomic.Bool
	done    atomic.Bool
	mtx     sync.Mutex
	expires time.Time

	slotID           string
	lastUserActivity time.Time
	idleTTL          time.Duration

	acctUid       string
	acctSid       string
	acctMtx       sync.Mutex
	acctWg        sync.WaitGroup
	lastAccounted time.Time

	dnsTester *proxyd.DNSTester
	upstream  *radius_pkg.Client
}

func (state *peerSessionState) Refresh(ctx context.Context, peer *radius_pkg.PeerAuthorization) error {

	state.mtx.Lock()
	defer state.mtx.Unlock()

	if state.done.Load() {
		return fmt.Errorf("invalid state")
	}

	if state.acctSid != "" && state.acctSid != peer.AcctSessionID {
		return fmt.Errorf("accounting session id doesn't match: %s/%s", state.acctSid, peer.AcctSessionID)
	}

	isInit := state.init.CompareAndSwap(false, true)

	state.acctSid = peer.AcctSessionID
	state.acctUid = peer.ChargeableUserID

	state.expires = time.Now().Add(unwrapTTL(peer.Timeout, DefaultSessionTTL))
	state.idleTTL = unwrapTTL(peer.IdleTimeout, DefaultReauthPeriod)

	// enable peer and update its id as it is only an informational field
	state.sess.PeerID = unwrapSessionPeerID(peer.ChargeableUserID, state.params.Username, peer.AcctSessionID)
	state.sess.PeerEnabled = true

	if state.sess.Pool.ConnectionLimit() != peer.ConnectionLimit {

		if !isInit {
			slog.Info("RADIUS: Update connection limit",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.Int("maxconn", peer.ConnectionLimit))
		}

		state.sess.Pool.SetConnectionLimit(peer.ConnectionLimit)
	}

	if rxRate, txRate := state.sess.Pool.Bandwidth(); rxRate != peer.DataRateRx || txRate != peer.DataRateTx {

		if !isInit {
			slog.Info("RADIUS: Update bandwidth",
				slog.String("slot", state.slotID),
				slog.String("peer", state.sess.PeerID),
				slog.Int("rx", peer.DataRateRx),
				slog.Int("tx", peer.DataRateTx))
		}

		state.sess.Pool.SetBandwidth(peer.DataRateRx, peer.DataRateTx)
	}

	var sessionReset bool

	wrantFramedIP, err := unwrapFramedIP(peer.FramedIP)
	if err != nil {
		slog.Warn("RADIUS: Update framed IP",
			slog.String("slot", state.slotID),
			slog.String("peer", state.sess.PeerID),
			slog.String("framedIP", peer.FramedIP.String()),
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

	state.Account(ctx)

	return nil
}

func (state *peerSessionState) Account(ctx context.Context) {

	if state.acctSid == "" && state.acctUid == "" {
		return
	}

	state.acctMtx.Lock()

	rx := state.sess.Pool.TrafficRx.Load()
	tx := state.sess.Pool.TrafficTx.Load()

	acctType := rfc2866.AcctStatusType_Value_InterimUpdate
	if state.done.Load() {
		acctType = rfc2866.AcctStatusType_Value_Stop
	} else if state.lastAccounted.IsZero() {
		acctType = rfc2866.AcctStatusType_Value_Start
	} else if time.Since(state.lastAccounted) < DefaultAccountingInterval || rx <= 0 && tx <= 0 {
		state.acctMtx.Unlock()
		return
	}

	state.acctWg.Add(1)

	go func() {

		defer state.acctMtx.Unlock()
		defer state.acctWg.Done()

		if err := state.upstream.AccountTraffic(ctx, radius_pkg.AccountingParams{
			Type:             acctType,
			SessionID:        state.acctSid,
			ChargeableUserID: state.acctUid,
			RxBytes:          uint32(rx),
			TxBytes:          uint32(tx),
		}); err != nil {

			slog.Error("RADIUS: Account traffic",
				slog.String("peer_id", state.sess.PeerID),
				slog.String("acct_id", state.acctSid),
				slog.String("acct_type", acctType.String()),
				slog.String("err", err.Error()))

			return
		}

		state.sess.Pool.TrafficRx.Add(-rx)
		state.sess.Pool.TrafficTx.Add(-tx)

		state.lastAccounted = time.Now()
	}()
}

func (state *peerSessionState) Reauthenticate(ctx context.Context) error {
	peer, err := state.upstream.Authorize(ctx, state.params)
	if err != nil {
		return err
	}
	return state.Refresh(ctx, peer)
}

func (state *peerSessionState) Terminate(ctx context.Context) {

	if !state.done.CompareAndSwap(false, true) {
		return
	}

	state.sess.Reset()

	state.Account(ctx)
	state.acctWg.Wait()
}

func unwrapTTL(values ...time.Duration) time.Duration {
	for _, val := range values {
		if val > time.Second {
			return val
		}
	}
	panic("no non-zero ttl options")
}

func unwrapFramedIP(ip net.IP) (*proxyd.PeerAddr, error) {

	if ip == nil {
		return nil, nil
	}

	if !utils.IPBindable(ip) {
		return nil, errors.New("ip address not assignable")
	}

	return &proxyd.PeerAddr{IP: ip}, nil
}

func unwrapDnsServerAddr(addr net.IP) string {
	if addr != nil {
		return addr.String()
	}
	return ""
}

func unwrapSessionPeerID(peerID, username, sessionID string) string {

	if peerID != "" {
		return peerID
	} else if username != "" {
		return username
	} else if sessionID != "" {
		return "sess:" + sessionID
	}

	return ""
}
