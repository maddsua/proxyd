package main

import (
	"github.com/maddsua/proxyd/local"
	radius_handler "github.com/maddsua/proxyd/radius/handler"
	radius_manager "github.com/maddsua/proxyd/radius/manager"
	rpc_client "github.com/maddsua/proxyd/rpc/client"
	rpc_handler "github.com/maddsua/proxyd/rpc/handler"
)

const GlobalConfigLocation = "/etc/proxyd/proxyd.yml"

type GlobalConfiguration struct {
	LegacyManagerOptions `yaml:",inline"`
	Debug                bool                `json:"debug" yaml:"debug"`
	Manager              ManagerOptions      `json:"manager" yaml:"manager"`
	RPC                  RPCServerOptions    `json:"rpc_server" yaml:"rpc_server"`
	Radius               RadiusServerOptions `json:"radius_server" yaml:"radius_server"`
}

type LegacyManagerOptions struct {
	RemoteURL   string `json:"remote_url" yaml:"remote_url"`
	SecretToken string `json:"secret_token" yaml:"secret_token"`
}

const (
	ManagerTypeLocal  = "local"
	ManagerTypeRPC    = "rpc"
	ManagerTypeRadius = "radius"
)

type ManagerOptions struct {
	Type string `json:"type" yaml:"type"`

	rpc_client.RPCClientOptions  `yaml:",inline"`
	local.LocalManagerOptions    `yaml:",inline"`
	radius_manager.RadiusOptions `yaml:",inline"`
}

type RPCServerOptions struct {
	ListenAddr                 string `json:"listen_addr" yaml:"listen_addr"`
	rpc_handler.HandlerOptions `yaml:",inline"`
}

type RadiusServerOptions struct {
	ListenAddr string                             `json:"listen_addr" yaml:"listen_addr"`
	DacAddr    string                             `json:"dac_addr" yaml:"dac_addr"`
	Secret     string                             `json:"secret" yaml:"secret"`
	Users      []radius_handler.RadiusUserOptions `json:"users" yaml:"users"`
}
