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

	radius "github.com/maddsua/layeh-radius"
	"github.com/maddsua/layeh-radius/rfc2866"
	"github.com/maddsua/layeh-radius/rfc3576"
	"github.com/maddsua/proxyd"
	radius_pkg "github.com/maddsua/proxyd/radius"
)

type peerEntry struct {
	sess *peerSessionState
	miss *peerCredentialsMiss
	mtx  sync.Mutex
}

func (entry *peerEntry) reset() {
	entry.miss = nil
	entry.sess = nil
}

type peerCredentialsMiss struct {
	radius_pkg.AuthorizationParams
	expires time.Time
}

type peerAuthenticator struct {
	Client radius_pkg.Client

	mtx           sync.Mutex
	wg            sync.WaitGroup
	index         map[string]*peerEntry
	refreshInit   atomic.Bool
	refreshCtx    context.Context
	cancelRefresh context.CancelFunc

	dnsTester proxyd.DNSTester
}

func (auth *peerAuthenticator) AuthenticateWithPassword(ctx context.Context, proxyHost net.Addr, clientIP net.IP, username, password string) (*proxyd.ProxySession, error) {

	params := radius_pkg.AuthorizationParams{
		Username:  username,
		Password:  password,
		UserAddr:  &net.IPAddr{IP: clientIP},
		ProxyHost: proxyHost,
	}

	entry := auth.acquireIndexEntry(params.Hash())
	defer entry.mtx.Unlock()

	if entry.sess != nil {
		entry.sess.lastUserActivity = time.Now()
		return &entry.sess.sess, nil
	} else if entry.miss != nil {
		return nil, &proxyd.ProxyCredentialsError{}
	}

	peer, err := auth.Client.Authorize(ctx, params)
	if err != nil {

		if _, ok := err.(*proxyd.ProxyCredentialsError); ok {

			miss := peerCredentialsMiss{
				AuthorizationParams: params,
				expires:             time.Now().Add(DefaultSessionTTL),
			}

			slog.Debug("RADIUS: Invalid credentials",
				slog.String("host_addr", proxyHost.String()),
				slog.String("client_ip", clientIP.String()),
				slog.String("username", username),
				slog.Time("timeout", miss.expires))

			entry.miss = &miss
		}

		return nil, err
	}

	state := peerSessionState{
		params:           params,
		lastUserActivity: time.Now(),
		slotID:           fmt.Sprintf("%v", proxyHost),
		dnsTester:        &auth.dnsTester,
		upstream:         &auth.Client,
	}

	if err := state.Refresh(ctx, peer); err != nil {
		return nil, err
	}

	entry.sess = &state

	slog.Info("RADIUS: Authorize session",
		slog.String("slot_id", state.slotID),
		slog.String("peer_id", state.sess.PeerID),
		slog.String("client_ip", clientIP.String()))

	return &state.sess, nil
}

func (auth *peerAuthenticator) acquireIndexEntry(key string) *peerEntry {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.refreshInit.CompareAndSwap(false, true) {
		auth.refreshCtx, auth.cancelRefresh = context.WithCancel(context.Background())
		go auth.indexRefreshRoutine()
	}

	return auth.acquireIndexEntryLocked(key)
}

func (auth *peerAuthenticator) acquireIndexEntryLocked(key string) *peerEntry {

	if auth.index == nil {
		auth.index = map[string]*peerEntry{}
	}

	entry := auth.index[key]
	if entry == nil {
		entry = &peerEntry{}
		auth.index[key] = entry
	}

	entry.mtx.Lock()

	return entry
}

func (auth *peerAuthenticator) removeIndexEntry(key string) {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.index == nil {
		return
	}

	if entry := auth.index[key]; entry != nil {
		entry.mtx.Lock()
		defer entry.mtx.Unlock()
	}

	delete(auth.index, key)
}

func (auth *peerAuthenticator) getIndexAccountingSession(acctID string) *peerSessionState {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	for _, val := range auth.index {
		if sess := val.sess; sess != nil && sess.acctSid == acctID {
			return sess
		}
	}

	return nil
}

func (auth *peerAuthenticator) indexRefreshRoutine() {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			auth.refreshIndex(auth.refreshCtx)
		case <-auth.refreshCtx.Done():
			return
		}
	}
}

func (auth *peerAuthenticator) refreshIndex(ctx context.Context) {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	for key, entry := range auth.index {
		auth.lockAndRefreshIndexEntry(ctx, key, entry)
	}

	auth.wg.Wait()
}

func (auth *peerAuthenticator) lockAndRefreshIndexEntry(ctx context.Context, key string, entry *peerEntry) {

	entry.mtx.Lock()

	if entry.sess != nil {
		auth.refreshLockedIndexedSession(ctx, entry)
		return
	}

	defer entry.mtx.Unlock()

	if miss := entry.miss; miss != nil && miss.expires.Before(time.Now()) {

		slog.Debug("RADIUS: Login timeout expired",
			slog.String("host_addr", miss.ProxyHost.String()),
			slog.String("client_ip", miss.UserAddr.String()),
			slog.String("username", miss.Username),
			slog.String("client_ip", miss.UserAddr.String()))

		delete(auth.index, key)
		entry.reset()

		return
	}
}

func (auth *peerAuthenticator) refreshLockedIndexedSession(ctx context.Context, entry *peerEntry) {

	state := entry.sess
	now := time.Now()

	if state.expires.After(now) {
		defer entry.mtx.Unlock()
		state.sess.Pool.Rebalance()
		state.Account(ctx)
		return
	}

	if state.lastUserActivity.Add(state.idleTTL).After(now) {
		auth.wg.Add(1)
		go auth.reauthLockedIndexedSession(ctx, entry)
		return
	}

	defer entry.mtx.Unlock()
	auth.expireIndexedSession(ctx, entry)
}

func (auth *peerAuthenticator) reauthLockedIndexedSession(ctx context.Context, entry *peerEntry) {

	defer entry.mtx.Unlock()
	defer auth.wg.Done()

	state := entry.sess

	if err := state.Reauthenticate(ctx); err != nil {
		slog.Debug("RADIUS: Session re-auth failed",
			slog.String("slot_id", state.slotID),
			slog.String("peer_id", state.sess.PeerID),
			slog.String("acct_sess", state.acctSid),
			slog.String("err", err.Error()))
		auth.expireIndexedSession(ctx, entry)
		return
	}

	slog.Info("RADIUS: Re-authorized session",
		slog.String("slot_id", state.slotID),
		slog.String("peer_id", state.sess.PeerID),
		slog.String("acct_sess", state.acctSid))

	entry.sess.Account(ctx)
}

func (auth *peerAuthenticator) expireIndexedSession(ctx context.Context, entry *peerEntry) {

	state := entry.sess

	slog.Debug("RADIUS: Session expired",
		slog.String("slot_id", state.slotID),
		slog.String("peer_id", state.sess.PeerID),
		slog.String("acct_sess", state.acctSid))

	delete(auth.index, state.params.Hash())

	entry.reset()
	state.Terminate(ctx)
}

func (auth *peerAuthenticator) DisconnectSession(ctx context.Context, acctSid string) error {

	state := auth.getIndexAccountingSession(acctSid)
	if state == nil {
		return errors.New("session not found")
	}

	slog.Debug("RADIUS: Forcing client disconnect",
		slog.String("slot_id", state.slotID),
		slog.String("peer_id", state.sess.PeerID))

	auth.removeIndexEntry(state.params.Hash())
	state.Terminate(ctx)

	return nil
}

func (auth *peerAuthenticator) ChangeSessionAuthority(ctx context.Context, peer *radius_pkg.PeerAuthorization) error {

	state := auth.getIndexAccountingSession(peer.AcctSessionID)
	if state == nil {
		return errors.New("session not found")
	}

	slog.Debug("RADIUS: Changing client authority",
		slog.String("slot_id", state.slotID),
		slog.String("peer_id", state.sess.PeerID))

	return state.Refresh(ctx, peer)
}

func (auth *peerAuthenticator) DACHandler() radius.Handler {
	return radius.HandlerFunc(func(wrt radius.ResponseWriter, req *radius.Request) {
		if reply := auth.replyDAC(req); reply != nil {
			wrt.Write(reply)
		}
	})
}

func (auth *peerAuthenticator) replyDAC(req *radius.Request) *radius.Packet {

	switch req.Code {

	case radius.CodeDisconnectRequest:

		sessionID := rfc2866.AcctSessionID_GetString(req.Packet)
		if sessionID == "" {
			reply := req.Response(radius.CodeDisconnectNAK)
			rfc3576.ErrorCause_Set(reply, rfc3576.ErrorCause_Value_MissingAttribute)
			return reply
		}

		if err := auth.DisconnectSession(req.Context(), sessionID); err != nil {
			reply := req.Response(radius.CodeDisconnectNAK)
			rfc3576.ErrorCause_Set(reply, rfc3576.ErrorCause_Value_SessionContextNotFound)
			return reply
		}

		return req.Response(radius.CodeDisconnectACK)

	case radius.CodeCoARequest:

		sessionID := rfc2866.AcctSessionID_GetString(req.Packet)
		if sessionID == "" {
			reply := req.Response(radius.CodeCoANAK)
			rfc3576.ErrorCause_Set(reply, rfc3576.ErrorCause_Value_MissingAttribute)
			return reply
		}

		if err := auth.ChangeSessionAuthority(req.Context(), radius_pkg.PeerAuthFromPacket(req.Packet)); err != nil {
			reply := req.Response(radius.CodeCoANAK)
			rfc3576.ErrorCause_Set(reply, rfc3576.ErrorCause_Value_SessionContextNotFound)
			return reply
		}

		return req.Response(radius.CodeCoAACK)

	default:
		slog.Warn("RADIUS DAC: Unknown code",
			slog.String("code", req.Code.String()))
		return nil
	}
}

func (auth *peerAuthenticator) Shutdown(ctx context.Context) error {

	auth.mtx.Lock()
	defer auth.mtx.Unlock()

	if auth.refreshInit.CompareAndSwap(true, false) {
		auth.cancelRefresh()
	}

	for _, entry := range auth.index {

		entry.mtx.Lock()

		if sess := entry.sess; sess != nil {
			sess.Terminate(ctx)
		}

		entry.mtx.Unlock()
	}

	auth.index = nil

	return ctx.Err()
}
