package proxytable

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"maps"
	"net"
	"sync"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

type peerAuthenticator struct {
	slotName  string
	dnsTester *proxyd.DNSTester

	mtx        sync.Mutex
	peers      map[string]*peerSlot
	users      map[string]*peerSlot
	deltaQueue map[string]TrafficDelta
}

func (auth *peerAuthenticator) AuthenticateWithPassword(ctx context.Context, _ net.Addr, clientIP net.IP, username, password string) (*proxyd.ProxySession, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.users == nil {
		return nil, &proxyd.ProxyCredentialsError{}
	}

	slot := auth.users[username]
	if slot == nil {
		return nil, &proxyd.ProxyCredentialsError{}
	}

	maxAttempts, attemptWindow := slot.UserInfo.Options.RateLimiter()

	rlc := slot.AuthAttemptRL.SetNoExist(clientIP.String(), 0, attemptWindow)

	// deny any access if rate limited
	if rlc.Val >= uint64(maxAttempts) {

		// refresh the counter so that any subsequent request before the timeout will reset it. YOU WILL FUCKING WAIT!
		rlc.Bump()

		return nil, &proxyd.ProxyCredentialsError{Username: username, RetryAfter: rlc.Expires}
	}

	if subtle.ConstantTimeCompare([]byte(slot.UserInfo.Password), []byte(password)) != 1 {

		// increase attempt count and update couter expiration
		rlc.Val++
		rlc.Bump()

		return nil, &proxyd.ProxyCredentialsError{Username: username}
	}

	rlc.Val = 0

	return &slot.ProxySession, nil
}

func (auth *peerAuthenticator) Peers() []PeerStatus {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	var entries []PeerStatus
	for _, peer := range auth.peers {

		next := PeerStatus{
			ID:      peer.ProxySession.PeerID,
			Enabled: peer.ProxySession.PeerEnabled,
		}

		if info := peer.UserInfo; info != nil {
			next.Username = info.Username
		}

		entries = append(entries, next)
	}

	return entries
}

func (auth *peerAuthenticator) RebalancePools() {

	if auth.peers == nil {
		return
	}

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	for _, state := range auth.peers {
		state.Pool.Rebalance()
	}
}

func (auth *peerAuthenticator) Deltas() []TrafficDelta {

	if auth.peers == nil {
		return nil
	}

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	for _, slot := range auth.peers {
		auth.collectPeerDelta(slot)
	}

	var entries []TrafficDelta
	for key, delta := range auth.deltaQueue {
		entries = append(entries, delta)
		delete(auth.deltaQueue, key)
	}

	return entries
}

func (auth *peerAuthenticator) collectPeerDelta(peer *peerSlot) {

	rx := peer.Pool.TrafficRx.Swap(0)
	tx := peer.Pool.TrafficRx.Swap(0)

	if rx == 0 && tx == 0 {
		return
	}

	if auth.deltaQueue == nil {
		auth.deltaQueue = map[string]TrafficDelta{}
	}

	delta := auth.deltaQueue[peer.PeerID]

	delta.RxBytes += rx
	delta.TxBytes += tx

	auth.deltaQueue[peer.PeerID] = delta
}

func (auth *peerAuthenticator) RefreshPeers(ctx context.Context, peerList []ProxyTablePeerEntry) {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	// prepare remove-list
	staleMap := map[string]*peerSlot{}
	if auth.peers == nil {
		auth.peers = map[string]*peerSlot{}
	} else {
		maps.Copy(staleMap, auth.peers)
	}

	// iterate over a new table and update peers
	for _, entry := range peerList {

		if entry.ID == "" {
			continue
		}

		delete(staleMap, entry.ID)

		var sessionReset bool

		peer, peerExisted := auth.peers[entry.ID]
		if peer == nil {
			peer = &peerSlot{
				ProxySession: proxyd.ProxySession{
					PeerID: entry.ID,
				},
			}
			auth.peers[entry.ID] = peer
		}

		if unwrapUserInfo(peer.UserInfo) != unwrapUserInfo(entry.Userinfo) {

			if peerExisted {
				slog.Info("PeerAuthenticator: Update credentials",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()))
			}

			peer.UserInfo = entry.Userinfo
			peer.AuthAttemptRL.Clear()

			sessionReset = true
		}

		if peer.PeerEnabled != entry.Enabled {

			if peerExisted {
				if entry.Enabled {
					slog.Info("PeerAuthenticator: Enable peer",
						slog.String("slot", auth.slotName),
						slog.String("peer", peer.displayName()))
				} else {
					slog.Info("PeerAuthenticator: Disable peer",
						slog.String("slot", auth.slotName),
						slog.String("peer", peer.displayName()))
				}
			}

			peer.PeerEnabled = entry.Enabled
			sessionReset = true
		}

		if wantDNS := entry.DNS; peer.DNS.ServerAddr != wantDNS {

			slog.Info("PeerAuthenticator: Update DNS server",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.String("dns_server", wantDNS))

			if auth.dnsTester != nil && wantDNS != "" {
				go auth.setPeerDnsServer(ctx, peer, wantDNS)
			} else {
				peer.DNS.ServerAddr = wantDNS
			}
		}

		wantOutboundAddr, err := unwrapPeerOutboundIP(entry.OutboundAddr)
		if err != nil {
			slog.Warn("PeerAuthenticator: Outbound address",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.String("addr", entry.OutboundAddr),
				slog.String("err", err.Error()))
		}

		if wantOutboundAddr.String() != peer.Dialer.OutboundAddr.String() {

			if peerExisted {
				slog.Info("PeerAuthenticator: Update outbound address",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.String("addr", wantOutboundAddr.String()))
			}

			peer.Dialer.OutboundAddr = wantOutboundAddr
			sessionReset = true
		}

		if peer.Pool.ConnectionLimit() != entry.MaxConnections {

			if peerExisted {
				slog.Info("PeerAuthenticator: Update connection limit",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.Int("maxconn", entry.MaxConnections))
			}

			if err := peer.Pool.SetConnectionLimit(entry.MaxConnections); err != nil {
				slog.Error("PeerAuthenticator: Update connection limit",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.Int("maxconn", entry.MaxConnections),
					slog.String("err", err.Error()))
			}
		}

		wantRx, wantTx := unwrapPeerBandwidth(entry.Bandwidth)
		if haveRx, haveTx := peer.Pool.Bandwidth(); wantRx != haveRx || wantTx != haveTx {

			if peerExisted {
				slog.Info("PeerAuthenticator: Update bandwidth",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.Int("rx", wantRx),
					slog.Int("tx", wantTx))
			}

			peer.Pool.SetBandwidth(wantRx, wantTx)
		}

		if peerExisted && sessionReset {

			slog.Debug("PeerAuthenticator: Forcing re-auth",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()))

			peer.ProxySession.Reset()
		}

		if !peerExisted {

			rxMax, txMax := peer.Pool.Bandwidth()

			slog.Info("PeerAuthenticator: Add peer",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.String("addr", peer.Dialer.OutboundAddr.String()),
				slog.String("dns", peer.DNS.ServerName()),
				slog.Int("max_conn", peer.Pool.ConnectionLimit()),
				slog.Int("rx_max", rxMax),
				slog.Int("tx_max", txMax))
		}
	}

	// remove outdated peers
	for key, peer := range staleMap {

		slog.Info("PeerAuthenticator: Remove peer",
			slog.String("slot", auth.slotName),
			slog.String("peer_id", peer.PeerID))

		peer.Reset()
		auth.collectPeerDelta(peer)

		delete(auth.peers, key)
	}

	// recreate username map
	auth.users = map[string]*peerSlot{}
	for _, peer := range auth.peers {
		if info := peer.UserInfo; info != nil && info.Username != "" {
			auth.users[info.Username] = peer
		}
	}
}

func (auth *peerAuthenticator) ResetPeers() {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.peers == nil {
		return
	}

	for _, peer := range auth.peers {
		peer.Reset()
		auth.collectPeerDelta(peer)
	}

	auth.peers = nil
	auth.users = nil
}

func (auth *peerAuthenticator) setPeerDnsServer(ctx context.Context, peer *peerSlot, addr string) {

	if err := auth.dnsTester.Test(ctx, addr); err != nil {

		slog.Warn("PeerAuthenticator: Test DNS server",
			slog.String("slot", auth.slotName),
			slog.String("peer", peer.displayName()),
			slog.String("dns_server", addr),
			slog.String("err", err.Error()))

		peer.DNS.ServerAddr = ""
		return
	}

	peer.DNS.ServerAddr = addr
}

type peerSlot struct {
	proxyd.ProxySession

	UserInfo      *ProxyPeerUserInfo
	AuthAttemptRL utils.ExpireMap[uint64]
}

func (slot *peerSlot) displayName() string {
	if user := slot.UserInfo; user != nil {
		return user.Username
	}
	return slot.ProxySession.PeerID
}

func unwrapPeerBandwidth(bw *ProxyPeerBandwidth) (rx, tx int) {
	if bw == nil {
		return 0, 0
	}
	return max(0, bw.RxBytes), max(0, bw.TxBytes)
}

func unwrapPeerOutboundIP(addr string) (*proxyd.PeerAddr, error) {

	if addr == "" {
		return nil, nil
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		return nil, errors.New("invalid IP address")
	}

	if !utils.IPBindable(ip) {
		return nil, errors.New("ip address not assignable")
	}

	return &proxyd.PeerAddr{IP: ip}, nil
}

func unwrapUserInfo(userinfo *ProxyPeerUserInfo) string {
	if userinfo != nil {
		return userinfo.Username + ":" + userinfo.Password
	}
	return ""
}
