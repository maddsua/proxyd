package proxytable

import (
	"fmt"
	"time"

	http_pkg "github.com/maddsua/proxyd/http"
)

type ProxyServiceOptions struct {
	BindAddr string `json:"bind_addr"`
	Service  string `json:"service"`

	http_pkg.HttpServiceOptions
}

type ProxyServiceEntry struct {
	ProxyServiceOptions
	Peers []ProxyTablePeerEntry `json:"peers"`
}

func (entry *ProxyServiceEntry) bindKey() string {
	return "tcp/" + entry.BindAddr
}

func (entry *ProxyServiceEntry) slotName() string {
	return fmt.Sprintf("%s/%s", entry.BindAddr, entry.Service)
}

type ProxyTablePeerEntry struct {
	ID             string              `json:"id"`
	Userinfo       *ProxyPeerUserInfo  `json:"userinfo"`
	Enabled        bool                `json:"enabled"`
	MaxConnections int                 `json:"max_connections"`
	Bandwidth      *ProxyPeerBandwidth `json:"bandwidth"`
	DNS            string              `json:"dns"`
	OutboundAddr   string              `json:"outbound_addr"`
}

type ProxyPeerUserInfo struct {
	Username string `json:"username"`
	Password string `json:"password"`

	Options *PeerLoginOptions `json:"options"`
}

const DefaultAllowLoginAttempts = 50
const DefaultLoginAttemptWindow = 15 * time.Minute

type PeerLoginOptions struct {
	MaxAttempts   int `json:"max_attempts"`
	AttemptWindow int `json:"attempt_window"`
}

func (opts *PeerLoginOptions) RateLimiter() (quota int, window time.Duration) {

	if opts != nil && opts.MaxAttempts > 0 && opts.AttemptWindow > 0 {
		return opts.MaxAttempts, time.Duration(opts.AttemptWindow) * time.Second
	}

	return DefaultAllowLoginAttempts, DefaultLoginAttemptWindow
}

type ProxyPeerBandwidth struct {
	RxBytes int `json:"rx"`
	TxBytes int `json:"tx"`
}

type TrafficDelta struct {
	PeerID  string `json:"peer_id"`
	RxBytes int64  `json:"rx"`
	TxBytes int64  `json:"tx"`
}

type ServiceStatus struct {
	BindAddr string       `json:"bind_addr"`
	Type     string       `json:"type"`
	Up       bool         `json:"up"`
	Peers    []PeerStatus `json:"peers"`
	Error    string       `json:"error,omitempty"`
}

type PeerStatus struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
	Enabled  bool   `json:"enabled"`
}
