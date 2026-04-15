package model

import (
	"github.com/google/uuid"
	"github.com/maddsua/proxyd/proxytable"
)

const (
	ProcedureReady      = "ready"
	ProcedureStatus     = "status"
	ProcedureTraffic    = "traffic"
	ProcedureProxyTable = "proxytable"
)

type Result[T any] struct {
	Data  *T        `json:"data"`
	Error *RPCError `json:"error"`
}

type RPCError struct {
	Message string `json:"message"`
	Cause   string `json:"cause,omitempty"`
}

func (err *RPCError) Error() string {
	return err.Message
}

type InstanceStatus struct {
	RunID    uuid.UUID                  `json:"run_id"`
	Uptime   int64                      `json:"uptime"`
	Services []proxytable.ServiceStatus `json:"services"`
}

type InstanceTrafficUpdate struct {
	Deltas []proxytable.TrafficDelta `json:"deltas"`
}

type ProxyTable struct {
	Services []proxytable.ProxyServiceEntry `json:"services"`
}
