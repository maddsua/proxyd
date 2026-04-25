package main

import (
	"net"

	"github.com/google/uuid"
	radius_pkg "github.com/maddsua/proxyd/radius"
	radius_manager "github.com/maddsua/proxyd/radius/manager"
	"github.com/maddsua/proxyd/rpc"
	rpc_client "github.com/maddsua/proxyd/rpc/client"
	static_config "github.com/maddsua/proxyd/staticconfig"
	static_manager "github.com/maddsua/proxyd/staticconfig"
)

const GlobalConfigLocation = "/etc/proxyd/proxyd.yml"

type GlobalConfiguration struct {
	Debug   bool                      `json:"debug" yaml:"debug"`
	Manager ManagerConfiguration      `json:"manager" yaml:"manager"`
	RPC     RPCServerConfiguration    `json:"rpc_server" yaml:"rpc_server"`
	Radius  RadiusServerConfiguration `json:"radius_server" yaml:"radius_server"`
}

const (
	ManagerTypeStatic = "static"
	ManagerTypeRPC    = "rpc"
	ManagerTypeRadius = "radius"
)

type ManagerConfiguration struct {
	Type string `json:"type" yaml:"type"`

	rpc_client.RPCClientConfig         `yaml:",inline"`
	static_manager.StaticManagerConfig `yaml:",inline"`
	radius_manager.RadiusOptions       `yaml:",inline"`
}

type RPCServerConfiguration struct {
	ListenAddr string                           `json:"listen_addr" yaml:"listen_addr"`
	Instances  []RPCClientInstanceConfiguration `json:"instances" yaml:"instances"`
}

type RPCClientInstanceConfiguration struct {
	ID     uuid.UUID        `json:"id" yaml:"id"`
	Secret rpc.RawSecretKey `json:"secret" yaml:"secret"`

	static_manager.StaticManagerConfig `yaml:",inline"`
}

type RadiusServerConfiguration struct {
	ListenAddr string             `json:"listen_addr" yaml:"listen_addr"`
	DacAddr    string             `json:"dac_addr" yaml:"dac_addr"`
	Secret     string             `json:"secret" yaml:"secret"`
	Users      []RadiusUserConfig `json:"users" yaml:"users"`
}

type RadiusUserConfig struct {
	ProxyHost                string `json:"proxy_host" yaml:"proxy_host"`
	static_config.UserConfig `yaml:",inline"`
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
		ConnectionLimit:  cfg.MaxConn,
		MaxRxRate:        int64(cfg.BandwidthKbit) * 1000,
		MaxTxRate:        int64(cfg.BandwidthKbit) * 1000,
	}
}
