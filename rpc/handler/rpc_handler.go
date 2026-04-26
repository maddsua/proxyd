package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/rpc/model"
)

type MethodHandler interface {
	OnStatus(ctx context.Context, token *rpc.InstanceToken, params model.InstanceStatus) error
	OnTraffic(ctx context.Context, token *rpc.InstanceToken, params model.InstanceTrafficUpdate) error
	OnProxyTable(ctx context.Context, token *rpc.InstanceToken) (*model.ProxyTable, error)
}

const RoutePrefix = "/proxyd/rpc/v1"

func HandleWithMux(mux *http.ServeMux, handler MethodHandler) {
	mux.Handle(RoutePrefix+"/", http.StripPrefix(RoutePrefix, NewHandler(handler)))
}

func NewHandler(handler MethodHandler) http.Handler {

	mux := http.NewServeMux()

	mux.Handle("POST /"+model.ProcedureStatus, receiveRPCHandler(handler.OnStatus))
	mux.Handle("POST /"+model.ProcedureTraffic, receiveRPCHandler(handler.OnTraffic))
	mux.Handle("GET /"+model.ProcedureProxyTable, sendRPCHandler(handler.OnProxyTable))

	mux.Handle("GET /"+model.ProcedureReady, http.HandlerFunc(func(wrt http.ResponseWriter, _ *http.Request) {
		wrt.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("/", wrapRPC(func(req *http.Request) (*any, error) {
		return nil, &ProcedureError{
			RPCError: model.RPCError{
				Message: "route not defined",
				Cause:   fmt.Sprintf("route '%s %s' doesn't point to any of the defined procedures", req.Method, req.URL.Path),
			},
			Code: http.StatusNotFound,
		}
	}))

	return mux
}

func sendRPCHandler[R any](procedure func(ctx context.Context, token *rpc.InstanceToken) (*R, error)) http.Handler {
	return wrapRPC(func(req *http.Request) (*R, error) {

		token, err := requestToken(req)
		if err != nil {
			return nil, err
		}

		if req.ContentLength != 0 {
			return nil, &ProcedureError{
				RPCError: model.RPCError{
					Message: "procedure doesn't accept data",
				},
				Code: http.StatusBadRequest,
			}
		}

		return procedure(req.Context(), token)
	})
}

func receiveRPCHandler[T any](procedure func(ctx context.Context, token *rpc.InstanceToken, params T) error) http.Handler {
	return wrapRPC(func(req *http.Request) (*any, error) {

		token, err := requestToken(req)
		if err != nil {
			return nil, err
		}

		params, err := requestPayload[T](req)
		if err != nil {
			return nil, err
		}

		return nil, procedure(req.Context(), token, params)
	})
}

func requestToken(req *http.Request) (*rpc.InstanceToken, error) {

	auth := req.Header.Get("Authorization")
	if auth == "" {
		return nil, nil
	}

	schema, bearer, _ := strings.Cut(auth, " ")
	if !strings.EqualFold(schema, "Bearer") {
		return nil, &ProcedureError{
			RPCError: model.RPCError{
				Message: "Unauthorized",
				Cause:   "Invalid auth schema",
			},
			Code: http.StatusUnauthorized,
		}
	}

	var token rpc.InstanceToken
	if err := token.UnmarshalText(bearer); err != nil {
		return nil, &ProcedureError{
			RPCError: model.RPCError{
				Message: "Unauthorized",
				Cause:   "Invalid instance token",
			},
			Code: http.StatusUnauthorized,
		}
	}

	return &token, nil
}

func requestPayload[T any](req *http.Request) (payload T, err error) {

	if req.ContentLength == 0 {
		err = &ProcedureError{
			RPCError: model.RPCError{
				Message: "request contains no data",
			},
			Code: http.StatusBadRequest,
		}
		return
	}

	if contentType := req.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(contentType), "json") {
		err = &ProcedureError{
			RPCError: model.RPCError{
				Message: "unsupported content type",
				Cause:   fmt.Sprintf("content type '%s' not support", contentType),
			},
			Code: http.StatusBadRequest,
		}
		return
	}

	if derr := json.NewDecoder(req.Body).Decode(&payload); derr != nil {
		err = &ProcedureError{
			RPCError: model.RPCError{
				Message: "decode data",
				Cause:   derr.Error(),
			},
			Code: http.StatusBadRequest,
		}
	}

	return
}

type procHandler[R any] func(req *http.Request) (*R, error)

func wrapRPC[R any](procedure procHandler[R]) http.Handler {
	return http.HandlerFunc(func(wrt http.ResponseWriter, req *http.Request) {

		data, err := procedure(req)
		if data == nil && err == nil {
			wrt.WriteHeader(http.StatusNoContent)
			return
		}

		wrt.Header().Set("Content-Type", "application/json")
		wrt.WriteHeader(rpcResultCode(err))
		json.NewEncoder(wrt).Encode(newResult(data, err))
	})
}

func newResult[R any](data *R, err error) model.Result[R] {

	if err == nil {
		return model.Result[R]{Data: data}
	}

	if err, ok := err.(*model.RPCError); ok {
		return model.Result[R]{Error: err}
	}

	if err, ok := err.(*ProcedureError); ok {
		return model.Result[R]{Error: &err.RPCError}
	}

	return model.Result[R]{Error: &model.RPCError{Message: err.Error()}}
}

type StatusCoder interface {
	StatusCode() int
}

func rpcResultCode(err error) int {

	if err == nil {
		return http.StatusOK
	}

	if sc, ok := err.(StatusCoder); ok {
		if code := sc.StatusCode(); code >= http.StatusBadRequest {
			return code
		}
	}

	return http.StatusBadRequest
}

type ProcedureError struct {
	model.RPCError
	Code int
}

func (err *ProcedureError) StatusCode() int {
	return err.Code
}
