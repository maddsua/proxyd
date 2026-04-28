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
	Debug   bool                      `json:"debug" yaml:"debug"`
	Manager ManagerConfiguration      `json:"manager" yaml:"manager"`
	RPC     RPCServerConfiguration    `json:"rpc_server" yaml:"rpc_server"`
	Radius  RadiusServerConfiguration `json:"radius_server" yaml:"radius_server"`
}

const (
	ManagerTypeLocal  = "local"
	ManagerTypeRPC    = "rpc"
	ManagerTypeRadius = "radius"
)

type ManagerConfiguration struct {
	Type string `json:"type" yaml:"type"`

	rpc_client.RPCClientConfig   `yaml:",inline"`
	local.LocalManagerConfig     `yaml:",inline"`
	radius_manager.RadiusOptions `yaml:",inline"`
}

type RPCServerConfiguration struct {
	ListenAddr                 string `json:"listen_addr" yaml:"listen_addr"`
	rpc_handler.HandlerOptions `yaml:",inline"`
}

type RadiusServerConfiguration struct {
	ListenAddr string                            `json:"listen_addr" yaml:"listen_addr"`
	DacAddr    string                            `json:"dac_addr" yaml:"dac_addr"`
	Secret     string                            `json:"secret" yaml:"secret"`
	Users      []radius_handler.RadiusUserConfig `json:"users" yaml:"users"`
}
