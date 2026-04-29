package proxytable

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

type peerSlot struct {
	parentName string
	init       bool

	user *ProxyPeerUserInfo
	sess proxyd.ProxySession

	mtx sync.Mutex
	wg  sync.WaitGroup

	authRl    utils.ExpireMap[uint64]
	dnsLocked atomic.Bool
	dnsTester *proxyd.DNSTester
}

func (peer *peerSlot) displayName() string {
	if user := peer.user; user != nil {
		return user.Username
	}
	return peer.sess.PeerID
}

func (peer *peerSlot) refresh(entry ProxyTablePeerEntry) {

	defer peer.mtx.Unlock()

	var sessionReset bool

	if unwrapUserInfo(peer.user) != unwrapUserInfo(entry.Userinfo) {

		if peer.init {
			slog.Info("PeerAuthenticator: Update credentials",
				slog.String("slot", peer.parentName),
				slog.String("peer", peer.displayName()))
		}

		peer.user = entry.Userinfo
		peer.authRl.Clear()

		sessionReset = true
	}

	if peer.sess.PeerEnabled != entry.Enabled {

		if peer.init {
			if entry.Enabled {
				slog.Info("PeerAuthenticator: Enable peer",
					slog.String("slot", peer.parentName),
					slog.String("peer", peer.displayName()))
			} else {
				slog.Info("PeerAuthenticator: Disable peer",
					slog.String("slot", peer.parentName),
					slog.String("peer", peer.displayName()))
			}
		}

		peer.sess.PeerEnabled = entry.Enabled
		sessionReset = true
	}

	if wantDNS := unwrapDnsServerAddr(entry.DNS); !peer.sess.DNS.Server.Load().Equal(wantDNS) {

		// the dns update is a bit complicated here,
		// but it basically boils down to making sure
		// that you're not blocking the whole authenticator
		// while checking whether or not a provided server is valid

		var applyResult = func(err error, logUpdate bool) {

			if err != nil {
				slog.Warn("PeerAuthenticator: DNS server cannot be set",
					slog.String("slot", peer.parentName),
					slog.String("peer", peer.displayName()),
					slog.String("dns", entry.DNS),
					slog.String("err", err.Error()))
				return
			}

			if logUpdate {
				slog.Info("PeerAuthenticator: Update DNS server",
					slog.String("slot", peer.parentName),
					slog.String("peer", peer.displayName()),
					slog.String("dns", wantDNS.Name()))
			}
			peer.sess.DNS.Server.Store(wantDNS)
		}

		// check the cache first to speed up tests of frequently used servers,
		// and only go poke at it if that is absolutely necessary

		if wantDNS == nil {
			applyResult(nil, peer.init)
		} else if err, valid := peer.dnsTester.LookupCached(wantDNS.Addr()); valid {
			applyResult(err, peer.init)
		} else if peer.dnsLocked.CompareAndSwap(false, true) {

			// an atomic bool acts as a guard here to make sure that
			// the subsequent refresh calls don't create a logic race,
			// where the same DNS server is checked by multiple routines in parallel.
			// this, however can cause the DNS to lag behind until the next update cycle

			peer.wg.Add(1)

			logUpdate := peer.init

			go func() {
				defer peer.wg.Done()
				defer peer.dnsLocked.Store(false)
				applyResult(peer.dnsTester.Test(context.Background(), wantDNS.Addr()), logUpdate)
			}()

			if peer.init {
				slog.Debug("PeerAuthenticator: Deferred DNS server test",
					slog.String("slot", peer.parentName),
					slog.String("peer", peer.displayName()),
					slog.String("dns", wantDNS.Name()))
			}
		}
	}

	if wantOutboundAddr, err := unwrapPeerOutboundIP(entry.OutboundAddr); err != nil {

		slog.Warn("PeerAuthenticator: Outbound IP cannot be set",
			slog.String("slot", peer.parentName),
			slog.String("peer", peer.displayName()),
			slog.String("addr", entry.OutboundAddr),
			slog.String("err", err.Error()))

	} else if !peer.sess.Dialer.OutboundAddr.Load().Equal(wantOutboundAddr) {

		if peer.init {
			slog.Info("PeerAuthenticator: Update outbound address",
				slog.String("slot", peer.parentName),
				slog.String("peer", peer.displayName()),
				slog.String("addr", wantOutboundAddr.String()))
		}

		peer.sess.Dialer.OutboundAddr.Store(wantOutboundAddr)
		sessionReset = true
	}

	if peer.sess.Pool.ConnectionLimit() != entry.MaxConnections {

		if peer.init {
			slog.Info("PeerAuthenticator: Update connection limit",
				slog.String("slot", peer.parentName),
				slog.String("peer", peer.displayName()),
				slog.Int("maxconn", entry.MaxConnections))
		}

		if err := peer.sess.Pool.SetConnectionLimit(entry.MaxConnections); err != nil {
			slog.Error("PeerAuthenticator: Update connection limit",
				slog.String("slot", peer.parentName),
				slog.String("peer", peer.displayName()),
				slog.Int("maxconn", entry.MaxConnections),
				slog.String("err", err.Error()))
		}
	}

	wantRx, wantTx := unwrapPeerBandwidth(entry.Bandwidth)
	if haveRx, haveTx := peer.sess.Pool.Bandwidth(); wantRx != haveRx || wantTx != haveTx {

		if peer.init {
			slog.Info("PeerAuthenticator: Update bandwidth",
				slog.String("slot", peer.parentName),
				slog.String("peer", peer.displayName()),
				slog.Int64("rx_rate", wantRx),
				slog.Int64("tx_rate", wantTx))
		}

		peer.sess.Pool.SetBandwidth(wantRx, wantTx)
	}

	if peer.init && sessionReset {

		slog.Debug("PeerAuthenticator: Forcing re-auth",
			slog.String("slot", peer.parentName),
			slog.String("peer", peer.displayName()))

		peer.sess.Reset()
	}

	if !peer.init {

		rxMax, txMax := peer.sess.Pool.Bandwidth()

		slog.Info("PeerAuthenticator: Add peer",
			slog.String("slot", peer.parentName),
			slog.String("peer", peer.displayName()),
			slog.String("addr", peer.sess.Dialer.OutboundAddr.Load().String()),
			slog.String("dns", peer.sess.DNS.Server.Load().Name()),
			slog.Int("max_conn", peer.sess.Pool.ConnectionLimit()),
			slog.Int64("rx_rate", rxMax),
			slog.Int64("tx_rate", txMax))

		peer.init = true
	}
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

func unwrapDnsServerAddr(addr string) *proxyd.DNSAddr {
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
