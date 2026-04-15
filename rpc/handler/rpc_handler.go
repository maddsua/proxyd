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

	mux.Handle("POST /"+model.ProcedureStatus, rpcUpdateHandler(handler.OnStatus))
	mux.Handle("POST /"+model.ProcedureTraffic, rpcUpdateHandler(handler.OnTraffic))
	mux.Handle("GET /"+model.ProcedureProxyTable, rpcDataHandler(handler.OnProxyTable))

	mux.Handle("GET /"+model.ProcedureReady, http.HandlerFunc(func(wrt http.ResponseWriter, _ *http.Request) {
		wrt.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("/", http.HandlerFunc(func(wrt http.ResponseWriter, req *http.Request) {
		wrt.Header().Set("Content-Type", "application/json")
		wrt.WriteHeader(http.StatusNotFound)
		json.NewEncoder(wrt).Encode(model.Result[any]{Error: &model.RPCError{
			Message: "route not defined",
			Cause:   fmt.Sprintf("route '%s %s' doesn't point to any of the defined procedures", req.Method, req.URL.Path),
		}})
	}))

	return mux
}

type procFn[T any, R any] func(ctx context.Context, token *rpc.InstanceToken, params *T) (*R, error)
type procHandler[R any] func(req *http.Request) (*R, error)

func genericProcedureHandler[T any, R any](procedure procFn[T, R]) http.Handler {
	return wrapRPC(func(req *http.Request) (*R, error) {

		token, err := requestToken(req)
		if err != nil {
			return nil, err
		}

		params, err := requestPayload[T](req)
		if err != nil {
			return nil, err
		}

		return procedure(req.Context(), token, params)
	})
}

func rpcDataHandler[R any](procedure func(ctx context.Context, token *rpc.InstanceToken) (*R, error)) http.Handler {
	return genericProcedureHandler(func(ctx context.Context, token *rpc.InstanceToken, params *any) (*R, error) {

		if procedure == nil {
			return nil, &rpc.Error{
				RPCError: model.RPCError{
					Message: "procedure not implemented",
				},
				Code: http.StatusNotImplemented,
			}
		}

		if params != nil {
			return nil, &rpc.Error{
				RPCError: model.RPCError{
					Message: "procedure doen't accept parameters",
				},
				Code: http.StatusBadRequest,
			}
		}

		return procedure(ctx, token)
	})
}

func rpcUpdateHandler[T any](procedure func(ctx context.Context, token *rpc.InstanceToken, params T) error) http.Handler {
	return genericProcedureHandler(func(ctx context.Context, token *rpc.InstanceToken, params *T) (*any, error) {

		if procedure == nil {
			return nil, &rpc.Error{
				RPCError: model.RPCError{
					Message: "procedure not implemented",
				},
				Code: http.StatusNotImplemented,
			}
		}

		if params == nil {
			return nil, &rpc.Error{
				RPCError: model.RPCError{
					Message: "procedure requires parameters",
				},
				Code: http.StatusBadRequest,
			}
		}

		return nil, procedure(ctx, token, *params)
	})
}

func requestToken(req *http.Request) (*rpc.InstanceToken, error) {

	auth := req.Header.Get("Authorization")
	if auth == "" {
		return nil, nil
	}

	schema, bearer, _ := strings.Cut(auth, " ")
	if !strings.EqualFold(schema, "Bearer") {
		return nil, &rpc.Error{
			RPCError: model.RPCError{
				Message: "Unauthorized",
				Cause:   "Invalid auth schema",
			},
			Code: http.StatusUnauthorized,
		}
	}

	var token rpc.InstanceToken
	if err := token.UnmarshalText(bearer); err != nil {
		return nil, &rpc.Error{
			RPCError: model.RPCError{
				Message: "Unauthorized",
				Cause:   "Invalid instance token",
			},
			Code: http.StatusUnauthorized,
		}
	}

	return &token, nil
}

func requestPayload[T any](req *http.Request) (*T, error) {

	if req.ContentLength == 0 {
		return nil, nil
	}

	if contentType := req.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(contentType), "json") {
		return nil, &rpc.Error{
			RPCError: model.RPCError{
				Message: "unsupported content type",
				Cause:   fmt.Sprintf("content type '%s' not support", contentType),
			},
			Code: http.StatusBadRequest,
		}
	}

	var payload T
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		return nil, &rpc.Error{
			RPCError: model.RPCError{
				Message: "decode json",
				Cause:   err.Error(),
			},
			Code: http.StatusBadRequest,
		}
	}

	return &payload, nil
}

func wrapRPC[R any](procedure procHandler[R]) http.Handler {
	return http.HandlerFunc(func(wrt http.ResponseWriter, req *http.Request) {

		data, err := procedure(req)
		if data == nil && err == nil {
			wrt.WriteHeader(http.StatusNoContent)
			return
		}

		wrt.Header().Set("Content-Type", "application/json")

		if err != nil {
			writeRPCError(wrt, err)
			return
		}

		json.NewEncoder(wrt).Encode(model.Result[R]{Data: data})
	})
}

func writeRPCError(wrt http.ResponseWriter, err error) {
	writeRPCErrorCode(wrt, err)
	json.NewEncoder(wrt).Encode(wrapRPCError(err))
}

func writeRPCErrorCode(wrt http.ResponseWriter, err error) {

	if sc, ok := err.(StatusCoder); ok {
		if code := sc.StatusCode(); code >= http.StatusBadRequest {
			wrt.WriteHeader(code)
			return
		}
	}

	wrt.WriteHeader(http.StatusBadRequest)
}

func wrapRPCError(err error) model.Result[any] {

	if err, ok := err.(*model.RPCError); ok {
		return model.Result[any]{Error: err}
	}

	if err, ok := err.(*rpc.Error); ok {
		return model.Result[any]{Error: &err.RPCError}
	}

	return model.Result[any]{Error: &model.RPCError{Message: err.Error()}}
}

type StatusCoder interface {
	StatusCode() int
}
