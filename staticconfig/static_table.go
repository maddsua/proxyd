package static

import (
	http_pkg "github.com/maddsua/proxyd/http"
	"github.com/maddsua/proxyd/proxytable"
	"github.com/maddsua/proxyd/utils"
)

type ConfigurationWrapper struct {
	Manager StaticManagerConfig `json:"manager" yaml:"manager"`
}

type StaticManagerConfig struct {
	Services []ServiceUtilityConfig `json:"services" yaml:"services"`
}

type ServiceUtilityConfig struct {
	BindAddr string       `json:"bind_addr" yaml:"bind_addr"`
	Type     string       `json:"type" yaml:"type"`
	Users    []UserConfig `json:"users" yaml:"users"`

	http_pkg.HttpServiceOptions `yaml:",inline"`
}

type UserConfig struct {
	Username      string `json:"username" yaml:"username"`
	Password      string `json:"password" yaml:"password"`
	Suspended     bool   `json:"suspended" yaml:"suspended"`
	MaxConn       int    `json:"max_conn" yaml:"max_conn"`
	BandwidthKbit int    `json:"bandwidth_kbit" yaml:"bandwidth_kbit"`
	DNS           string `json:"dns" yaml:"dns"`
	OutboundAddr  string `json:"outbound_addr" yaml:"outbound_addr"`
}

func ProxyServiceTable(entries []ServiceUtilityConfig) []proxytable.ProxyServiceEntry {

	services := make([]proxytable.ProxyServiceEntry, len(entries))

	for idx, entry := range entries {

		peers := make([]proxytable.ProxyTablePeerEntry, len(entry.Users))

		for idx, entry := range entry.Users {

			peer := proxytable.ProxyTablePeerEntry{
				ID: entry.Username,
				Userinfo: &proxytable.ProxyPeerUserInfo{
					Username: entry.Username,
					Password: entry.Password,
				},
				Enabled:        !entry.Suspended,
				MaxConnections: entry.MaxConn,
				Bandwidth: &proxytable.ProxyPeerBandwidth{
					RxBytes: utils.KbitsToRawBandwidth(entry.BandwidthKbit),
					TxBytes: utils.KbitsToRawBandwidth(entry.BandwidthKbit),
				},
				DNS:          entry.DNS,
				OutboundAddr: entry.OutboundAddr,
			}

			if entry.BandwidthKbit > 0 {
				peer.Bandwidth = &proxytable.ProxyPeerBandwidth{
					RxBytes: utils.KbitsToRawBandwidth(entry.BandwidthKbit),
					TxBytes: utils.KbitsToRawBandwidth(entry.BandwidthKbit),
				}
			}

			peers[idx] = peer
		}

		services[idx] = proxytable.ProxyServiceEntry{
			ProxyServiceOptions: proxytable.ProxyServiceOptions{
				BindAddr:           entry.BindAddr,
				Service:            entry.Type,
				HttpServiceOptions: entry.HttpServiceOptions,
			},
			Peers: peers,
		}
	}

	return services
}
