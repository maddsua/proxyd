package proxytable

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"maps"
	"net"
	"sync"
	"sync/atomic"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

type peerAuthenticator struct {
	slotName  string
	dnsTester *proxyd.DNSTester

	mtx        sync.Mutex
	peers      map[string]*peerSlot
	users      map[string]*peerSlot
	deltaQueue map[string]*TrafficDelta
}

func (auth *peerAuthenticator) AuthenticateWithPassword(ctx context.Context, _ net.Addr, clientIP net.IP, username, password string) (*proxyd.ProxySession, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	peer := auth.lookupPeer(username)
	if peer == nil {
		return nil, &proxyd.ProxyCredentialsError{}
	}

	defer peer.mtx.Unlock()

	maxAttempts, attemptWindow := peer.user.Options.RateLimiter()

	rlc := peer.authRl.SetNoExist(clientIP.String(), 0, attemptWindow)

	// deny any access if rate limited
	if rlc.Val >= uint64(maxAttempts) {

		// refresh the counter so that any subsequent request before the timeout will reset it. YOU WILL FUCKING WAIT!
		rlc.Bump()

		return nil, &proxyd.ProxyCredentialsError{Username: username, RetryAfter: rlc.Expires}
	}

	if subtle.ConstantTimeCompare([]byte(peer.user.Password), []byte(password)) != 1 {

		// increase attempt count and update couter expiration
		rlc.Val++
		rlc.Bump()

		return nil, &proxyd.ProxyCredentialsError{Username: username}
	}

	rlc.Val = 0

	return &peer.sess, nil
}

func (auth *peerAuthenticator) lookupPeer(username string) *peerSlot {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.users == nil {
		return nil
	}

	peer := auth.users[username]
	if peer == nil {
		return nil
	}

	peer.mtx.Lock()
	peer.wg.Wait()

	return peer
}

func (auth *peerAuthenticator) Peers() []PeerStatus {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	var entries []PeerStatus
	for _, peer := range auth.peers {

		next := PeerStatus{
			ID:      peer.sess.PeerID,
			Enabled: peer.sess.PeerEnabled,
		}

		if info := peer.user; info != nil {
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
		state.sess.Pool.Rebalance()
	}
}

func (auth *peerAuthenticator) Deltas() []TrafficDelta {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	for _, slot := range auth.peers {
		auth.sumPeerDelta(slot)
	}

	var entries []TrafficDelta
	for key, delta := range auth.deltaQueue {
		entries = append(entries, *delta)
		delete(auth.deltaQueue, key)
	}

	return entries
}

func (auth *peerAuthenticator) sumPeerDelta(peer *peerSlot) {

	rx := peer.sess.Pool.TrafficRx.Swap(0)
	tx := peer.sess.Pool.TrafficTx.Swap(0)

	if rx == 0 && tx == 0 {
		return
	}

	if auth.deltaQueue == nil {
		auth.deltaQueue = map[string]*TrafficDelta{}
	}

	peerID := peer.sess.PeerID
	delta := auth.deltaQueue[peerID]
	if delta == nil {
		delta = &TrafficDelta{PeerID: peerID}
		auth.deltaQueue[peerID] = delta
	}

	delta.RxBytes += rx
	delta.TxBytes += tx
}

func (auth *peerAuthenticator) RefreshPeers(ctx context.Context, peerList []ProxyTablePeerEntry) {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	// enumerate existing peers
	staleMap := map[string]*peerSlot{}
	if auth.peers == nil {
		auth.peers = map[string]*peerSlot{}
	} else {
		maps.Copy(staleMap, auth.peers)
	}

	var invalidIdReported atomic.Bool

	// go over a new peer list and create new peers or update existing
	for _, entry := range peerList {

		if entry.ID == "" {
			if invalidIdReported.CompareAndSwap(false, true) {
				slog.Warn("PeerAuthenticator: Encountered invalid peer IDs",
					slog.String("slot", auth.slotName))
			}
			continue
		}

		delete(staleMap, entry.ID)
		auth.refreshPeer(entry)
	}

	// purge peers that aren't present in the new list
	for key, peer := range staleMap {

		slog.Info("PeerAuthenticator: Remove peer",
			slog.String("slot", auth.slotName),
			slog.String("peer_id", peer.sess.PeerID))

		peer.sess.Reset()
		auth.sumPeerDelta(peer)

		delete(auth.peers, key)
	}

	// recreate username index
	auth.users = map[string]*peerSlot{}
	for _, peer := range auth.peers {
		if info := peer.user; info != nil && info.Username != "" {
			auth.users[info.Username] = peer
		}
	}
}

func (auth *peerAuthenticator) refreshPeer(entry ProxyTablePeerEntry) {

	var sessionReset bool

	peer, peerExisted := auth.peers[entry.ID]
	if peer == nil {
		peer = &peerSlot{
			sess: proxyd.ProxySession{PeerID: entry.ID},
		}
		auth.peers[entry.ID] = peer
	}

	peer.mtx.Lock()
	defer peer.mtx.Unlock()

	if unwrapUserInfo(peer.user) != unwrapUserInfo(entry.Userinfo) {

		if peerExisted {
			slog.Info("PeerAuthenticator: Update credentials",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()))
		}

		peer.user = entry.Userinfo
		peer.authRl.Clear()

		sessionReset = true
	}

	if peer.sess.PeerEnabled != entry.Enabled {

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

		peer.sess.PeerEnabled = entry.Enabled
		sessionReset = true
	}

	if wantDNS := parseDnsServerAddr(entry.DNS); !peer.sess.DNS.Server.Load().Equal(wantDNS) {

		// the dns update is a bit complicated here,
		// but it basically boils down to making sure
		// that you're not blocking the whole authenticator
		// while checking whether or not a provided server is valid

		var applyResult = func(err error) {

			if err != nil {
				slog.Warn("PeerAuthenticator: DNS server cannot be assigned",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.String("dns", entry.DNS),
					slog.String("err", err.Error()))
				return
			}

			if peerExisted {
				slog.Info("PeerAuthenticator: Update DNS server",
					slog.String("slot", auth.slotName),
					slog.String("peer", peer.displayName()),
					slog.String("dns", wantDNS.Name()))
			}
			peer.sess.DNS.Server.Store(wantDNS)
		}

		// check the cache first to speed up tests of frequently used servers,
		// and only go poke at it if that is absolutely necessary

		if err, valid := auth.dnsTester.LookupCached(wantDNS.Addr()); valid {
			applyResult(err)
		} else if peer.dnsLocked.CompareAndSwap(false, true) {

			// an atomic bool acts as a guard here to make sure that
			// the subsequent refresh calls don't create a logic race,
			// where the same DNS server is checked by multiple routines in parallel.
			// this, however can cause the DNS to lag behind until the next update cycle

			peer.wg.Add(1)

			go func() {
				defer peer.wg.Done()
				defer peer.dnsLocked.Store(false)
				applyResult(auth.dnsTester.Test(context.Background(), wantDNS.Addr()))
			}()
		}
	}

	if wantOutboundAddr, err := unwrapPeerOutboundIP(entry.OutboundAddr); err != nil {

		slog.Warn("PeerAuthenticator: Outbound IP cannot be assigned",
			slog.String("slot", auth.slotName),
			slog.String("peer", peer.displayName()),
			slog.String("addr", entry.OutboundAddr),
			slog.String("err", err.Error()))

	} else if !peer.sess.Dialer.OutboundAddr.Load().Equal(wantOutboundAddr) {

		if peerExisted {
			slog.Info("PeerAuthenticator: Update outbound address",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.String("addr", wantOutboundAddr.String()))
		}

		peer.sess.Dialer.OutboundAddr.Store(wantOutboundAddr)
		sessionReset = true
	}

	if peer.sess.Pool.ConnectionLimit() != entry.MaxConnections {

		if peerExisted {
			slog.Info("PeerAuthenticator: Update connection limit",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.Int("maxconn", entry.MaxConnections))
		}

		if err := peer.sess.Pool.SetConnectionLimit(entry.MaxConnections); err != nil {
			slog.Error("PeerAuthenticator: Update connection limit",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.Int("maxconn", entry.MaxConnections),
				slog.String("err", err.Error()))
		}
	}

	wantRx, wantTx := unwrapPeerBandwidth(entry.Bandwidth)
	if haveRx, haveTx := peer.sess.Pool.Bandwidth(); wantRx != haveRx || wantTx != haveTx {

		if peerExisted {
			slog.Info("PeerAuthenticator: Update bandwidth",
				slog.String("slot", auth.slotName),
				slog.String("peer", peer.displayName()),
				slog.Int64("rx_rate", wantRx),
				slog.Int64("tx_rate", wantTx))
		}

		peer.sess.Pool.SetBandwidth(wantRx, wantTx)
	}

	if peerExisted && sessionReset {

		slog.Debug("PeerAuthenticator: Forcing re-auth",
			slog.String("slot", auth.slotName),
			slog.String("peer", peer.displayName()))

		peer.sess.Reset()
	}

	if !peerExisted {

		rxMax, txMax := peer.sess.Pool.Bandwidth()

		slog.Info("PeerAuthenticator: Add peer",
			slog.String("slot", auth.slotName),
			slog.String("peer", peer.displayName()),
			slog.String("addr", peer.sess.Dialer.OutboundAddr.Load().String()),
			slog.String("dns", peer.sess.DNS.Server.Load().Name()),
			slog.Int("max_conn", peer.sess.Pool.ConnectionLimit()),
			slog.Int64("rx_rate", rxMax),
			slog.Int64("tx_rate", txMax))
	}
}

func (auth *peerAuthenticator) ResetPeers() {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.peers == nil {
		return
	}

	for _, peer := range auth.peers {
		peer.sess.Reset()
		auth.sumPeerDelta(peer)
	}

	auth.peers = nil
	auth.users = nil
}

type peerSlot struct {
	user      *ProxyPeerUserInfo
	authRl    utils.ExpireMap[uint64]
	sess      proxyd.ProxySession
	mtx       sync.Mutex
	wg        sync.WaitGroup
	dnsLocked atomic.Bool
}

func (slot *peerSlot) displayName() string {
	if user := slot.user; user != nil {
		return user.Username
	}
	return slot.sess.PeerID
}

func unwrapPeerBandwidth(bw *ProxyPeerBandwidth) (rx, tx int64) {
	if bw == nil {
		return 0, 0
	}
	return max(0, bw.MaxRx), max(0, bw.MaxTx)
}

func unwrapPeerOutboundIP(addr string) (*proxyd.PeerAddr, error) {

	if addr == "" {
		return nil, nil
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		return nil, errors.New("invalid IP address")
	} else if !ip.IsGlobalUnicast() {
		return nil, errors.New("ip not public")
	}

	if !utils.IPBindable(ip) {
		return nil, errors.New("ip not bindable")
	}

	return &proxyd.PeerAddr{IP: ip}, nil
}

func parseDnsServerAddr(addr string) *proxyd.DNSAddr {
	if addr != "" {
		return &proxyd.DNSAddr{ServerAddr: addr}
	}
	return nil
}

func unwrapUserInfo(userinfo *ProxyPeerUserInfo) string {
	if userinfo != nil {
		return userinfo.Username + ":" + userinfo.Password
	}
	return ""
}
