package proxytable

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"maps"
	"net"
	"sync"
	"sync/atomic"

	"github.com/maddsua/proxyd"
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

	// make sure any blocking updates are finished befure a session is returned
	peer.wg.Wait()

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

		peer := auth.peers[entry.ID]
		if peer == nil {
			peer = &peerSlot{
				parentName: auth.slotName,
				sess:       proxyd.ProxySession{PeerID: entry.ID},
				dnsTester:  auth.dnsTester,
			}
			auth.peers[entry.ID] = peer
		}

		peer.mtx.Lock()
		peer.refresh(entry)
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
