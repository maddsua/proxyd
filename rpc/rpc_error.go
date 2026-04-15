package rpc

import "github.com/maddsua/proxyd/rpc/model"

type Error struct {
	model.RPCError
	Code int
}

func (err *Error) StatusCode() int {
	return err.Code
}
